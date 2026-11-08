# Gold Labeling Policy for `eval/gold`

Scope: 17 briefs, 116 requirements (last updated 2026-11-08).

## 1. Principles

1. **Taxonomy alignment is the primary gate.**
   Every annotated pair `(role_id, competency_id)` must satisfy `role_id ∈ competency.roles` from `contracts/taxonomy.yaml`.
   `role.core_competencies` is a *subset* and may be used as a confidence signal, but it must not be used to reject valid pairs.

2. **Evidence must be traceable.**
   - `evidence[].quote` must be an exact substring of the brief text.
   - `char_start`/`char_end` are optional; when present they must match `quote`.
   - If a quote cannot be found in the text, the requirement is invalid.

3. **No role-only requirements unless explicitly justified.**
   A requirement with `competency = null` is allowed only when a role is explicitly named in the brief and no competency from the taxonomy can be attached to it.
   The current gold set contains no role-only requirements.

4. **"Стек:" / stack-only mentions.**
   Technologies listed in a generic stack line are extracted only if they are also tied to a concrete role or requirement elsewhere in the brief.
   Example: `Docker` in `brief_001` is listed in the stack but not attached to any role, so it is **not** in gold.

5. **Competencies that belong to several roles.**
   For competencies with multiple allowed roles (e.g. `db_postgresql`, `mq_kafka`, `lang_python`), assign the role that is:
   1. explicitly named in the same sentence/phrase, or
   2. implied by the team list, or
   3. the most natural owner for the competency in the project context.
   If the choice remains ambiguous, the requirement may be marked `needs_review = true`.

6. **Derived roles.**
   - Testing competencies (`test_*`, E2E, automation) without an explicit role → `qa_engineer`.
   - Cloud / container / deployment competencies without an explicit role → `devops_engineer`.
   - PCI DSS / security standards → `security_engineer`.
   - Database in the context of a backend service → `backend_engineer`.

7. **Implicit requirements.**
   Only a short, agreed list of domain inferences is allowed:
   - PCI DSS level 1 → `security_engineer / sec_appsec` (with `needs_review = true`).
   Other implicit competencies must not be invented.

## 2. Corrections applied to gold

The following 5 requirements had a `role_id` that was not in `competency.roles`. They were corrected to an allowed role.

| Brief | requirement_id | competency_id | Old role | New role | Rationale |
|-------|----------------|---------------|----------|----------|-----------|
| `brief_009_esb_messaging` | `req_009_002` | `mq_kafka` | `solution_architect` | `backend_engineer` | Kafka is the messaging backbone implemented by backend engineers; architect is kept on `arch_event_driven`. |
| `brief_011_ambiguous_pipelines` | `req_011_004` | `lang_python` | `devops_engineer` | `data_engineer` | Python is listed for data-loading scripts in the data-pipeline context. |
| `brief_014_complex_portal` | `req_014_002` | `db_postgresql` | `solution_architect` | `backend_engineer` | PostgreSQL is the backend database; the architect requirement uses `arch_microservices`. |
| `brief_015_telecom_migration` | `req_015_001` | `dom_telecom` | `solution_architect` | `business_analyst` | Domain knowledge is assigned to BA per current taxonomy; architect keeps `arch_event_driven`. |
| `brief_016_ecommerce` | `req_016_005` | `ml_recsys` | `data_engineer` | `ml_engineer` | Recommender systems belong to ML engineer in the taxonomy. |

After the corrections:

- `python eval/scripts/validate_gold.py` passes with 0 errors.
- All 116 gold pairs satisfy `role_id ∈ competency.roles`.
- 18 pairs remain outside `role.core_competencies`; these are intentional many-to-many pairs and are listed in the baseline report for reference.

## 3. Decisions recorded per special case

### `brief_001` — Docker in stack
Docker appears only in the generic stack line `Стек: Go, React, TypeScript, PostgreSQL, Docker`. It is **not** attached to any concrete role or task, so it is **not** added to gold.

### `brief_011` — ambiguous "пайплайны"
Two requirements are kept:
- `cicd_gitlab / devops_engineer` for CI/CD pipelines.
- `etl_orchestration / data_engineer` for data pipelines, with `needs_review = true`.

### `brief_008` / `brief_012` — PCI DSS
`security_engineer / sec_appsec` is inferred from PCI DSS and marked `needs_review = true`.

### `brief_013` — contradictory levels
Junior level for architect-level work is kept as-is with `needs_review = true`.

## 4. Validation commands

```bash
# Check gold against taxonomy and evidence offsets
python eval/scripts/validate_gold.py --verbose

# Reproduce the baseline (rules-only, no API key needed)
python eval/run_eval.py --mode rules --out eval/BASELINE.md --dump-raw
```

## 5. Open questions for the orchestrator / gold owner

1. **Primary metric:** Should the main acceptance metric be exact pair F1, competency F1, or relaxed pair F1? The harness reports all three.
2. **`primary_role` in taxonomy:** The fix plan suggests adding an optional `primary_role` field to `taxonomy.yaml` to disambiguate multi-role competencies. This requires contract approval.
3. **Domain competency ownership:** `dom_telecom` is currently assigned to `business_analyst` to satisfy the taxonomy, but the brief text ties telecom experience to the architect. If the policy prefers `solution_architect`, either the taxonomy should add `solution_architect` to `dom_telecom.roles`, or this gold entry should be escalated.
