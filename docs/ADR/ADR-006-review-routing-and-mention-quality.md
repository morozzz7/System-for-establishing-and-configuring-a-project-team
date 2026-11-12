# ADR-006: Review routing and deterministic mention-quality gates for `/v1/extract`

- Status: accepted
- Date: 2026-11-09
- Deciders: architecture (orchestrator), ml, qa-eval
- Related: `docs/PLAN.md` 3.1-fix, `contracts/ml-openapi.yaml` (`ExtractResponse`, `ExtractDiagnostics`, `ExtractVerification`), ADR-005
- Supersedes: none

## Context

The live run of `/v1/extract` on `eval/long_brief2` (642 words, 19 gold requirements) exposed three defects
that ADR-005 did not cover. All numbers below are from `eval/runs/long_brief_latest.json`.

1. **Candidate verification was dead, not degraded.** The word-boundary scanner found 14 candidate
   competencies; the single verification LLM call returned an empty body (`finish_reason = "length"`
   with `max_tokens = 1024`), so `_parse_json_items` returned `None` and all 14 candidates were logged
   `skipped` with `"verification response JSON parse error: Empty response from LLM"`.
   `confirmed = rejected = 0`. The stage could not add or remove anything. The reported
   `pair recall = 1.0` came entirely from the primary extraction pass, i.e. it was luck, not verification.

2. **Low-confidence pairs leaked into the optimizer input.** Three of the four pair false positives had
   `confidence = 0.56` and `needs_review = true` (`ux_ui_designer/ui_figma`,
   `ux_ui_designer/ui_wireframing`, `business_analyst/an_requirements`). The lexical anchor check had
   correctly detected that neither the competency name nor its synonyms occur near the evidence quote,
   lowered the confidence and set `needs_review`, but the items still appeared in `requirements`.
   Any consumer that ignores `needs_review` (the optimizer will) treats them as confirmed requirements.

3. **A passing remark became a high-confidence requirement.** `devops_engineer/cloud_docker`
   (`confidence = 0.90`, `needs_review = false`) was extracted from
   «Контейнеризация — разумеется, Docker, как у всех». The `extract-v4` prompt already forbids this
   ("generic stack summary lines and conversational remarks ... do NOT create a separate requirement"),
   but an LLM instruction is not a guarantee. The gold annotation deliberately excludes it: the sentence
   contains no concrete project task. The competency is lexically anchored, so the lexical check could
   not catch it either.

The same run also produced two scanner false positives from generic prose: `dom_telecom` from the
company name «Север-Телеком» and `test_performance` from «снять нагрузку с контактного центра».
With verification live, these candidates would reach the LLM and could be confirmed as new pairs.

## Decision

### 1. Verification is batched, budgeted and retried; every candidate gets an explicit decision

- Candidates (competency mentions + missing roles) are split into batches of **8** items per LLM call.
- Per batch, `max_tokens = min(8192, max(1024, 256 + 192 * n_items))`.
- Each batch gets `1 + LLM_MAX_RETRIES` attempts (default 3). On empty content or a JSON parse error the
  retry re-sends the batch with `max_tokens *= 1.5` and an explicit "return ONLY the JSON array" nudge.
  `finish_reason` and the effective `max_tokens` are written to the per-candidate `verification_log` reason.
- A candidate is added to the output **only** on an explicit `decision = "confirm"` with an exact brief
  quote. A missing/unknown `decision` is `skipped`, never an implicit confirm.
- Accounting invariant: `candidates_found == confirmed + rejected + skipped`, exposed as
  `diagnostics.verification`. `skipped` means "no decision obtained" (transport/parse), not "rejected".
  `diagnostics.verification` counts **competency** candidates only; the role-candidate pass keeps its own
  `missing_roles_*` counters, which are reported alongside it and not folded into `candidates_found`.
- Acceptance: on `eval/long_brief2` **0 failed batches, 0 skipped** (all candidates resolved;
  the scanner fix in §4 reduces the expected candidate count from 14 to 11);
  across the 17 gold briefs `skipped <= 10%` of `candidates_found`.

### 2. Low-confidence pairs are routed to a separate review list

- A requirement with `competency != null`, `needs_review == true` and `confidence <= 0.60` is returned in
  `requirements_for_review`, not in `requirements`.
- Role-only requirements (`competency == null`) stay in `requirements` with `needs_review = true`: a role
  without a technology produces no pair false positive and is still useful for staffing.
- `requirements_for_review` is an **additive** response field (`default: []`); clients that do not know it
  still receive a valid response. Items in it are never sent to `/v1/optimize` until a human confirms them.
- Threshold `0.60` is a starting hypothesis (same class as the `METRICS.md` weights) and is calibrated
  against expert review decisions later; it is a single named constant, not scattered literals.
- Acceptance on the archived `eval/runs/long_brief_latest.json` artifact (offline replay, deterministic):
  the 3 low-confidence false positives appear in `requirements_for_review` and **0** of them appear in
  `requirements`; `devops_engineer/cloud_docker` is dropped by §3; main pair **precision 0.8261 -> 1.0**.
  The rule is confidence-based, so the 0.58 gold pair `solution_architect/arch_event_driven` (found in
  1 of 3 ensemble runs) also moves to review: main pair recall is **18/19 = 0.9474**, review recall is
  **1/19 = 0.0526** (TP=1, FN=18 — the missing gold pair is in the review queue, not lost; main+review recall is 19/19 = 1.0), and main+review precision is
  **0.8636** (the 3 remaining false positives are confined to the review queue). This is the intended
  human-in-the-loop behaviour, not a recall regression: an uncertain pair is confirmed by a human before
  it reaches `/v1/optimize`.

### 3. Generic / passing technology mentions are dropped deterministically

A deterministic gate runs after the lexical anchor check and before deduplication:

- Marker set (case-folded, `ё -> е`): `разумеется`, `как у всех`, `как обычно`, `как всегда`,
  `само собой`, `по умолчанию`, `как все`, `очевидно`, `общеизвестно`, `всем известно`,
  `стек:`, `технологический стек`, `общий стек`, `стек компании`.
- Task-cue set (stems): `настра`, `разворач`, `разверн`, `собира`, `собер`, `пиш`, `напиш`, `разраб`,
  `внедр`, `поддерж`, `эксплуат`, `управля`, `описыва`, `опиш`, `тестир`, `проверя`, `мигрир`,
  `перенос`, `депло`, `резервн`, `реплик`, `обновля`, `монитор`, `наблюда`, `дашборд`, `кластер`,
  `балансир`, `шифр`, `аутентифик`, `авториз`, `интегр`, `проектир`, `документир`, `конфигурир`, `настрой`.
- If a marker is present **and** no task cue is present in the evidence quote, the requirement is dropped
  and counted in `diagnostics.dropped_generic_mention` (and repeated in `warnings`). If a marker is
  present **and** a task cue is present, the item is kept with `needs_review = true` (weak mention, not
  silently trusted).
- Acceptance: `devops_engineer/cloud_docker` from «разумеется, Docker, как у всех» is dropped;
  **0 of the 116 gold quotes** and **0 of the 19 `long_brief2` gold quotes** are flagged by the gate.

### 4. The candidate scanner must not mine generic prose or hyphenated proper names

- The word-boundary lookbehind also excludes `-`, so «Север-Телеком» no longer yields `dom_telecom`.
- `телеком`, `оператор связи`, `критерии приёмки`, `нагрузка`, `нагрузку`, `нагрузки` are added to the
  generic-prose stop list, so `dom_telecom`, `an_user_stories` and `test_performance` are not proposed
  from those phrases.
- Acceptance: the candidate set for `eval/long_brief2` contains none of `dom_telecom`, `an_user_stories`,
  `test_performance`; the trap rate stays 0 and no gold pair is lost.

## Alternatives considered

| Alternative | Why rejected |
|---|---|
| Only increase `max_tokens` for the single verification call | One large response is still fragile (truncation, provider empty-body), and one failure loses all candidates. Batching isolates failures. |
| Keep low-confidence pairs in `requirements` and rely on `needs_review` | The optimizer input is exactly the place where the flag is ignored. A separate list makes the contract enforce the separation. |
| Drop low-confidence pairs silently | Violates explainability: a human must be able to see and confirm them. Review list keeps the information. |
| Let the LLM reject passing remarks (prompt-only) | Already in `extract-v4` and already failed at confidence 0.90. Instructions are not guarantees; deterministic gates are testable. |
| Verify every extracted pair with a second LLM pass | Doubles cost/latency and re-introduces non-determinism for the common case. Deterministic gates cover the known failure mode; verification stays for scanner candidates. |
| New `/v2/extract` for the additive field | Additive fields are explicitly allowed by `ml-openapi.yaml` conventions; a version bump would force a coordinated Go release for no benefit. |

## Consequences

- **Positive:** the verification stage becomes observable and testable (`diagnostics.verification`);
  the optimizer never receives unconfirmed low-confidence pairs; the known passing-remark false positive
  is removed deterministically; every removal is counted and explained.
- **Negative / risks:** the marker/task-cue lists are language-specific (ru/en) and will need extension;
  a legitimately phrased generic mention can be dropped, which is why the gate is limited to sentences
  with a marker and no task cue and is covered by gold-quote regression tests. The `0.60` threshold is a
  hypothesis and may move after calibration.
- **Testable:** unit tests for batch budgeting/retry/accounting, review routing, the generic gate on all
  gold quotes, and the scanner stop list; live `eval/run_long_brief.py` must report
  `requirements_for_review`, `dropped_generic_mention` and `diagnostics.verification`.
