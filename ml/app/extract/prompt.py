"""Prompt template for LLM-first extraction (`extract-v3`).

Strategy (ADR-005 v2 / `tz_ml_extract_fix_v2.md` §3.5):
- LLM is the sole source of extraction decisions; deterministic code validates
  schema, taxonomy membership, quote presence, and `role_id in competency.roles`.
- Compact LLM output schema: `{role_id, competency_id, quote, level, criticality, headcount, confidence, needs_review}`.
- Vocabulary includes `id`, `name_ru`, main `synonyms`, and allowed `roles`
  (`competency.roles`) for every competency, plus `id`, `name_ru`, `synonyms`
  for every role.
- Few-shot examples use synthetic texts strictly outside `eval/gold` (Example 1
  that leaked `brief_001` is removed).
- Role-selection and implicit-requirement rules follow `eval/GOLD_POLICY.md`.

Security: `brief_text` is passed ONLY as a separate `user` message, never concatenated
into the system instruction (prompt-injection defense).
"""

from __future__ import annotations

import json
from typing import Iterable

from .taxonomy import TaxonomyRegistry

PROMPT_VERSION = "extract-v3"

_COMPACT_OUTPUT_SCHEMA = (
    "JSON array of compact requirement objects (Enriched into ExtractedRequirement[] per contracts/ml-openapi.yaml). "
    "Each element MUST have: "
    "role_id (string from roles vocabulary), "
    "competency_id (string from competencies vocabulary), "
    "quote (exact substring from the brief text, 3..1000 chars), "
    "level (integer 1..5: 1=intern, 2=junior, 3=middle, 4=senior/lead, 5=principal), "
    "criticality ('must' | 'should' | 'nice'), "
    "headcount (integer 1..20, default 1 unless explicit count is stated), "
    "confidence (number 0.0..1.0), "
    "needs_review (boolean: true when confidence < 0.7, implicit requirement, or ambiguous/contradictory text)."
)


def _build_few_shot_examples() -> str:
    """Return synthetic few-shot examples strictly outside `eval/gold`."""
    return """## Few-shot examples (synthetic texts outside evaluation set)

### Example A — explicit requirements, multi-role disambiguation, and stack-summary filtering
Brief: "Разрабатываем внутреннюю систему бронирования переговорных комнат для офиса. Бэкенд-сервис пишем на Python (FastAPI, уровень senior), хранение данных в PostgreSQL, кэширование в Redis. Веб-клиент на Vue.js (middle). Автоматизация сквозных тестов на Selenium. Деплой и CI/CD через GitHub Actions. Общий стек компании: Python, Vue.js, PostgreSQL, Redis, Linux."

Output:
[
  {
    "role_id": "backend_engineer",
    "competency_id": "lang_python",
    "quote": "Бэкенд-сервис пишем на Python",
    "level": 4,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.95,
    "needs_review": false
  },
  {
    "role_id": "backend_engineer",
    "competency_id": "fw_fastapi",
    "quote": "FastAPI, уровень senior",
    "level": 4,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.95,
    "needs_review": false
  },
  {
    "role_id": "backend_engineer",
    "competency_id": "db_postgresql",
    "quote": "хранение данных в PostgreSQL",
    "level": 4,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.9,
    "needs_review": false
  },
  {
    "role_id": "backend_engineer",
    "competency_id": "db_redis",
    "quote": "кэширование в Redis",
    "level": 4,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.9,
    "needs_review": false
  },
  {
    "role_id": "frontend_engineer",
    "competency_id": "fw_vue",
    "quote": "Веб-клиент на Vue.js (middle)",
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.95,
    "needs_review": false
  },
  {
    "role_id": "qa_engineer",
    "competency_id": "test_selenium",
    "quote": "Автоматизация сквозных тестов на Selenium",
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.9,
    "needs_review": false
  },
  {
    "role_id": "devops_engineer",
    "competency_id": "cicd_github_actions",
    "quote": "Деплой и CI/CD через GitHub Actions",
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.9,
    "needs_review": false
  }
]

### Example B — allowed implicit requirement (PCI DSS) and ambiguous phrasing
Brief: "Требуется модуль эквайринга для приёма банковских карт с прохождением аудита PCI DSS. Также нужно настроить конвейеры обработки событий для витрин и конвейеры сборки релизов."

Output:
[
  {
    "role_id": "security_engineer",
    "competency_id": "sec_appsec",
    "quote": "прохождением аудита PCI DSS",
    "level": 4,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.75,
    "needs_review": true
  },
  {
    "role_id": "data_engineer",
    "competency_id": "etl_orchestration",
    "quote": "конвейеры обработки событий для витрин",
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.65,
    "needs_review": true
  },
  {
    "role_id": "devops_engineer",
    "competency_id": "cicd_gitlab",
    "quote": "конвейеры сборки релизов",
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "confidence": 0.65,
    "needs_review": true
  }
]"""


def build_extract_prompt(
    brief_text: str,
    taxonomy: TaxonomyRegistry,
    locale: str = "ru",
    *,
    allowed_role_ids: Iterable[str] | None = None,
    allowed_competency_ids: Iterable[str] | None = None,
) -> list[dict[str, str]]:
    """Build OpenAI messages for LLM-first structured extraction (`extract-v3`).

    Returns a list of two messages:
    1. `system`: task instruction + closed vocabulary with synonyms & allowed roles + examples (no `brief_text`!)
    2. `user`: `brief_text` only (isolated to prevent prompt injection)
    """
    role_filter = set(allowed_role_ids) if allowed_role_ids is not None else None
    comp_filter = (
        set(allowed_competency_ids) if allowed_competency_ids is not None else None
    )

    role_lines: list[str] = []
    for rid in sorted(taxonomy.roles.keys()):
        if role_filter is not None and rid not in role_filter:
            continue
        r = taxonomy.roles[rid]
        syns = ", ".join(r.synonyms[:5])
        role_lines.append(f"- {r.role_id}: {r.name_ru} ({syns})")

    comp_lines: list[str] = []
    for cid in sorted(taxonomy.competencies.keys()):
        if comp_filter is not None and cid not in comp_filter:
            continue
        c = taxonomy.competencies[cid]
        allowed_roles = [
            r for r in c.roles if (role_filter is None or r in role_filter)
        ]
        if not allowed_roles:
            continue
        syns = ", ".join(c.synonyms[:5])
        roles_str = ", ".join(allowed_roles)
        comp_lines.append(
            f"- {c.competency_id}: {c.name_ru} ({syns}) -> roles: [{roles_str}]"
        )

    roles_block = "\n".join(role_lines)
    comps_block = "\n".join(comp_lines)

    system_text = (
        "You are a structured-extraction assistant for project team formation.\n"
        "Your task: extract staffing and competency requirements from a project brief.\n\n"
        "## Output format\n"
        "Output ONLY a valid JSON array. No prose, no markdown fences, no commentary.\n"
        f"Schema: {_COMPACT_OUTPUT_SCHEMA}\n\n"
        "## Core extraction rules (aligned with GOLD_POLICY.md)\n"
        "1. Every requirement MUST have a `competency_id` from the competencies vocabulary and a `role_id` from that competency's `roles` list (`role_id in competency.roles`). Never output a role that is not listed in `competency.roles`.\n"
        "2. Do NOT create role-only requirements without a competency mention.\n"
        "3. `quote` MUST be an exact, literal substring copied from the user's brief text (at least 3 characters). Never paraphrase or invent quotes.\n"
        "4. Generic stack summary lines (e.g. 'Стек: ...'): do NOT create a separate requirement for a technology that appears ONLY in a generic stack list and is not tied to any role, task, or architectural/deployment requirement in the brief.\n"
        "5. Role selection rules for competencies allowed for multiple roles:\n"
        "   - First priority: choose the role explicitly named in the same sentence, clause, or bullet item.\n"
        "   - Second priority (when no role is explicitly named in the sentence): choose by domain responsibility in the brief:\n"
        "     * E2E testing, test automation, QA, Playwright, Selenium, load/API testing (`test_*`, `qa_*`) -> `qa_engineer`.\n"
        "     * Cloud platforms (Yandex Cloud, AWS, GCP, Azure), Docker, Kubernetes, CI/CD (GitLab CI, GitHub Actions), Terraform, Prometheus/Grafana/OpenTelemetry (`cloud_*`, `cicd_*`, `iac_*`, `obs_*`) -> `devops_engineer`.\n"
        "     * Service databases (PostgreSQL, MySQL, MongoDB, Redis), REST/gRPC/GraphQL APIs, message brokers (Kafka, RabbitMQ), microservices implementation (`db_*`, `api_*`, `mq_*`) -> `backend_engineer` (unless explicitly in a DWH/ETL sentence for `data_engineer`).\n"
        "     * ETL/ELT pipelines, Airflow, DWH modeling, ClickHouse, analytical SQL (`etl_*`, `dwh_*`, `db_clickhouse`) -> `data_engineer`.\n"
        "     * Machine learning, PyTorch, scikit-learn, NLP, recommender systems, MLOps (`ml_*`) -> `ml_engineer`.\n"
        "     * UI/UX, Figma, wireframing, design systems (`ui_*`, `ux_*`) -> `ux_ui_designer`.\n"
        "     * Business requirements, BPMN, user stories, domain analysis (`an_requirements`, `an_bpmn`, `an_user_stories`, `an_sql_analysis`) -> `business_analyst`.\n"
        "     * System integration specs, API contracts (`an_integration`) -> `system_analyst`.\n"
        "   - Never invent a role without basis in the text.\n"
        "6. Implicit requirements: infer an implicit requirement ONLY from the closed policy list:\n"
        "   - Explicit mention of `PCI DSS` -> `role_id: 'security_engineer'`, `competency_id: 'sec_appsec'`, `needs_review: true`.\n"
        "   Do not invent any other unmentioned competencies.\n"
        "7. Deduplication: do not repeat the same `(role_id, competency_id)` pair. The same `competency_id` may be assigned to two different roles ONLY when the brief contains two separate quotes for those two roles.\n"
        "8. Set `needs_review: true` when confidence < 0.7, when the requirement is implicit (e.g. PCI DSS), when phrasing is ambiguous (e.g. 'пайплайны'), or when level and role conflict (e.g. junior level <= 2 for architect/lead).\n\n"
        "## Closed vocabulary — Roles (`role_id: name_ru (synonyms)`)\n"
        f"{roles_block}\n\n"
        "## Closed vocabulary — Competencies (`competency_id: name_ru (synonyms) -> roles: [allowed_roles]`)\n"
        f"{comps_block}\n\n"
        f"{_build_few_shot_examples()}\n\n"
        "Output ONLY the JSON array."
    )

    user_text = f"Brief text (locale={locale}):\n\n{brief_text}"

    return [
        {"role": "system", "content": system_text},
        {"role": "user", "content": user_text},
    ]
