# Gold Brief Labeling Guide v1

> **Purpose.** This document describes how a human annotator marks up a project brief (Russian text)
> into a gold-standard list of `Requirement` objects. Two independent annotators using this guide
> should agree on ≥ 90 % of labels (measured on inter-annotator Cohen's κ during calibration).
>
> **Canonical references.** `contracts/taxonomy.yaml` (roles + competencies), `contracts/json-schema/requirement.schema.json`
> (shape), `contracts/json-schema/common.schema.json` (shared types), `contracts/reference.yaml` (effort/time model).

---

## 1. What is a requirement?

A **requirement** is one atomic tuple **(role, competency, level, criticality, effort range)**.
If the brief says *«нужен Go-разработчик middle и Python-разработчик для ETL»* → that is **two**
requirements, not one.

Split rules:
- **Different roles** → always separate requirements.
- **Same role, different competencies** mentioned as distinct skills → separate requirements.
- **Same role, same competency, different levels** (e.g. *«middle и senior»*) → separate requirements.
- **Headcount > 1** is allowed in a single requirement (`headcount` field), but only when the brief
  uses collective language (*«два бэкенд-разработчика»*).

---

## 2. How to identify roles

Use the **12 canonical `role_id` values** from `contracts/taxonomy.yaml`:

| role_id | name_ru | Synonym hints |
|---|---|---|
| `backend_engineer` | Бэкенд-разработчик | бэкенд, серверный разработчик, api developer |
| `frontend_engineer` | Фронтенд-разработчик | фронтенд, фронт, веб-разработчик |
| `qa_engineer` | Тестировщик | qa, тестирование, инженер по тестированию |
| `devops_engineer` | DevOps-инженер | девопс, sre, инфраструктурный инженер |
| `data_engineer` | Дата-инженер | дата-инженер, инженер данных, etl developer |
| `ml_engineer` | ML-инженер | ml-инженер, data scientist, дата-сайентист |
| `business_analyst` | Бизнес-аналитик | бизнес-аналитик, аналитик требований, product owner |
| `system_analyst` | Системный аналитик | системный аналитик, аналитик api |
| `project_manager` | Руководитель проекта | pm, руководитель проекта, delivery manager |
| `ux_ui_designer` | UX/UI-дизайнер | дизайнер, ux, ui, продуктовый дизайнер |
| `solution_architect` | Архитектор решения | архитектор, техлид, lead architect |
| `security_engineer` | Инженер по безопасности | инфобез, appsec, security analyst |

**Disambiguation rules:**
- *«аналитик»* without context → `business_analyst` (default). If the brief talks about APIs,
  integration specs, OpenAPI → `system_analyst`.
- *«архитектор»* → `solution_architect`. If the brief says *«архитектор бэкенда»* or
  *«ведущий разработчик-архитектор»* → `backend_engineer` with level 4-5 + note the ambiguity.
- *«разработчик»* without language → flag `needs_review: true`, propose the most likely role from
  context, record the assumption.

---

## 3. How to identify competencies

Use `competency_id` from `competencies:` section of `contracts/taxonomy.yaml` (101 entries).
If the brief mentions a technology **not in the taxonomy**:
1. Set `needs_review: true`.
2. Use a proposed `competency_id` following the naming pattern: `<category>_<short_name>`
   (e.g. `lang_rust`, `fw_ember`).
3. Record in `notes.md` why the taxonomy is insufficient.
4. Set `competency.confidence` ≤ 0.5.

**Examples:**
- *«Go 1.23»* → `lang_go` (synonym match)
- *«реляционная база»* (generic) → `db_postgresql` if context implies Postgres, else `needs_review: true`
- *«блокчейн»* → not in taxonomy → `needs_review: true`, proposed id `dom_blockchain`

---

## 4. Levels 1–5

Use the descriptors from `contracts/taxonomy.yaml` section `levels:`:

| Level | Label (RU) | Mapping from brief language |
|---|---|---|
| 1 | Ознакомлен | «знаком с», «читал о», «базовое представление» |
| 2 | Базовый | «начинающий», «junior», «работает с наставником» |
| 3 | Уверенный | «middle», «самостоятельно», «уверенный» |
| 4 | Продвинутый | «senior», «продвинутый», «архитектирует», «менторит» |
| 5 | Эксперт | «lead», «principal», «эксперт», «определяет стандарты» |

**Rules:**
- *«middle+»* → level 3 (not 4). The «+» means solid middle, not senior.
- *«senior / lead»* → level 4.
- *«principal / architect-level»* → level 5.
- If unclear → level 3 (default) + `assumptions: ["Defaulted to level 3; brief does not specify"]`.

---

## 5. Criticality: must / should / nice

| Value | Meaning | Signal words |
|---|---|---|
| `must` | Cannot ship without | «обязательно», «критично», «без этого нельзя», «must have» |
| `should` | Major quality loss without | «желательно», «важно», «should», «нужно» |
| `nice` | Desirable but deferrable | «было бы хорошо», «nice to have», «опционально» |

**Default:** If unclear → `should`. Record in `assumptions`.

---

## 6. Effort ranges (three-point estimates)

**Always three-point:** `{min_person_days, likely_person_days, max_person_days}`. Unit is always
`"person_days"` (= 8 working hours).

**Conversion rules:**
| Brief says | Conversion |
|---|---|
| Single number X person-days | `[0.7·X, X, 1.4·X]`, round to 0.5 |
| Range [A, B] | `[A, (A+B)/2, B]` |
| «about N months, M people» | `likely = N · 21 · M`; min = 0.7 · likely; max = 1.4 · likely |
| No hint at all | Use role-specific defaults (see `notes.md`); flag `assumptions: ["Effort estimated from role benchmark, no brief hint"]` |
| «MVP in 6 weeks, 3 people» | Total effort = 6 · 5 · 3 = 90 pd; distribute across requirements |

**Rounding:** round to nearest 0.5. Always ensure min ≤ likely ≤ max.

---

## 7. Evidence spans

Each requirement **MUST** have ≥ 1 evidence span of `kind: "brief_span"`.

**Schema:**
```json
{
  "kind": "brief_span",
  "quote": "exact substring from brief text",
  "char_start": 142,
  "char_end": 185
}
```

**Rules:**
1. `quote` must appear **literally** in the brief text. No paraphrasing.
2. `char_start` / `char_end` are **code-point (character) offsets** in the source text,
   0-indexed, end-exclusive. Python: `text[char_start:char_end] == quote`.
3. Minimum quote length: 3 characters. Maximum: 1000 characters.
4. If you cannot find any quote → the requirement is a **hallucination** and MUST NOT be in gold.
5. Multiple evidence spans are allowed (e.g. when the requirement is supported by two sentences).

**How to compute offsets:**
```python
text = open("brief.md").read()
# Remove the front-matter (everything before the first non-comment line)
# The brief body starts after the YAML front-matter or after the first blank line
idx = text.find(quote)
assert text[idx:idx+len(quote)] == quote
char_start = idx  # 0-indexed character offset
char_end = idx + len(quote)
```

> **Self-check:** run `python eval/scripts/validate_gold.py` — it verifies all offsets automatically.

---

## 8. Assumptions

Free list of strings (1–10 items, 3–300 chars each). Record everything the annotator **assumed**
that is not literally in the brief:
- effort estimation basis
- default level when not specified
- role disambiguation reasoning
- implicit scope (e.g. «CI/CD is implied for any DevOps role»)
- working-time assumptions (8h/day, 5d/week)

---

## 9. Confidence

Float 0.0–1.0. Rules:
| Signal | Confidence |
|---|---|
| Exact quote, clear role/competency/level | 0.95–1.0 |
| Clear quote, level inferred from context | 0.80–0.94 |
| Quote present, role or competency needs disambiguation | 0.65–0.79 |
| Inferred from context, no direct quote for some field | 0.50–0.64 |
| Guess / very uncertain | < 0.50 → `needs_review: true` |

**Hard rule:** `confidence < 0.7` → `needs_review: true` (per schema invariant).

---

## 10. Disambiguation rules for planted-defect briefs

Three briefs in the gold set contain deliberate traps. Annotator instructions:

### 10a. Ambiguous term → two possible competencies
**Example:** brief mentions *«настройка pipelines»* — could be CI/CD pipelines (`cicd_gitlab`) or
data pipelines (`etl_orchestration`).
- **Rule:** annotate based on surrounding context. If context is ambiguous, create two requirements
  with the same evidence span, mark both `needs_review: true`, confidence ≤ 0.65, and note in
  `notes.md`. Or pick one and note the alternative in `assumptions`.
- **In gold:** we choose one interpretation and document both.

### 10b. Implicit requirement
**Example:** *«система должна соответствовать PCI DSS»* → implies security competency
(`sec_appsec`, `sec_owasp`) even though no «security engineer» is mentioned.
- **Rule:** extract the implicit requirement. Mark `confidence` ≤ 0.80. In `assumptions` explain
  the inference chain (PCI DSS → application security → `sec_appsec`).

### 10c. Contradictory statement
**Example:** *«нужен junior Go-разработчик, который также будет архитектором системы»*.
- **Rule:** this is contradictory (junior = level 2, architect = level 4-5). Split into two
  requirements:
  1. backend_engineer / lang_go / level 2 / must (the junior part)
  2. solution_architect / arch_system_design / level 4 / must (the architect part)
  Both with the same evidence. In `assumptions` note: *«Brief contradiction resolved by splitting
  into two roles; in practice this would be two people or one senior.»*

---

## Appendix A: JSON example

```json
[
  {
    "requirement_id": "req_001_001",
    "brief_id": "brf_001",
    "role": { "role_id": "backend_engineer", "confidence": 0.95 },
    "competency": { "competency_id": "lang_go", "confidence": 0.95 },
    "level": 3,
    "criticality": "must",
    "headcount": 1,
    "effort": {
      "min_person_days": 40,
      "likely_person_days": 60,
      "max_person_days": 90,
      "unit": "person_days"
    },
    "assumptions": ["8h/day, no weekends", "Includes code review time"],
    "evidence": [
      {
        "kind": "brief_span",
        "quote": "нужен Go-разработчик уровня middle",
        "char_start": 142,
        "char_end": 180
      }
    ],
    "confidence": 0.95,
    "needs_review": false
  }
]
```

## Appendix B: Checklist before submitting gold.json

- [ ] Every `role.role_id` is in `contracts/taxonomy.yaml`
- [ ] Every `competency.competency_id` is in `contracts/taxonomy.yaml` (or `needs_review: true`)
- [ ] `level` ∈ {1,2,3,4,5}
- [ ] `criticality` ∈ {must, should, nice}
- [ ] `effort` has min ≤ likely ≤ max, all ≥ 0.5, unit = "person_days"
- [ ] ≥ 1 evidence span with valid `char_start`/`char_end`
- [ ] ≥ 1 assumption
- [ ] `requirement_id` is unique within this brief
- [ ] `brief_id` matches directory naming convention
- [ ] `confidence < 0.7` → `needs_review: true`
- [ ] `python eval/scripts/validate_gold.py` passes
