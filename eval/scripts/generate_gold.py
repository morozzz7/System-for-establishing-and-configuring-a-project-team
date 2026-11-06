#!/usr/bin/env python3
"""
Helper script to generate gold.json files with verified character offsets.
Usage: This is a development tool; generates all gold briefs at once.
"""
import json
import os
from pathlib import Path

GOLD_DIR = Path(__file__).parent.parent / "gold"

def find_offset(text: str, quote: str, start_from: int = 0) -> tuple[int, int]:
    """Find quote in text starting from start_from. Returns (char_start, char_end)."""
    idx = text.find(quote, start_from)
    if idx == -1:
        raise ValueError(f"Quote not found: '{quote[:60]}...' in text (from pos {start_from})")
    return idx, idx + len(quote)


def make_evidence(text: str, quote: str, start_from: int = 0) -> dict:
    cs, ce = find_offset(text, quote, start_from)
    # Verify
    assert text[cs:ce] == quote, f"Mismatch: '{text[cs:ce]}' != '{quote}'"
    return {
        "kind": "brief_span",
        "quote": quote,
        "char_start": cs,
        "char_end": ce
    }


def write_brief(brief_id_num: int, slug: str, brief_text: str, gold_reqs: list, notes: str):
    """Write a complete brief directory."""
    dir_name = f"brief_{brief_id_num:03d}_{slug}"
    brief_dir = GOLD_DIR / dir_name
    brief_dir.mkdir(parents=True, exist_ok=True)

    # Write brief.md
    (brief_dir / "brief.md").write_text(brief_text, encoding="utf-8")

    # Write gold.json
    (brief_dir / "gold.json").write_text(
        json.dumps(gold_reqs, ensure_ascii=False, indent=2),
        encoding="utf-8"
    )

    # Write notes.md
    (brief_dir / "notes.md").write_text(notes, encoding="utf-8")

    print(f"  ✅ {dir_name}: {len(gold_reqs)} requirements")
    return dir_name


# =========================================================================
# BRIEF 001: Simple Web MVP (Web/Mobile, simple, no defects)
# =========================================================================
brief_001_text = """Проект «Альфа»

Необходимо разработать MVP веб-сервиса для управления задачамиsmall команды до 50 человек. Срок — 8 недель, команда 2-3 человека.

Бэкенд на Go (уровень middle), REST API, PostgreSQL. Фронтенд на React + TypeScript. Нужно покрыть тестами основной функционал (Playwright для E2E).

Стек: Go, React, TypeScript, PostgreSQL, Docker. Запуск в Yandex Cloud.

Основные функции: регистрация, CRUD задач, уведомления по email.
"""

brief_001_gold = [
    {
        "requirement_id": "req_001_001",
        "brief_id": "brf_001",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["8 weeks × 5 days = 40 working days; one full-time backend developer", "Effort = 80pd likely with 0.7-1.4 range"],
        "evidence": [make_evidence(brief_001_text, "Бэкенд на Go (уровень middle)")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_001_002",
        "brief_id": "brf_001",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["Database schema and queries included in backend effort, estimated separately for schema design"],
        "evidence": [make_evidence(brief_001_text, "PostgreSQL")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_001_003",
        "brief_id": "brf_001",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "api_rest", "name_ru": "REST API design", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["REST API design is part of backend work; estimated as separate competency requirement"],
        "evidence": [make_evidence(brief_001_text, "REST API")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_001_004",
        "brief_id": "brf_001",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Frontend work runs in parallel with backend", "8-week timeline, one frontend dev"],
        "evidence": [make_evidence(brief_001_text, "Фронтенд на React + TypeScript")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_001_005",
        "brief_id": "brf_001",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "lang_typescript", "name_ru": "TypeScript", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["TypeScript is used for both frontend and potentially some shared code"],
        "evidence": [make_evidence(brief_001_text, "TypeScript", find_offset(brief_001_text, "React + TypeScript")[0] + 5)],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_001_006",
        "brief_id": "brf_001",
        "role": {"role_id": "qa_engineer", "name_ru": "Тестировщик", "confidence": 0.85},
        "competency": {"competency_id": "test_playwright", "name_ru": "Playwright", "confidence": 0.85},
        "level": 2,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["Testing is part-time, covered by one of the developers or a dedicated QA at basic level"],
        "evidence": [make_evidence(brief_001_text, "Playwright для E2E")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_001_007",
        "brief_id": "brf_001",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.75},
        "competency": {"competency_id": "cloud_yandex", "name_ru": "Yandex Cloud", "confidence": 0.75},
        "level": 2,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 7, "likely_person_days": 10, "max_person_days": 14, "unit": "person_days"},
        "assumptions": ["Deployment to Yandex Cloud needed; DevOps effort is minimal for MVP"],
        "evidence": [make_evidence(brief_001_text, "Yandex Cloud")],
        "confidence": 0.75,
        "needs_review": False,
    },
]

brief_001_notes = """# Brief 001: Simple Web MVP

## Category
Web/Mobile product

## Complexity
Simple — 7 requirements, 4 roles.

## Planted defects
None — control case, purely feasible.

## Assumptions
- Team size 2-3 as stated in brief; effort estimates assume parallel work
- Go + PostgreSQL + React + TypeScript stack is explicit
- QA may be done by developers in MVP; listed as separate requirement for clarity
- Yandex Cloud deployment is basic (Docker containers, minimal IaC)
- Effort range: 8-week timeline × 1 person = 40pd; with range [0.7, 1.0, 1.4] applied per role

## Edge cases
- "middle" maps unambiguously to level 3
- TypeScript appears twice; both as frontend and general stack mention
"""


# =========================================================================
# BRIEF 002: DWH for retail (Data/Analytics)
# =========================================================================
brief_002_text = """Проект «Витрина»

Сеть магазинов «ПродуктМаркет» (200+ точек) запускает хранилище данных для аналитики продаж.
Текущие данные в MySQL и Excel-отчётах. Цель — единое хранилище на PostgreSQL + ClickHouse с витринами для BI.

Нужны: дата-инженер senior (Python, SQL, ETL через Airflow), бизнес-аналитик для сбора требований
от отдела продаж и построения отчётов. Срок — 4 месяца, бюджет — 3 человека.

BI-дашборды: 10-15 витрин, данные по продажам, остаткам, ABC-анализ.
Данные из 1С по выгрузке через API. Объём — до 50 ГБ в год.

Ожидается, что аналитик знает SQL на уровне уверенного написания запросов.
"""

brief_002_gold = [
    {
        "requirement_id": "req_002_001",
        "brief_id": "brf_002",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.95},
        "competency": {"competency_id": "lang_python", "name_ru": "Python", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["4 months × 21 days ≈ 84pd likely; senior = level 4", "Full-time engagement"],
        "evidence": [make_evidence(brief_002_text, "дата-инженер senior (Python")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_002_002",
        "brief_id": "brf_002",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.95},
        "competency": {"competency_id": "lang_sql", "name_ru": "SQL", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["SQL is core for DWH engineering; same person as Python DE"],
        "evidence": [make_evidence(brief_002_text, "SQL")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_002_003",
        "brief_id": "brf_002",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.90},
        "competency": {"competency_id": "etl_orchestration", "name_ru": "ETL и оркестрация", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Airflow for ETL orchestration; separate effort for pipeline development"],
        "evidence": [make_evidence(brief_002_text, "ETL через Airflow")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_002_004",
        "brief_id": "brf_002",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.90},
        "competency": {"competency_id": "an_requirements", "name_ru": "Работа с требованиями", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 28, "likely_person_days": 40, "max_person_days": 56, "unit": "person_days"},
        "assumptions": ["BA collects requirements from sales department; effort covers interviews + documentation"],
        "evidence": [make_evidence(brief_002_text, "бизнес-аналитик для сбора требований")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_002_005",
        "brief_id": "brf_002",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.85},
        "competency": {"competency_id": "an_sql_analysis", "name_ru": "Анализ данных через SQL", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["BA must write SQL queries for analysis; described as 'уверенный SQL'"],
        "evidence": [make_evidence(brief_002_text, "аналитик знает SQL на уровне уверенного написания запросов")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_002_006",
        "brief_id": "brf_002",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.85},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.85},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["PostgreSQL is the main DWH storage"],
        "evidence": [make_evidence(brief_002_text, "PostgreSQL")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_002_007",
        "brief_id": "brf_002",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.80},
        "competency": {"competency_id": "db_clickhouse", "name_ru": "ClickHouse", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["ClickHouse for analytical vitrines; separate from main PostgreSQL DWH"],
        "evidence": [make_evidence(brief_002_text, "ClickHouse")],
        "confidence": 0.80,
        "needs_review": False,
    },
]

brief_002_notes = """# Brief 002: DWH for Retail

## Category
Data/Analytics

## Complexity
Medium — 7 requirements, 2 roles.

## Planted defects
None — straightforward extraction.

## Assumptions
- "senior" = level 4
- "4 months" = ~84 working days; effort range 0.7-1.4× 
- Business analyst with SQL = both requirements engineering and SQL analysis competencies
- BI dashboards are output of the BA's work, not a separate tool requirement

## Edge cases
- MySQL mentioned as source system but not needed in team (source data, not target)
- 1С API — integration mentioned but not detailed enough for a separate requirement
"""


# =========================================================================
# BRIEF 003: Enterprise 1C Integration (Enterprise Integration)
# =========================================================================
brief_003_text = """Проект «Мост»

Компании необходимо интегрировать 1С:Предприятие (бухгалтерия и склад) с новой CRM-системой.
Обмен данными — через REST API, синхронизация номенклатуры, контрагентов и заказов.

Требуется системный аналитик (опыт интеграций, описание API в Swagger/OpenAPI) и
бэкенд-разработчик на Java (Spring Boot). Срок — 3 месяца.

Объём: около 20 эндпоинтов, нагрузка — до 1000 транзакций в час.
Нужно обеспечить идемпотентность и retry-механизм.
"""

brief_003_gold = [
    {
        "requirement_id": "req_003_001",
        "brief_id": "brf_003",
        "role": {"role_id": "system_analyst", "name_ru": "Системный аналитик", "confidence": 0.95},
        "competency": {"competency_id": "an_integration", "name_ru": "Анализ интеграций", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 30, "likely_person_days": 42, "max_person_days": 58, "unit": "person_days"},
        "assumptions": ["3 months × 21 days ≈ 63pd; SA does integration analysis + API spec"],
        "evidence": [make_evidence(brief_003_text, "системный аналитик (опыт интеграций")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_003_002",
        "brief_id": "brf_003",
        "role": {"role_id": "system_analyst", "name_ru": "Системный аналитик", "confidence": 0.90},
        "competency": {"competency_id": "api_rest", "name_ru": "REST API design", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["SA designs the API specification in OpenAPI format"],
        "evidence": [make_evidence(brief_003_text, "описание API в Swagger/OpenAPI")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_003_003",
        "brief_id": "brf_003",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_java", "name_ru": "Java", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Java backend developer for integration layer; 3 months full-time"],
        "evidence": [make_evidence(brief_003_text, "бэкенд-разработчик на Java")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_003_004",
        "brief_id": "brf_003",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "fw_spring", "name_ru": "Spring Boot", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Spring Boot is the framework for Java backend; same person as lang_java"],
        "evidence": [make_evidence(brief_003_text, "Spring Boot")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_003_005",
        "brief_id": "brf_003",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.80},
        "competency": {"competency_id": "arch_integration_patterns", "name_ru": "Паттерны интеграции", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 10, "likely_person_days": 15, "max_person_days": 21, "unit": "person_days"},
        "assumptions": ["Idempotency and retry patterns are integration patterns; implied by 'идемпотентность и retry-механизм'"],
        "evidence": [make_evidence(brief_003_text, "идемпотентность и retry-механизм")],
        "confidence": 0.80,
        "needs_review": False,
    },
]

brief_003_notes = """# Brief 003: Enterprise 1C Integration

## Category
Enterprise Integration

## Complexity
Medium — 5 requirements, 2 roles.

## Planted defects
None — straightforward.

## Assumptions
- No DevOps required (implied: existing infra or simple Docker deployment)
- 1C is the existing system, not requiring 1C specialists on the team (API-level integration)
- "20 endpoints" → informs effort but doesn't create separate requirements per endpoint

## Edge cases
- 1C itself is not in the taxonomy as a competency (it's a source system, not a team skill)
- Swagger/OpenAPI → maps to api_rest competency
"""


# =========================================================================
# BRIEF 004: Kubernetes Migration (Infrastructure/DevOps)
# =========================================================================
brief_004_text = """Проект «Контейнер»

Перевести микросервисную платформу (12 сервисов на Go + Python) с виртуальных серверов в Kubernetes.
Облако — Yandex Cloud. Нужен DevOps-инженер уровня strong middle / senior.

Задачи: описать манифесты в Helm, настроить CI/CD в GitLab, мониторинг через Prometheus + Grafana.
Покрыть сервисы трассировкой (OpenTelemetry).

Срок — 3 месяца. Один человек full-time.
"""

brief_004_gold = [
    {
        "requirement_id": "req_004_001",
        "brief_id": "brf_004",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.95},
        "competency": {"competency_id": "cloud_kubernetes", "name_ru": "Kubernetes", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["3 months × 21 days = 63pd; strong middle/senior = level 4"],
        "evidence": [make_evidence(brief_004_text, "Kubernetes")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_004_002",
        "brief_id": "brf_004",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "iac_terraform", "name_ru": "Terraform", "confidence": 0.70},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["Terraform not explicitly mentioned but implied for Yandex Cloud IaC; needs_review due to inference"],
        "evidence": [make_evidence(brief_004_text, "Yandex Cloud")],
        "confidence": 0.65,
        "needs_review": True,
    },
    {
        "requirement_id": "req_004_003",
        "brief_id": "brf_004",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cicd_gitlab", "name_ru": "GitLab CI", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["CI/CD pipeline configuration in GitLab"],
        "evidence": [make_evidence(brief_004_text, "CI/CD в GitLab")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_004_004",
        "brief_id": "brf_004",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "obs_prometheus", "name_ru": "Prometheus", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 10, "likely_person_days": 15, "max_person_days": 21, "unit": "person_days"},
        "assumptions": ["Monitoring stack setup"],
        "evidence": [make_evidence(brief_004_text, "Prometheus")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_004_005",
        "brief_id": "brf_004",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "obs_grafana", "name_ru": "Grafana", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 10, "likely_person_days": 15, "max_person_days": 21, "unit": "person_days"},
        "assumptions": ["Grafana dashboards for monitoring"],
        "evidence": [make_evidence(brief_004_text, "Grafana")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_004_006",
        "brief_id": "brf_004",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "obs_opentelemetry", "name_ru": "OpenTelemetry", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["Distributed tracing via OpenTelemetry for 12 services"],
        "evidence": [make_evidence(brief_004_text, "OpenTelemetry")],
        "confidence": 0.95,
        "needs_review": False,
    },
]

brief_004_notes = """# Brief 004: Kubernetes Migration

## Category
Infrastructure/DevOps

## Complexity
Medium — 6 requirements, 1 role.

## Planted defects
None — straightforward DevOps brief.

## Assumptions
- Terraform implied but not explicit → needs_review: true
- "strong middle / senior" → level 4 (closer to senior)
- Helm manifests are part of Kubernetes work, not a separate competency
- One person full-time for 3 months

## Edge cases
- Go and Python mentioned as existing services but not as team requirements
- Helm is a synonym for Kubernetes in the taxonomy
"""


# =========================================================================
# BRIEF 005: Legacy Modernization (Legacy Modernization)
# =========================================================================
brief_005_text = """Проект «Реновация»

Модернизация легаси-системы учёта заявок: переписать с Java 8 + JSP на современный стек.
Новый бэкенд — Go (микросервисы), фронтенд — React. Старая система должна работать параллельно
до полного перехода (стратегия Strangler Fig).

Команда: 2 бэкенд-разработчика (Go, senior), 1 фронтенд (React, middle), 1 DevOps
для настройки CI/CD и контейнеризации. Срок — 6 месяцев.

БД: миграция с Oracle на PostgreSQL. Архитектор для проектирования микросервисной архитектуры.
"""

brief_005_gold = [
    {
        "requirement_id": "req_005_001",
        "brief_id": "brf_005",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 126, "likely_person_days": 180, "max_person_days": 252, "unit": "person_days"},
        "assumptions": ["6 months × 21 days × 2 people = 252pd max; headcount=2", "Senior = level 4"],
        "evidence": [make_evidence(brief_005_text, "2 бэкенд-разработчика (Go, senior)")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_005_002",
        "brief_id": "brf_005",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "arch_microservices", "name_ru": "Микросервисы", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Microservices architecture for the new system; same backend developers"],
        "evidence": [make_evidence(brief_005_text, "микросервисы")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_005_003",
        "brief_id": "brf_005",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["6 months × 21 days = 126pd; middle = level 3"],
        "evidence": [make_evidence(brief_005_text, "1 фронтенд (React, middle)")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_005_004",
        "brief_id": "brf_005",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cicd_gitlab", "name_ru": "GitLab CI", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["CI/CD setup; GitLab not explicitly mentioned but typical for such projects"],
        "evidence": [make_evidence(brief_005_text, "CI/CD и контейнеризации")],
        "confidence": 0.75,
        "needs_review": False,
    },
    {
        "requirement_id": "req_005_005",
        "brief_id": "brf_005",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.85},
        "competency": {"competency_id": "cloud_docker", "name_ru": "Docker", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["Containerization via Docker; part of DevOps work"],
        "evidence": [make_evidence(brief_005_text, "контейнеризации")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_005_006",
        "brief_id": "brf_005",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["PostgreSQL migration from Oracle; effort for schema migration and query adaptation"],
        "evidence": [make_evidence(brief_005_text, "PostgreSQL")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_005_007",
        "brief_id": "brf_005",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.90},
        "competency": {"competency_id": "arch_system_design", "name_ru": "Проектирование систем", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 28, "likely_person_days": 40, "max_person_days": 56, "unit": "person_days"},
        "assumptions": ["Architect for microservices design; not full-time, ~2 days/week for 6 months"],
        "evidence": [make_evidence(brief_005_text, "Архитектор для проектирования микросервисной архитектуры")],
        "confidence": 0.90,
        "needs_review": False,
    },
]

brief_005_notes = """# Brief 005: Legacy Modernization

## Category
Legacy Modernization

## Complexity
Medium — 7 requirements, 4 roles.

## Planted defects
None.

## Assumptions
- Strangler Fig pattern mentioned → implies phased approach, effort spread over 6 months
- Oracle mentioned as source DB but no Oracle specialist needed (migration TO PostgreSQL)
- Architect is part-time (20-30% allocation)
- Java/JSP is legacy, not needed in the new team

## Edge cases
- "Go, senior" and "React, middle" — level mapping from brief language
- CI/CD tool not specified (GitLab assumed as typical)
- Headcount=2 for backend developer role
"""


# =========================================================================
# BRIEF 006: ML Recommendation Pipeline (Data/Analytics, PLANTED: must-role deficit)
# =========================================================================
brief_006_text = """Проект «Рекомендация»

Маркетплейс запускает систему рекомендаций товаров. Нужны: ML-инженер senior (Python, PyTorch),
дата-инженер (Python, Airflow, Kafka), бизнес-аналитик для определения метрик качества.

Особенность: нужен специалист по NLP для обработки описаний товаров (векторные эмбеддинги,
семантический поиск). Такого специалиста в компании нет — требуется внешний консультант
или найм.

Срок — 5 месяцев. Данные: каталог 100K товаров, история покупок 2M записей.
Хранилище — ClickHouse + PostgreSQL.
"""

brief_006_gold = [
    {
        "requirement_id": "req_006_001",
        "brief_id": "brf_006",
        "role": {"role_id": "ml_engineer", "name_ru": "ML-инженер", "confidence": 0.95},
        "competency": {"competency_id": "lang_python", "name_ru": "Python", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 70, "likely_person_days": 100, "max_person_days": 140, "unit": "person_days"},
        "assumptions": ["5 months × 21 days ≈ 105pd; senior = level 4"],
        "evidence": [make_evidence(brief_006_text, "ML-инженер senior (Python")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_002",
        "brief_id": "brf_006",
        "role": {"role_id": "ml_engineer", "name_ru": "ML-инженер", "confidence": 0.95},
        "competency": {"competency_id": "ml_pytorch", "name_ru": "PyTorch", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["PyTorch for recommendation model development"],
        "evidence": [make_evidence(brief_006_text, "PyTorch")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_003",
        "brief_id": "brf_006",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.95},
        "competency": {"competency_id": "lang_python", "name_ru": "Python", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 70, "likely_person_days": 100, "max_person_days": 140, "unit": "person_days"},
        "assumptions": ["Data engineer for pipeline development; middle level assumed"],
        "evidence": [make_evidence(brief_006_text, "дата-инженер (Python")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_004",
        "brief_id": "brf_006",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.90},
        "competency": {"competency_id": "etl_orchestration", "name_ru": "ETL и оркестрация", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Airflow for data pipeline orchestration"],
        "evidence": [make_evidence(brief_006_text, "Airflow")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_005",
        "brief_id": "brf_006",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.85},
        "competency": {"competency_id": "mq_kafka", "name_ru": "Apache Kafka", "confidence": 0.85},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["Kafka for streaming data from the marketplace to ML pipeline"],
        "evidence": [make_evidence(brief_006_text, "Kafka")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_006",
        "brief_id": "brf_006",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.85},
        "competency": {"competency_id": "an_requirements", "name_ru": "Работа с требованиями", "confidence": 0.85},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["BA for defining quality metrics; part-time role"],
        "evidence": [make_evidence(brief_006_text, "бизнес-аналитик для определения метрик качества")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_007",
        "brief_id": "brf_006",
        "role": {"role_id": "ml_engineer", "name_ru": "ML-инженер", "confidence": 0.80},
        "competency": {"competency_id": "ml_nlp", "name_ru": "Обработка естественного языка", "confidence": 0.80},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["NLP specialist for product description embeddings; external hire needed", "DEFICIT: no such specialist in the company"],
        "evidence": [make_evidence(brief_006_text, "специалист по NLP для обработки описаний товаров")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_008",
        "brief_id": "brf_006",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.80},
        "competency": {"competency_id": "db_clickhouse", "name_ru": "ClickHouse", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["ClickHouse for analytical data store"],
        "evidence": [make_evidence(brief_006_text, "ClickHouse")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_006_009",
        "brief_id": "brf_006",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.80},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["PostgreSQL for operational data"],
        "evidence": [make_evidence(brief_006_text, "PostgreSQL", find_offset(brief_006_text, "ClickHouse + PostgreSQL")[0])],
        "confidence": 0.80,
        "needs_review": False,
    },
]

brief_006_notes = """# Brief 006: ML Recommendation Pipeline

## Category
Data/Analytics

## Complexity
Complex — 9 requirements, 3 roles.

## Planted defects
**MUST-ROLE DEFICIT**: NLP specialist (ml_nlp competency, level 4) — brief explicitly states
"Такого специалиста в компании нет". This requires external cooperation (hire/contractor).

## Assumptions
- 5-month timeline × 21 days = 105pd for main ML engineer
- NLP specialist is a separate requirement even though it may be the same person
- Kafka for streaming product events
- BA role is part-time (~30% allocation)

## Edge cases
- NLP specialist deficit is a known gap; system should flag this for external cooperation
- "семантический поиск" → maps to ml_nlp competency
- PyTorch and NLP are related but tracked as separate competencies
"""


# =========================================================================
# BRIEF 007: BI Analytics Dashboards (Data/Analytics, simple)
# =========================================================================
brief_007_text = """Проект «Дашборд»

Отдел продаж заказывает набор BI-дашбордов. Данные уже в ClickHouse. Нужно: аналитик,
который построит 8 дашбордов (продажи по регионам, воронка, ABC/XYZ-анализ).

Инструмент: Superset или Metabase. Срок — 1.5 месяца. Один человек.

Аналитик должен уверенно знать SQL и уметь работать с бизнес-пользователями.
"""

brief_007_gold = [
    {
        "requirement_id": "req_007_001",
        "brief_id": "brf_007",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.85},
        "competency": {"competency_id": "an_sql_analysis", "name_ru": "Анализ данных через SQL", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["1.5 months × 21 days ≈ 31pd; one person full-time"],
        "evidence": [make_evidence(brief_007_text, "аналитик")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_007_002",
        "brief_id": "brf_007",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.85},
        "competency": {"competency_id": "an_requirements", "name_ru": "Работа с требованиями", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 7, "likely_person_days": 10, "max_person_days": 14, "unit": "person_days"},
        "assumptions": ["Working with business users = requirement gathering; part of BA role"],
        "evidence": [make_evidence(brief_007_text, "уметь работать с бизнес-пользователями")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_007_003",
        "brief_id": "brf_007",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.75},
        "competency": {"competency_id": "db_clickhouse", "name_ru": "ClickHouse", "confidence": 0.80},
        "level": 2,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 5, "likely_person_days": 7, "max_person_days": 10, "unit": "person_days"},
        "assumptions": ["Data already in ClickHouse; minimal DE effort for data prep"],
        "evidence": [make_evidence(brief_007_text, "Данные уже в ClickHouse")],
        "confidence": 0.70,
        "needs_review": False,
    },
]

brief_007_notes = """# Brief 007: BI Analytics Dashboards

## Category
Data/Analytics

## Complexity
Simple — 3 requirements, 2 roles.

## Planted defects
None.

## Assumptions
- Superset/Metabase are BI tools, not competencies in the taxonomy
- ClickHouse already set up; minimal data engineering effort
- BA does both SQL analysis and requirement gathering

## Edge cases
- "аналитик" ambiguous: could be data_engineer or business_analyst → context (dashboards, business users) → BA
- BI tool choice (Superset vs Metabase) doesn't create a separate requirement
"""


# =========================================================================
# BRIEF 008: Mobile Banking App (Web/Mobile, complex, PCI DSS)
# =========================================================================
brief_008_text = """Проект «МобилБанк»

Разработка мобильного приложения для регионального банка. Платформы: iOS и Android (React Native).
Бэкенд — Go (новые микросервисы) + интеграция с существующим Java-ядром через REST.

Команда: 2 мобильных разработчика (React Native), 2 бэкенд-разработчика (Go, senior),
1 DevOps (Kubernetes, GitLab CI), 1 QA (автотесты), 1 системный аналитик.

Срок — 8 месяцев. Обязательно соответствие PCI DSS и 152-ФЗ.

Функции: переводы, оплата ЖКХ, открытия вкладов, push-уведомления.
"""

brief_008_gold = [
    {
        "requirement_id": "req_008_001",
        "brief_id": "brf_008",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 224, "likely_person_days": 320, "max_person_days": 448, "unit": "person_days"},
        "assumptions": ["8 months × 21 days × 2 = 336pd; React Native = React competency", "Mobile platforms need 2 developers"],
        "evidence": [make_evidence(brief_008_text, "2 мобильных разработчика (React Native)")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_002",
        "brief_id": "brf_008",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 224, "likely_person_days": 320, "max_person_days": 448, "unit": "person_days"},
        "assumptions": ["8 months × 21 days × 2 = 336pd; senior = level 4"],
        "evidence": [make_evidence(brief_008_text, "2 бэкенд-разработчика (Go, senior)")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_003",
        "brief_id": "brf_008",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "arch_microservices", "name_ru": "Микросервисы", "confidence": 0.85},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 112, "likely_person_days": 160, "max_person_days": 224, "unit": "person_days"},
        "assumptions": ["New microservices for mobile backend"],
        "evidence": [make_evidence(brief_008_text, "новые микросервисы")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_004",
        "brief_id": "brf_008",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cloud_kubernetes", "name_ru": "Kubernetes", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 112, "likely_person_days": 160, "max_person_days": 224, "unit": "person_days"},
        "assumptions": ["Kubernetes for deployment; full-time DevOps for 8 months"],
        "evidence": [make_evidence(brief_008_text, "Kubernetes", find_offset(brief_008_text, "1 DevOps (Kubernetes")[0])],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_005",
        "brief_id": "brf_008",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cicd_gitlab", "name_ru": "GitLab CI", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["GitLab CI for CI/CD pipelines"],
        "evidence": [make_evidence(brief_008_text, "GitLab CI")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_006",
        "brief_id": "brf_008",
        "role": {"role_id": "qa_engineer", "name_ru": "Тестировщик", "confidence": 0.90},
        "competency": {"competency_id": "test_playwright", "name_ru": "Playwright", "confidence": 0.70},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Automated tests for mobile app; Playwright or similar tool", "QA full-time for 8 months"],
        "evidence": [make_evidence(brief_008_text, "1 QA (автотесты)")],
        "confidence": 0.70,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_007",
        "brief_id": "brf_008",
        "role": {"role_id": "system_analyst", "name_ru": "Системный аналитик", "confidence": 0.90},
        "competency": {"competency_id": "an_integration", "name_ru": "Анализ интеграций", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["SA for integration with existing Java core; REST API design"],
        "evidence": [make_evidence(brief_008_text, "1 системный аналитик")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_008_008",
        "brief_id": "brf_008",
        "role": {"role_id": "security_engineer", "name_ru": "Инженер по безопасности", "confidence": 0.75},
        "competency": {"competency_id": "sec_appsec", "name_ru": "Безопасность приложений", "confidence": 0.75},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 28, "likely_person_days": 40, "max_person_days": 56, "unit": "person_days"},
        "assumptions": ["PCI DSS compliance requires application security expertise", "IMPLICIT: security engineer needed even though not explicitly listed as a role", "Part-time engagement (~20%)"],
        "evidence": [make_evidence(brief_008_text, "PCI DSS")],
        "confidence": 0.75,
        "needs_review": False,
    },
]

brief_008_notes = """# Brief 008: Mobile Banking App

## Category
Web/Mobile product (Fintech)

## Complexity
Complex — 8 requirements, 6 roles.

## Planted defects
**IMPLICIT REQUIREMENT**: "PCI DSS" mentioned → implies need for security_engineer with sec_appsec
competency, even though no security role is explicitly listed in the team.
"152-ФЗ" (Russian personal data law) reinforces the security need.

## Assumptions
- React Native → mapped to fw_react (closest in taxonomy)
- Java core is existing, not a new team requirement
- 8 months × 21 days = 168pd per person
- Security engineer is part-time (~20% allocation)
- QA needs automated testing (автотесты) but tool not specified → Playwright assumed

## Edge cases
- React Native not in taxonomy → mapped to React as closest match
- "152-ФЗ" is implicit security requirement (Russian data protection law)
- Team size explicitly listed: 2+2+1+1+1 = 7 people minimum
"""


# =========================================================================
# BRIEF 009: ESB Messaging Platform (Enterprise Integration)
# =========================================================================
brief_009_text = """Проект «Шина»

Крупный ритейлер строит корпоративную шину данных (ESB) для обмена сообщениями между
15 системами (1С, CRM, WMS, сайт). Паттерны: pub/sub, request/reply, outbox.

Нужны: архитектор (событийная архитектура, Kafka), 2 бэкенд-разработчика (Go или Java),
системный аналитик (интеграционные спецификации).

Срок — 10 месяцев. Нагрузка: до 50K сообщений в минуту.
"""

brief_009_gold = [
    {
        "requirement_id": "req_009_001",
        "brief_id": "brf_009",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.90},
        "competency": {"competency_id": "arch_event_driven", "name_ru": "Событийная архитектура", "confidence": 0.90},
        "level": 5,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 140, "likely_person_days": 200, "max_person_days": 280, "unit": "person_days"},
        "assumptions": ["10 months × 21 days ≈ 210pd; architect = level 5", "Part-time ~50% = ~100pd actual"],
        "evidence": [make_evidence(brief_009_text, "архитектор (событийная архитектура")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_009_002",
        "brief_id": "brf_009",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.85},
        "competency": {"competency_id": "mq_kafka", "name_ru": "Apache Kafka", "confidence": 0.85},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Kafka as the messaging backbone; architect must know Kafka"],
        "evidence": [make_evidence(brief_009_text, "Kafka")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_009_003",
        "brief_id": "brf_009",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.80},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 196, "likely_person_days": 280, "max_person_days": 392, "unit": "person_days"},
        "assumptions": ["Go or Java → Go chosen as primary; 2 people × 10 months = 420pd max", "Headcount = 2"],
        "evidence": [make_evidence(brief_009_text, "2 бэкенд-разработчика (Go или Java)")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_009_004",
        "brief_id": "brf_009",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.70},
        "competency": {"competency_id": "lang_java", "name_ru": "Java", "confidence": 0.70},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 98, "likely_person_days": 140, "max_person_days": 196, "unit": "person_days"},
        "assumptions": ["Java as alternative; brief says 'Go or Java' → need at least one", "Marked as alternative to Go"],
        "evidence": [make_evidence(brief_009_text, "Go или Java")],
        "confidence": 0.65,
        "needs_review": True,
    },
    {
        "requirement_id": "req_009_005",
        "brief_id": "brf_009",
        "role": {"role_id": "system_analyst", "name_ru": "Системный аналитик", "confidence": 0.90},
        "competency": {"competency_id": "an_integration", "name_ru": "Анализ интеграций", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 98, "likely_person_days": 140, "max_person_days": 196, "unit": "person_days"},
        "assumptions": ["SA writes integration specs for 15 systems"],
        "evidence": [make_evidence(brief_009_text, "системный аналитик (интеграционные спецификации)")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_009_006",
        "brief_id": "brf_009",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.80},
        "competency": {"competency_id": "arch_integration_patterns", "name_ru": "Паттерны интеграции", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 2,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["pub/sub, request/reply, outbox are integration patterns"],
        "evidence": [make_evidence(brief_009_text, "pub/sub, request/reply, outbox")],
        "confidence": 0.80,
        "needs_review": False,
    },
]

brief_009_notes = """# Brief 009: ESB Messaging Platform

## Category
Enterprise Integration

## Complexity
Complex — 6 requirements, 3 roles.

## Planted defects
- "Go or Java" — ambiguous language choice; we annotate Go as primary and Java as alternative (needs_review)

## Assumptions
- Architect is level 5 (expert) for enterprise-level ESB
- 10 months timeline
- 50K msg/min = high-load, but load testing is not a separate requirement here

## Edge cases
- "Go or Java" → ambiguity in competency; both listed
- Outbox pattern → maps to arch_integration_patterns
- 15 systems to integrate → informs effort but not separate requirements
"""


# =========================================================================
# BRIEF 010: DevOps Platform (Infrastructure/DevOps)
# =========================================================================
brief_010_text = """Проект «Платформа»

Внутренняя DevOps-платформа для компании (200 разработчиков). Нужно:
— DevOps-инженер senior (Kubernetes, Terraform, GitLab CI)
— Бэкенд-разработчик Go (внутренний self-service портал)
— Фронтенд-разработчик React (UI портала)

Задачи: внутренние CLI-утилиты, шаблоны сервисов, автоматизация онбординга.
Срок — 6 месяцев. Команда — 3 человека.
"""

brief_010_gold = [
    {
        "requirement_id": "req_010_001",
        "brief_id": "brf_010",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.95},
        "competency": {"competency_id": "cloud_kubernetes", "name_ru": "Kubernetes", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["6 months × 21 days = 126pd; senior = level 4"],
        "evidence": [make_evidence(brief_010_text, "DevOps-инженер senior (Kubernetes")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_010_002",
        "brief_id": "brf_010",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "iac_terraform", "name_ru": "Terraform", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Terraform for IaC; same DevOps person"],
        "evidence": [make_evidence(brief_010_text, "Terraform")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_010_003",
        "brief_id": "brf_010",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cicd_gitlab", "name_ru": "GitLab CI", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["GitLab CI for template pipelines"],
        "evidence": [make_evidence(brief_010_text, "GitLab CI")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_010_004",
        "brief_id": "brf_010",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Go backend for self-service portal; middle level"],
        "evidence": [make_evidence(brief_010_text, "Бэкенд-разработчик Go")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_010_005",
        "brief_id": "brf_010",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["React frontend for portal UI"],
        "evidence": [make_evidence(brief_010_text, "Фронтенд-разработчик React")],
        "confidence": 0.90,
        "needs_review": False,
    },
]

brief_010_notes = """# Brief 010: DevOps Platform

## Category
Infrastructure/DevOps

## Complexity
Medium — 5 requirements, 3 roles.

## Planted defects
None.

## Assumptions
- Internal platform for 200 developers
- Self-service portal needs both backend and frontend
- All 3 people full-time for 6 months

## Edge cases
- CLI utilities → DevOps skill, not a separate requirement
- "шаблоны сервисов" → part of Kubernetes + Terraform work
"""


# =========================================================================
# BRIEF 011: PLANTED DEFECT — Ambiguous term
# =========================================================================
brief_011_text = """Проект «Пайплайн»

Нужно настроить пайплайны для команды из 15 разработчиков. Пайплайны должны включать
сборку, тесты, деплой в staging и production. Также нужна настройка пайплайнов данных
для ежедневной загрузки из внешних источников.

Технологии: GitLab, Docker, Python. Один DevOps-инженер middle на 2 месяца.
"""

brief_011_gold = [
    {
        "requirement_id": "req_011_001",
        "brief_id": "brf_011",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cicd_gitlab", "name_ru": "GitLab CI", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 28, "likely_person_days": 40, "max_person_days": 56, "unit": "person_days"},
        "assumptions": ["CI/CD pipelines in GitLab for 15 developers", "2 months × 21 days ≈ 42pd; middle = level 3"],
        "evidence": [make_evidence(brief_011_text, "пайплайны для команды из 15 разработчиков")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_011_002",
        "brief_id": "brf_011",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.85},
        "competency": {"competency_id": "cloud_docker", "name_ru": "Docker", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["Docker for containerized builds and deploys"],
        "evidence": [make_evidence(brief_011_text, "Docker")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_011_003",
        "brief_id": "brf_011",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.65},
        "competency": {"competency_id": "etl_orchestration", "name_ru": "ETL и оркестрация", "confidence": 0.60},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["AMBIGUOUS: 'пайплайны данных' could be ETL (data_engineer) or CI/CD (devops)", "Annotated as data pipeline → etl_orchestration", "Same person may handle both if they have the skills"],
        "evidence": [make_evidence(brief_011_text, "пайплайнов данных\nдля ежедневной загрузки")],
        "confidence": 0.60,
        "needs_review": True,
    },
    {
        "requirement_id": "req_011_004",
        "brief_id": "brf_011",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.75},
        "competency": {"competency_id": "lang_python", "name_ru": "Python", "confidence": 0.70},
        "level": 2,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 7, "likely_person_days": 10, "max_person_days": 14, "unit": "person_days"},
        "assumptions": ["Python used for data loading scripts; basic level sufficient"],
        "evidence": [make_evidence(brief_011_text, "Python")],
        "confidence": 0.65,
        "needs_review": True,
    },
]

brief_011_notes = """# Brief 011: PLANTED DEFECT — Ambiguous "пайплайны"

## Category
Infrastructure/DevOps + Data (ambiguous)

## Complexity
Simple — 4 requirements, 2 roles.

## Planted defect: AMBIGUOUS TERM
The word "пайплайны" is used twice with different meanings:
1. CI/CD pipelines (DevOps) — GitLab CI
2. Data pipelines (Data Engineering) — ETL

An annotator must decide based on context:
- First "пайплайны" + "сборку, тесты, деплой" → CI/CD → cicd_gitlab
- Second "пайплайны данных" + "загрузка из внешних источников" → ETL → etl_orchestration

The second interpretation is less certain → needs_review: true, confidence 0.60.

## Alternative interpretation
A single DevOps engineer could handle both, but the competencies are different.
We annotate them as separate requirements with the same headcount=1.

## Annotator decision
- CI/CD pipeline → devops_engineer / cicd_gitlab (clear)
- Data pipeline → data_engineer / etl_orchestration (ambiguous, needs_review)
"""


# =========================================================================
# BRIEF 012: PLANTED DEFECT — Implicit PCI DSS requirement
# =========================================================================
brief_012_text = """Проект «Платёж»

Платёжный сервис для интернет-магазина. Приём оплат картами, рекуррентные платежи,
возвраты. Система должна полностью соответствовать PCI DSS level 1.

Бэкенд: Go, микросервисы, PostgreSQL. Фронтенд: React. Интеграция с платёжным шлюзом.

Команда: 2 бэкенд-разработчика senior, 1 фронтенд middle, тестировщик.
Срок — 4 месяца.
"""

brief_012_gold = [
    {
        "requirement_id": "req_012_001",
        "brief_id": "brf_012",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 112, "likely_person_days": 160, "max_person_days": 224, "unit": "person_days"},
        "assumptions": ["4 months × 21 days × 2 = 168pd; senior = level 4"],
        "evidence": [make_evidence(brief_012_text, "2 бэкенд-разработчика senior")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_012_002",
        "brief_id": "brf_012",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "arch_microservices", "name_ru": "Микросервисы", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["Microservices architecture for payment system"],
        "evidence": [make_evidence(brief_012_text, "микросервисы")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_012_003",
        "brief_id": "brf_012",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["PostgreSQL for transaction data"],
        "evidence": [make_evidence(brief_012_text, "PostgreSQL")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_012_004",
        "brief_id": "brf_012",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["React frontend for payment UI; middle = level 3"],
        "evidence": [make_evidence(brief_012_text, "Фронтенд: React")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_012_005",
        "brief_id": "brf_012",
        "role": {"role_id": "qa_engineer", "name_ru": "Тестировщик", "confidence": 0.85},
        "competency": {"competency_id": "qa_test_design", "name_ru": "Тест-дизайн", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["QA for payment flows; test design for financial transactions"],
        "evidence": [make_evidence(brief_012_text, "тестировщик")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_012_006",
        "brief_id": "brf_012",
        "role": {"role_id": "security_engineer", "name_ru": "Инженер по безопасности", "confidence": 0.70},
        "competency": {"competency_id": "sec_appsec", "name_ru": "Безопасность приложений", "confidence": 0.70},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 28, "likely_person_days": 40, "max_person_days": 56, "unit": "person_days"},
        "assumptions": ["IMPLICIT: PCI DSS level 1 requires application security expertise", "Security engineer not explicitly listed but implied by compliance requirement", "Part-time engagement (~25%)"],
        "evidence": [make_evidence(brief_012_text, "PCI DSS level 1")],
        "confidence": 0.70,
        "needs_review": False,
    },
    {
        "requirement_id": "req_012_007",
        "brief_id": "brf_012",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.75},
        "competency": {"competency_id": "dom_fintech", "name_ru": "Финтех-домен", "confidence": 0.75},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["IMPLICIT: fintech domain knowledge needed for payment processing", "Payment gateway integration requires domain expertise"],
        "evidence": [make_evidence(brief_012_text, "Платёжный сервис для интернет-магазина")],
        "confidence": 0.70,
        "needs_review": False,
    },
]

brief_012_notes = """# Brief 012: PLANTED DEFECT — Implicit PCI DSS requirement

## Category
Web/Mobile (Fintech/Payments)

## Complexity
Medium — 7 requirements, 5 roles.

## Planted defect: IMPLICIT REQUIREMENT
"PCI DSS level 1" implies the need for a security engineer (sec_appsec competency)
even though no security role is mentioned in the team list.

The naive extractor might miss this because:
- The team list is explicit: "2 бэкенд-разработчика senior, 1 фронтенд middle, тестировщик"
- Security is not in the team list
- But PCI DSS compliance REQUIRES security expertise

## Correct annotation
- security_engineer / sec_appsec / level 4 / must / needs_review (due to inference)
- Additionally: dom_fintech as domain competency for backend

## Annotator decision
- Include security_engineer because PCI DSS is a hard compliance requirement
- Mark confidence at 0.70 because the role is not explicitly listed
- The system should flag this for human review
"""


# =========================================================================
# BRIEF 013: PLANTED DEFECT — Contradictory (junior + architect)
# =========================================================================
brief_013_text = """Проект «Старт»

Нужен Go-разработчик junior, который будет также проектировать архитектуру системы.
Система — микросервисный бэкенд для стартапа. БД — PostgreSQL.

Плюс нужен фронтенд на React. Срок — 3 месяца, бюджет ограничен.
"""

brief_013_gold = [
    {
        "requirement_id": "req_013_001",
        "brief_id": "brf_013",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.90},
        "level": 2,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["3 months × 21 days = 63pd; junior = level 2"],
        "evidence": [make_evidence(brief_013_text, "Go-разработчик junior")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_013_002",
        "brief_id": "brf_013",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.70},
        "competency": {"competency_id": "arch_system_design", "name_ru": "Проектирование систем", "confidence": 0.70},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 14, "likely_person_days": 20, "max_person_days": 28, "unit": "person_days"},
        "assumptions": ["CONTRADICTION: brief says 'junior who also architects' — split into two requirements", "Junior (level 2) cannot architect → separate solution_architect requirement at level 4", "Architect effort is part-time (~30%)"],
        "evidence": [make_evidence(brief_013_text, "проектировать архитектуру системы")],
        "confidence": 0.65,
        "needs_review": True,
    },
    {
        "requirement_id": "req_013_003",
        "brief_id": "brf_013",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "arch_microservices", "name_ru": "Микросервисы", "confidence": 0.80},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["Microservices architecture; same contradiction as above", "Needs someone who can design microservices — not a junior task"],
        "evidence": [make_evidence(brief_013_text, "микросервисный бэкенд")],
        "confidence": 0.75,
        "needs_review": False,
    },
    {
        "requirement_id": "req_013_004",
        "brief_id": "brf_013",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.85},
        "level": 2,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 10, "likely_person_days": 15, "max_person_days": 21, "unit": "person_days"},
        "assumptions": ["PostgreSQL for data storage; junior can handle basic queries"],
        "evidence": [make_evidence(brief_013_text, "PostgreSQL")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_013_005",
        "brief_id": "brf_013",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["React frontend; middle level assumed (not specified)"],
        "evidence": [make_evidence(brief_013_text, "фронтенд на React")],
        "confidence": 0.90,
        "needs_review": False,
    },
]

brief_013_notes = """# Brief 013: PLANTED DEFECT — Contradictory (junior + architect)

## Category
Web/Mobile (Startup)

## Complexity
Simple — 5 requirements, 3 roles.

## Planted defect: CONTRADICTORY STATEMENT
"Go-разработчик junior, который будет также проектировать архитектуру системы"

This is a contradiction:
- "junior" = level 2 (basic, needs mentoring)
- "проектировать архитектуру" = level 4-5 (advanced/expert)

## Resolution
Split into two separate requirements:
1. backend_engineer / lang_go / level 2 (the junior coding part)
2. solution_architect / arch_system_design / level 4 (the architecture part)

Both with the same evidence span. The architect requirement is marked needs_review: true
because the brief explicitly assigns this to a junior (which is unrealistic).

## Annotator decision
- Create both requirements from the same sentence
- Mark the architect requirement as needs_review because the brief's expectation is unrealistic
- In practice, this would be 2 people, or 1 senior person
"""


# =========================================================================
# BRIEF 014: Complex Portal (Web/Mobile, many roles)
# =========================================================================
brief_014_text = """Проект «Портал»

Крупный HR-портал для компании 5000+ сотрудников. Личные кабинеты, заявки на отпуск,
оценки эффективности, обучение.

Команда:
— Архитектор (микросервисы, PostgreSQL)
— 3 бэкенд-разработчика (Go, middle+)
— 2 фронтенд-разработчика (React + TypeScript)
— UX/UI-дизайнер (Figma, прототипы)
— 2 тестировщика (автотесты Playwright, ручное тестирование)
— Бизнес-аналитик (требования от HR-отдела)
— Project manager (Agile/Scrum)
— DevOps (Kubernetes, CI/CD)

Срок — 12 месяцев. Высокие требования к доступности (WCAG 2.1 AA).
"""

brief_014_gold = [
    {
        "requirement_id": "req_014_001",
        "brief_id": "brf_014",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.90},
        "competency": {"competency_id": "arch_microservices", "name_ru": "Микросервисы", "confidence": 0.90},
        "level": 5,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["12 months × 21 days ≈ 252pd; architect = level 5 (expert)", "Part-time ~60% = ~150pd actual"],
        "evidence": [make_evidence(brief_014_text, "Архитектор (микросервисы")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_002",
        "brief_id": "brf_014",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.85},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.80},
        "level": 4,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["PostgreSQL as main database; architect designs data model"],
        "evidence": [make_evidence(brief_014_text, "PostgreSQL", find_offset(brief_014_text, "микросервисы, PostgreSQL")[0])],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_003",
        "brief_id": "brf_014",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 3,
        "effort": {"min_person_days": 336, "likely_person_days": 480, "max_person_days": 672, "unit": "person_days"},
        "assumptions": ["12 months × 21 days × 3 = 756pd max; middle+ = level 3"],
        "evidence": [make_evidence(brief_014_text, "3 бэкенд-разработчика (Go, middle+")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_004",
        "brief_id": "brf_014",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 224, "likely_person_days": 320, "max_person_days": 448, "unit": "person_days"},
        "assumptions": ["12 months × 21 days × 2 = 504pd max"],
        "evidence": [make_evidence(brief_014_text, "2 фронтенд-разработчика (React + TypeScript)")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_005",
        "brief_id": "brf_014",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "lang_typescript", "name_ru": "TypeScript", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 224, "likely_person_days": 320, "max_person_days": 448, "unit": "person_days"},
        "assumptions": ["TypeScript as primary frontend language"],
        "evidence": [make_evidence(brief_014_text, "TypeScript")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_006",
        "brief_id": "brf_014",
        "role": {"role_id": "ux_ui_designer", "name_ru": "UX/UI-дизайнер", "confidence": 0.90},
        "competency": {"competency_id": "ui_figma", "name_ru": "Figma", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["UX/UI designer for portal design; Figma as main tool"],
        "evidence": [make_evidence(brief_014_text, "UX/UI-дизайнер (Figma")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_007",
        "brief_id": "brf_014",
        "role": {"role_id": "ux_ui_designer", "name_ru": "UX/UI-дизайнер", "confidence": 0.85},
        "competency": {"competency_id": "ui_wireframing", "name_ru": "Прототипирование", "confidence": 0.85},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Prototyping for portal screens"],
        "evidence": [make_evidence(brief_014_text, "прототипы")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_008",
        "brief_id": "brf_014",
        "role": {"role_id": "qa_engineer", "name_ru": "Тестировщик", "confidence": 0.90},
        "competency": {"competency_id": "test_playwright", "name_ru": "Playwright", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 224, "likely_person_days": 320, "max_person_days": 448, "unit": "person_days"},
        "assumptions": ["2 QA engineers; Playwright for automated E2E testing"],
        "evidence": [make_evidence(brief_014_text, "2 тестировщика (автотесты Playwright")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_009",
        "brief_id": "brf_014",
        "role": {"role_id": "qa_engineer", "name_ru": "Тестировщик", "confidence": 0.85},
        "competency": {"competency_id": "qa_manual", "name_ru": "Ручное тестирование", "confidence": 0.85},
        "level": 2,
        "criticality": "should",
        "headcount": 2,
        "effort": {"min_person_days": 112, "likely_person_days": 160, "max_person_days": 224, "unit": "person_days"},
        "assumptions": ["Manual testing alongside automated"],
        "evidence": [make_evidence(brief_014_text, "ручное тестирование")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_010",
        "brief_id": "brf_014",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.90},
        "competency": {"competency_id": "an_requirements", "name_ru": "Работа с требованиями", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["BA gathers requirements from HR department"],
        "evidence": [make_evidence(brief_014_text, "Бизнес-аналитик (требования от HR-отдела)")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_011",
        "brief_id": "brf_014",
        "role": {"role_id": "project_manager", "name_ru": "Руководитель проекта", "confidence": 0.90},
        "competency": {"competency_id": "proc_agile_scrum", "name_ru": "Agile / Scrum", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["PM for Scrum process management"],
        "evidence": [make_evidence(brief_014_text, "Project manager (Agile/Scrum)")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_012",
        "brief_id": "brf_014",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.90},
        "competency": {"competency_id": "cloud_kubernetes", "name_ru": "Kubernetes", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 126, "likely_person_days": 180, "max_person_days": 252, "unit": "person_days"},
        "assumptions": ["Kubernetes for portal deployment"],
        "evidence": [make_evidence(brief_014_text, "DevOps (Kubernetes")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_013",
        "brief_id": "brf_014",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.85},
        "competency": {"competency_id": "cicd_gitlab", "name_ru": "GitLab CI", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["CI/CD pipelines"],
        "evidence": [make_evidence(brief_014_text, "CI/CD")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_014_014",
        "brief_id": "brf_014",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.75},
        "competency": {"competency_id": "web_accessibility", "name_ru": "Доступность веб-интерфейсов", "confidence": 0.75},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 28, "likely_person_days": 40, "max_person_days": 56, "unit": "person_days"},
        "assumptions": ["WCAG 2.1 AA compliance requires accessibility expertise", "Part of frontend work but separate competency"],
        "evidence": [make_evidence(brief_014_text, "WCAG 2.1 AA")],
        "confidence": 0.75,
        "needs_review": False,
    },
]

brief_014_notes = """# Brief 014: Complex HR Portal

## Category
Web/Mobile (Enterprise Portal)

## Complexity
Very complex — 14 requirements, 8 roles.

## Planted defects
None — comprehensive brief with clear role list.

## Assumptions
- 12 months timeline
- Team size: 1+3+2+1+2+1+1+1 = 12 people
- Architect and designer are full-time
- WCAG 2.1 AA → web_accessibility competency
- All competencies are in the taxonomy

## Edge cases
- "middle+" → level 3 (solid middle)
- Team composition is explicit → clear headcount per role
- Many roles → high complexity for extraction
"""


# =========================================================================
# BRIEF 015: Telecom Migration (Telecom domain)
# =========================================================================
brief_015_text = """Проект «Связь»

Телеком-оператор модернизирует биллинговую систему. Текущая — на Oracle + Java 6.
Новая: Go-микросервисы + PostgreSQL + Kafka (событийная архитектура).

Нужны: архитектор (опыт в телекоме, биллинг), 2 Go-разработчика senior,
дата-инженер (миграция данных из Oracle), DevOps.

Срок — 9 месяцев. Объём данных — 500M записей.
"""

brief_015_gold = [
    {
        "requirement_id": "req_015_001",
        "brief_id": "brf_015",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.85},
        "competency": {"competency_id": "dom_telecom", "name_ru": "Телеком-домен", "confidence": 0.80},
        "level": 5,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 126, "likely_person_days": 180, "max_person_days": 252, "unit": "person_days"},
        "assumptions": ["9 months × 21 days ≈ 189pd; telecom domain expertise required", "Architect = level 5; part-time ~60%"],
        "evidence": [make_evidence(brief_015_text, "архитектор (опыт в телекоме, биллинг)")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_015_002",
        "brief_id": "brf_015",
        "role": {"role_id": "solution_architect", "name_ru": "Архитектор решения", "confidence": 0.85},
        "competency": {"competency_id": "arch_event_driven", "name_ru": "Событийная архитектура", "confidence": 0.85},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 63, "likely_person_days": 90, "max_person_days": 126, "unit": "person_days"},
        "assumptions": ["Event-driven architecture with Kafka"],
        "evidence": [make_evidence(brief_015_text, "Kafka (событийная архитектура)")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_015_003",
        "brief_id": "brf_015",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 252, "likely_person_days": 360, "max_person_days": 504, "unit": "person_days"},
        "assumptions": ["9 months × 21 days × 2 = 378pd; senior = level 4"],
        "evidence": [make_evidence(brief_015_text, "2 Go-разработчика senior")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_015_004",
        "brief_id": "brf_015",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.85},
        "competency": {"competency_id": "lang_sql", "name_ru": "SQL", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Data migration from Oracle; SQL expertise needed for data extraction and transformation"],
        "evidence": [make_evidence(brief_015_text, "дата-инженер (миграция данных из Oracle)")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_015_005",
        "brief_id": "brf_015",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "db_postgresql", "name_ru": "PostgreSQL", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["PostgreSQL as target database"],
        "evidence": [make_evidence(brief_015_text, "PostgreSQL")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_015_006",
        "brief_id": "brf_015",
        "role": {"role_id": "devops_engineer", "name_ru": "DevOps-инженер", "confidence": 0.85},
        "competency": {"competency_id": "cloud_kubernetes", "name_ru": "Kubernetes", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["DevOps for deployment; Kubernetes implied for microservices"],
        "evidence": [make_evidence(brief_015_text, "DevOps")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_015_007",
        "brief_id": "brf_015",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.80},
        "competency": {"competency_id": "mq_kafka", "name_ru": "Apache Kafka", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["Kafka for event-driven messaging"],
        "evidence": [make_evidence(brief_015_text, "Kafka")],
        "confidence": 0.80,
        "needs_review": False,
    },
]

brief_015_notes = """# Brief 015: Telecom Migration

## Category
Legacy Modernization (Telecom domain)

## Complexity
Medium — 7 requirements, 4 roles.

## Planted defects
None — domain-specific brief.

## Assumptions
- Telecom domain expertise critical for architect (dom_telecom competency)
- Oracle is source, not target → no Oracle competency needed in team
- 500M records = significant data migration effort
- DevOps skills not detailed → Kubernetes assumed

## Edge cases
- "биллинг" → dom_telecom competency (telecom billing)
- Oracle Java 6 → legacy, not needed in new team
- Data migration is a separate role from backend development
"""


# =========================================================================
# BRIEF 016: E-commerce Platform
# =========================================================================
brief_016_text = """Проект «Маркет»

Интернет-магазин с каталогом 50K товаров, корзиной, оплатой и доставкой.
Платформы: веб + мобильное приложение (React Native).

Команда: 3 бэкенд-разработчика (Go, middle+), 2 фронтенд (React, TypeScript),
1 дата-инженер (рекомендации, аналитика), тестировщик, дизайнер.
Срок — 6 месяцев.

Интеграции: платёжный шлюз, службы доставки (2 API), CRM.
"""

brief_016_gold = [
    {
        "requirement_id": "req_016_001",
        "brief_id": "brf_016",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "lang_go", "name_ru": "Go", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 3,
        "effort": {"min_person_days": 252, "likely_person_days": 360, "max_person_days": 504, "unit": "person_days"},
        "assumptions": ["6 months × 21 days × 3 = 378pd; middle+ = level 3"],
        "evidence": [make_evidence(brief_016_text, "3 бэкенд-разработчика (Go, middle+")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_002",
        "brief_id": "brf_016",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.85},
        "competency": {"competency_id": "api_rest", "name_ru": "REST API design", "confidence": 0.85},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["REST APIs for payment gateway, delivery services, CRM integrations"],
        "evidence": [make_evidence(brief_016_text, "Интеграции: платёжный шлюз, службы доставки (2 API)")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_003",
        "brief_id": "brf_016",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.95},
        "competency": {"competency_id": "fw_react", "name_ru": "React", "confidence": 0.95},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["6 months × 21 days × 2 = 252pd; React for web + React Native for mobile"],
        "evidence": [make_evidence(brief_016_text, "2 фронтенд (React, TypeScript)")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_004",
        "brief_id": "brf_016",
        "role": {"role_id": "frontend_engineer", "name_ru": "Фронтенд-разработчик", "confidence": 0.90},
        "competency": {"competency_id": "lang_typescript", "name_ru": "TypeScript", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["TypeScript for frontend codebase"],
        "evidence": [make_evidence(brief_016_text, "TypeScript")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_005",
        "brief_id": "brf_016",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.85},
        "competency": {"competency_id": "ml_recsys", "name_ru": "Рекомендательные системы", "confidence": 0.75},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Data engineer for recommendation system; recsys competency"],
        "evidence": [make_evidence(brief_016_text, "дата-инженер (рекомендации, аналитика)")],
        "confidence": 0.75,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_006",
        "brief_id": "brf_016",
        "role": {"role_id": "qa_engineer", "name_ru": "Тестировщик", "confidence": 0.85},
        "competency": {"competency_id": "qa_test_design", "name_ru": "Тест-дизайн", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["QA for e-commerce flows; test design for checkout, payment, delivery"],
        "evidence": [make_evidence(brief_016_text, "тестировщик")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_007",
        "brief_id": "brf_016",
        "role": {"role_id": "ux_ui_designer", "name_ru": "UX/UI-дизайнер", "confidence": 0.85},
        "competency": {"competency_id": "ui_figma", "name_ru": "Figma", "confidence": 0.80},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["Designer for e-commerce UI; Figma as main tool"],
        "evidence": [make_evidence(brief_016_text, "дизайнер")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_016_008",
        "brief_id": "brf_016",
        "role": {"role_id": "backend_engineer", "name_ru": "Бэкенд-разработчик", "confidence": 0.80},
        "competency": {"competency_id": "dom_retail", "name_ru": "Ритейл-домен", "confidence": 0.75},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 21, "likely_person_days": 30, "max_person_days": 42, "unit": "person_days"},
        "assumptions": ["Retail domain knowledge for e-commerce specifics (catalog, cart, delivery)"],
        "evidence": [make_evidence(brief_016_text, "Интернет-магазин")],
        "confidence": 0.70,
        "needs_review": False,
    },
]

brief_016_notes = """# Brief 016: E-commerce Platform

## Category
Web/Mobile (E-commerce / Retail)

## Complexity
Complex — 8 requirements, 5 roles.

## Planted defects
None.

## Assumptions
- React Native for mobile → mapped to React competency
- 3 integrations (payment, 2 delivery APIs, CRM) → REST API work
- Recommendations → ml_recsys competency for data engineer
- No DevOps mentioned → assume existing infrastructure or part of backend work

## Edge cases
- "дизайнер" → ux_ui_designer (only one designer role)
- React Native → fw_react (closest match in taxonomy)
- CRM integration is implied but not detailed
"""


# =========================================================================
# BRIEF 017: Data Lake (Data/Analytics, complex)
# =========================================================================
brief_017_text = """Проект «Озеро»

Строительство data lake для аналитической платформы. Источники: 10+ систем (CRM, ERP, логи,
ClickHouse). Хранилище: S3 + Iceberg. Обработка: Spark + Airflow.

Команда: 2 дата-инженера senior (Python, Spark, SQL), ML-инженер для построения
предиктивных моделей (Python, PyTorch, MLOps), бизнес-аналитик.

Срок — 8 месяцев. Данные: 2 ТБ растёт на 200 ГБ/месяц.
Цель: self-service аналитика + ML-модели для прогнозирования спроса.
"""

brief_017_gold = [
    {
        "requirement_id": "req_017_001",
        "brief_id": "brf_017",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.95},
        "competency": {"competency_id": "lang_python", "name_ru": "Python", "confidence": 0.95},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 224, "likely_person_days": 320, "max_person_days": 448, "unit": "person_days"},
        "assumptions": ["8 months × 21 days × 2 = 336pd; senior = level 4"],
        "evidence": [make_evidence(brief_017_text, "2 дата-инженера senior (Python")],
        "confidence": 0.95,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_002",
        "brief_id": "brf_017",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.90},
        "competency": {"competency_id": "etl_orchestration", "name_ru": "ETL и оркестрация", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 168, "likely_person_days": 240, "max_person_days": 336, "unit": "person_days"},
        "assumptions": ["Spark + Airflow for data processing pipelines"],
        "evidence": [make_evidence(brief_017_text, "Spark + Airflow")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_003",
        "brief_id": "brf_017",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.90},
        "competency": {"competency_id": "lang_sql", "name_ru": "SQL", "confidence": 0.90},
        "level": 4,
        "criticality": "must",
        "headcount": 2,
        "effort": {"min_person_days": 112, "likely_person_days": 160, "max_person_days": 224, "unit": "person_days"},
        "assumptions": ["SQL for data querying and transformation"],
        "evidence": [make_evidence(brief_017_text, "SQL")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_004",
        "brief_id": "brf_017",
        "role": {"role_id": "ml_engineer", "name_ru": "ML-инженер", "confidence": 0.90},
        "competency": {"competency_id": "lang_python", "name_ru": "Python", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 112, "likely_person_days": 160, "max_person_days": 224, "unit": "person_days"},
        "assumptions": ["ML engineer for predictive models; middle level assumed"],
        "evidence": [make_evidence(brief_017_text, "ML-инженер для построения\nпредиктивных моделей")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_005",
        "brief_id": "brf_017",
        "role": {"role_id": "ml_engineer", "name_ru": "ML-инженер", "confidence": 0.90},
        "competency": {"competency_id": "ml_pytorch", "name_ru": "PyTorch", "confidence": 0.90},
        "level": 3,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["PyTorch for model development"],
        "evidence": [make_evidence(brief_017_text, "PyTorch")],
        "confidence": 0.90,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_006",
        "brief_id": "brf_017",
        "role": {"role_id": "ml_engineer", "name_ru": "ML-инженер", "confidence": 0.85},
        "competency": {"competency_id": "ml_mlops", "name_ru": "MLOps", "confidence": 0.85},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 42, "likely_person_days": 60, "max_person_days": 84, "unit": "person_days"},
        "assumptions": ["MLOps for model deployment and monitoring"],
        "evidence": [make_evidence(brief_017_text, "MLOps")],
        "confidence": 0.85,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_007",
        "brief_id": "brf_017",
        "role": {"role_id": "business_analyst", "name_ru": "Бизнес-аналитик", "confidence": 0.80},
        "competency": {"competency_id": "an_sql_analysis", "name_ru": "Анализ данных через SQL", "confidence": 0.80},
        "level": 3,
        "criticality": "should",
        "headcount": 1,
        "effort": {"min_person_days": 84, "likely_person_days": 120, "max_person_days": 168, "unit": "person_days"},
        "assumptions": ["BA for self-service analytics; SQL skills for ad-hoc queries"],
        "evidence": [make_evidence(brief_017_text, "бизнес-аналитик")],
        "confidence": 0.80,
        "needs_review": False,
    },
    {
        "requirement_id": "req_017_008",
        "brief_id": "brf_017",
        "role": {"role_id": "data_engineer", "name_ru": "Дата-инженер", "confidence": 0.75},
        "competency": {"competency_id": "dwh_modeling", "name_ru": "Моделирование хранилища данных", "confidence": 0.75},
        "level": 4,
        "criticality": "must",
        "headcount": 1,
        "effort": {"min_person_days": 56, "likely_person_days": 80, "max_person_days": 112, "unit": "person_days"},
        "assumptions": ["Data lake architecture requires DWH modeling expertise (Iceberg format, schema design)"],
        "evidence": [make_evidence(brief_017_text, "data lake")],
        "confidence": 0.75,
        "needs_review": False,
    },
]

brief_017_notes = """# Brief 017: Data Lake

## Category
Data/Analytics

## Complexity
Complex — 8 requirements, 3 roles.

## Planted defects
None — complex data engineering brief.

## Assumptions
- S3 + Iceberg → data lake storage (dwh_modeling competency)
- Spark not in taxonomy as separate competency → mapped to etl_orchestration
- ML engineer for demand forecasting (прогнозирование спроса)
- 2 TB initial + 200 GB/month growth → significant data volume

## Edge cases
- "Spark" → not in taxonomy → mapped to etl_orchestration (closest)
- "Iceberg" → not in taxonomy → part of dwh_modeling
- "S3" → not in taxonomy → storage detail, not a team competency
- Self-service analytics → BA role with SQL skills
"""


# =========================================================================
# Generate all briefs
# =========================================================================

if __name__ == "__main__":
    print("Generating gold briefs...")
    briefs = [
        (1, "simple_web_mvp", brief_001_text, brief_001_gold, brief_001_notes),
        (2, "data_dwh", brief_002_text, brief_002_gold, brief_002_notes),
        (3, "enterprise_1c", brief_003_text, brief_003_gold, brief_003_notes),
        (4, "k8s_migration", brief_004_text, brief_004_gold, brief_004_notes),
        (5, "legacy_modernization", brief_005_text, brief_005_gold, brief_005_notes),
        (6, "ml_recommendation", brief_006_text, brief_006_gold, brief_006_notes),
        (7, "bi_dashboards", brief_007_text, brief_007_gold, brief_007_notes),
        (8, "mobile_banking", brief_008_text, brief_008_gold, brief_008_notes),
        (9, "esb_messaging", brief_009_text, brief_009_gold, brief_009_notes),
        (10, "devops_platform", brief_010_text, brief_010_gold, brief_010_notes),
        (11, "ambiguous_pipelines", brief_011_text, brief_011_gold, brief_011_notes),
        (12, "implicit_pci_dss", brief_012_text, brief_012_gold, brief_012_notes),
        (13, "contradictory_junior", brief_013_text, brief_013_gold, brief_013_notes),
        (14, "complex_portal", brief_014_text, brief_014_gold, brief_014_notes),
        (15, "telecom_migration", brief_015_text, brief_015_gold, brief_015_notes),
        (16, "ecommerce", brief_016_text, brief_016_gold, brief_016_notes),
        (17, "data_lake", brief_017_text, brief_017_gold, brief_017_notes),
    ]

    for num, slug, text, gold, notes in briefs:
        write_brief(num, slug, text, gold, notes)

    print(f"\nGenerated {len(briefs)} briefs in {GOLD_DIR}")
