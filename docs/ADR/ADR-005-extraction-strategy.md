# ADR-005: Extraction strategy — LLM-first with deterministic validation

- Status: accepted
- Date: 2026-11-08
- Deciders: architecture (orchestrator), ml, qa-eval
- Related: `docs/PLAN.md` 3.1, `contracts/ml-openapi.yaml`, `contracts/taxonomy.yaml`, ADR-004
- Supersedes: ADR-005 v1 ("rules-first, LLM as enricher")

## Context

An audit of the `/v1/extract` pipeline and its evaluation against the 17 gold briefs (`eval/gold`, 116 requirements) established the following facts:

1. **The post-processor rejected valid taxonomy pairs (`Ф1`).**
   `postprocessor.py` required `competency_id in role.core_competencies`. However, in `contracts/taxonomy.yaml`, 159 of 226 allowed `(role, competency)` associations in `competency.roles` are outside `role.core_competencies`, and 50 of 101 competencies do not belong to any role's `core_competencies` (e.g., `cloud_docker`, `cloud_yandex`). In the gold set, 96% of pairs are in `competency.roles`, while only 82% are in `role.core_competencies`.

2. **LLM errors were swallowed silently (`Ф2`).**
   `llm_extract.py` caught `LLMError` and returned `([], stats)`, making the error-handling branches in `router.py` unreachable and silently returning rules-only outputs under `engine="hybrid"` without warnings. In addition, the LLM was asked to emit the verbose wire schema (`EffortRange`, `assumptions`, offsets, etc.) within 20 s with no `max_tokens` budget.

3. **Retries were identical and validation was all-or-nothing (`Ф3`).**
   Failed validation retried the exact same prompt at `temperature=0, seed=42` without feeding back the validation error. A single invalid item discarded the entire brief's extraction result. Quote validation lacked normalization for whitespace, non-breaking spaces, `ё/е`, dashes, and quotation marks.

4. **Prompt issues (`Ф4`).**
   The prompt omitted taxonomy synonyms, leaked `brief_001` from `eval/gold` as Few-Shot Example 1, and forced `core_competencies` constraints that distorted role selection.

5. **Rules engine is unsuitable as a decision source (`Ф5`).**
   While dictionary matching finds explicit technology mentions (competency F1 = 0.7778), character-proximity role assignment is unreliable (exact pair F1 = 0.5944 on the 17 gold briefs), substring matching risks false positives on short synonyms, and union-merging with priority to rules injected rules noise into final API outputs.

## Decision

We transition `/v1/extract` to an **LLM-first** architecture with **strictly deterministic validation**:

1. **LLM is the sole source of extraction decisions.**
   - The LLM decides *what* competencies are required and *which role* owns each requirement based on the brief text and the closed taxonomy (`contracts/taxonomy.yaml`).
   - `RulesEngine` and `Merger` are removed from the `/v1/extract` request path (`diagnostics.engine = "llm"`). `rules.py` is marked deprecated and retained only as a reference baseline in `eval/run_eval.py`.

2. **Compact LLM output schema + deterministic enrichment.**
   - The LLM outputs compact items: `{role_id, competency_id, quote, level, criticality}` (plus optional `headcount`, `confidence`, `needs_review`).
   - Deterministic Python code constructs the full `ExtractedRequirement` wire shape: `requirement_id`, taxonomy `name_ru`, `effort` via `EffortEstimator` (decoupled from `rules.py`), `assumptions`, and exact `char_start`/`char_end` offsets in `brief.text`.
   - Public contract `contracts/ml-openapi.yaml` and `ExtractResponse` remain unchanged.

3. **Deterministic 6-stage validation gate (`validator.py` / `postprocessor.py`).**
   Every item returned by the LLM is validated independently (element-by-element, never all-or-nothing) in the following order, with per-stage diagnostic counters:
   1. **Schema validation** (Pydantic element-level check).
   2. **Taxonomy existence & request subset check** (`role_id` and `competency_id` exist in `taxonomy.yaml` and, when `request.taxonomy.role_ids` / `competency_ids` are provided, belong to that subset).
   3. **Normalized quote verification (hallucination gate)**: `quote` is matched against `brief.text` after normalizing whitespace, non-breaking spaces, `ё/е`, dash variants, and quote characters; the exact original substring `brief.text[char_start:char_end]` and its offsets are written to `evidence`. Any requirement whose quote does not match is dropped (`dropped_no_evidence`).
   4. **Role-competency validity via `competency.roles`**: a pair `(role_id, competency_id)` is valid iff `role_id in competency.roles`. `role.core_competencies` is never used to reject a pair (it may only contribute as a weak signal for `needs_review=True` when confidence is low).
   5. **Deduplication**: duplicate `(role_id, competency_id)` pairs are removed. The same `competency_id` under different roles is permitted only when backed by distinct `quote` spans.
   6. **Limit & confidence filtering**: filter by `options.min_confidence` and cap at `options.max_requirements` deterministically by confidence descending.

4. **Roles are NEVER rewritten automatically.**
   - If the LLM outputs an invalid `(role_id, competency_id)` pair where `role_id not in competency.roles`, the post-processor must **not** substitute a default role.
   - Instead, invalid items (or quote/schema errors) trigger a feedback retry telling the LLM which element and field failed and listing the allowed `competency.roles`. If the pair remains invalid after retries, it is dropped and counted in `diagnostics.post_processor_dropped`.

5. **Feedback retries and fail-loudly error policy.**
   - Retries include the specific validation error(s) from the failed attempt; if output was truncated (`finish_reason == "length"`), `max_tokens` is increased. Identical retries are forbidden.
   - Total execution across attempts respects `options.timeout_ms`.
   - LLM errors are never swallowed: when the LLM is unavailable (missing API key, connection failure, HTTP 5xx after retries) or times out, `/v1/extract` returns a `Problem` response (`503 CAPABILITY_UNAVAILABLE` with `reason_codes: ["ML_LLM_UNAVAILABLE"]` or `408 TIMEOUT` with `reason_codes: ["ML_TIMEOUT"]`, `retryable: true`) per `contracts/ml-openapi.yaml` and ADR-004. Returning HTTP 200 with an empty requirement list on LLM failure is forbidden.

6. **Prompt `extract-v3` and hold-out evaluation discipline.**
   - `extract-v3` includes role and competency synonyms and the `competency -> allowed roles` table, removes leaked gold examples, uses 2–3 examples from outside `eval/gold`, and specifies role-selection and implicit-requirement rules aligned with `eval/GOLD_POLICY.md`.
   - Evaluation (`eval/run_eval.py`) tracks competency F1, exact pair F1, relaxed pair F1 (`role_id in competency.roles`), and conditional role accuracy across stages (`rules`, `llm_raw`, `llm_validated`, `final_api`), with a dedicated hold-out split of at least 5 briefs.

## Consequences

- **Positive:**
  - Eliminates the artificial ~0.82 pair recall ceiling caused by filtering on `role.core_competencies` (e.g. `devops_engineer / cloud_yandex` is now valid).
  - Compact LLM output reduces completion tokens by ~3–4x, preventing timeouts on large briefs.
  - Element-level validation and feedback retries prevent single-item mistakes from zeroing out an entire brief.
  - Honest `Problem` responses on LLM failure align `/v1/extract` with ADR-004 ("fail loudly, never silently").
- **Negative / risks:**
  - `/v1/extract` requires a configured and reachable LLM endpoint (`503` when unavailable); manual requirement entry in the UI remains the degradation path per ADR-004 §3.
- **Metrics affected:**
  - `METRICS.md` §3.1: competency F1 (target ≥ 0.85 on hold-out), pair F1 (target ≥ 0.70 on hold-out), hallucination rate (0%), JSON validity (≥ 99%), empty/failed brief rate (≤ 5%).
