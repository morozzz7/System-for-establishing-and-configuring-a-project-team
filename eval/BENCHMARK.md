# Benchmark Plan v1

> **Owner:** qa-eval · **Depends on:** tasks 1.2 (taxonomy), 1.4 (gold briefs), 3.1 (/v1/extract)
> **Target:** validate that the extraction pipeline meets METRICS.md §3.1 targets before optimizer
> integration. The same harness is reused for the full benchmark in task 4.6.

---

## 1. Directory structure

```
eval/
├── GOLD_GUIDE.md             # labeling guide for human annotators
├── BENCHMARK.md              # this file
├── gold/
│   ├── README.md             # index of all briefs (id, category, one-line description)
│   ├── brief_001_*/          # one dir per gold brief
│   │   ├── brief.md          # source text in Russian
│   │   ├── gold.json         # array of Requirement objects
│   │   └── notes.md          # annotator assumptions, ambiguity notes
│   └── ...
├── scripts/
│   ├── validate_gold.py      # schema + offset validation (runs now, no ML service needed)
│   └── run_benchmark.py      # calls /v1/extract, computes metrics, writes report
└── reports/
    ├── .gitkeep
    └── <YYYY-MM-DD>.md       # human-readable report
    └── <YYYY-MM-DD>.json     # machine-readable report
```

### Versioning
- Gold set is versioned via git. Each brief directory has a stable name (`brief_NNN_<slug>`).
- `eval/gold/README.md` is the registry; a new brief = new directory + new line in README.
- **Current size:** 17 briefs (target: ≥ 30 before final benchmark in task 4.6).

---

## 2. How the benchmark runs

```
python eval/scripts/run_benchmark.py \
  --gold-dir eval/gold \
  --extract-url http://localhost:8100/v1/extract \
  --output eval/reports/$(date +%F) \
  --seed 42
```

Pipeline per brief:
1. Load `brief.md` (strip front-matter, take body text).
2. POST `{brief_text, taxonomy_ref}` to `/v1/extract`.
3. Parse the response as `Requirement[]`.
4. For each predicted requirement, match to gold by (role, competency) — using Hungarian matching
   if multiple candidates.
5. Compute metrics (see §3).
6. Aggregate across briefs.
7. Write report with per-brief and aggregate numbers, diff vs previous report.

---

## 3. Metrics to compute

All targets from METRICS.md §3.1:

| Metric | Definition | Target | How computed |
|---|---|---|---|
| **Precision / Recall / F1** | match vs gold (role, competency) | F1 ≥ 0.85 | per-brief then macro-average |
| **Level accuracy** | predicted level == gold level | ≥ 80 % exact, ≥ 95 % ±1 | exact match rate |
| **Criticality accuracy** | predicted criticality == gold | ≥ 85 % | exact match rate |
| **Normalization top-1** | predicted `competency_id` == gold `competency_id` | ≥ 0.80 | share of correct top-1 |
| **Normalization top-3** | gold in predicted top-3 candidates | ≥ 0.95 | share where gold ∈ candidates |
| **Hallucination rate** | predicted requirements without evidence in brief | 0 % | hard gate |
| **JSON validity** | responses passing schema within ≤ 2 retries | ≥ 99 % | jsonschema validation |
| **Effort MAPE** | `abs(pred - gold) / gold` on `likely_person_days` | ≤ 30 % | macro-average |
| **Effort range hit** | gold value inside [min, max] | ≥ 80 % | share of requirements |
| **needs_review share** | predicted `needs_review: true` | 10–30 % | share (calibration check) |

### Per-role / per-competency breakdown
Report precision/recall/F1 for each role and each competency that appears ≥ 3 times in gold.

---

## 4. Planted scenarios (must be present in gold set)

| # | Category | Planted defect | Brief IDs |
|---|---|---|---|
| 1 | Purely feasible | No defect — control case | brief_001, brief_008 |
| 2 | Must-role deficit | Required role not in company pool → needs external hire | brief_006 (ML NLP expert) |
| 3 | Ambiguous language | One term maps to 2 competencies | brief_011 (pipelines: CI/CD vs ETL) |
| 4 | Implicit requirement | Compliance mentioned → security competency implied | brief_012 (PCI DSS → sec_appsec) |
| 5 | Contradictory | Junior who also architects | brief_013 (junior Go dev + architect) |
| 6 | Effort spread | Many roles, rough size hints | brief_014, brief_017 |

Each scenario is documented in `eval/gold/README.md` and annotated in individual `notes.md`.

---

## 5. Pass/fail thresholds

From METRICS.md §3.1 (starting hypotheses, not to be changed without orchestrator approval):

| Metric | Pass threshold | Fail → action |
|---|---|---|
| F1 (role+competency) | ≥ 0.85 | File bug with minimal repro brief |
| Hallucination rate | = 0 % | Hard block, must fix before release |
| JSON validity | ≥ 99 % | Fix prompt / retry logic |
| Normalization top-1 | ≥ 0.80 | Review taxonomy coverage |
| Normalization top-3 | ≥ 0.95 | Review embedding model |
| Effort MAPE | ≤ 30 % | Review effort prompts |
| needs_review share | 10–30 % | Calibration: if < 10 % → overconfident; if > 30 % → under-confident |

---

## 6. Gold set governance

- **Current:** 17 briefs across 5 categories.
- **Target before 4.6:** ≥ 30 briefs (add ~2/month from real customer brief patterns, anonymized).
- **Extension protocol:**
  1. New brief text (anonymized!) → `brief_NNN_<slug>/brief.md`.
  2. Two independent annotators label → `gold.json` each.
  3. Disagreements resolved in `notes.md`; if κ < 0.8 on the brief → rewrite GOLD_GUIDE.
  4. `validate_gold.py` passes on the new brief.
  5. Add line to `README.md`.

---

## 7. Reports

Each run produces:
- `eval/reports/<date>.json`: all metrics, per-brief breakdown, predicted vs gold diff.
- `eval/reports/<date>.md`: human-readable summary with:
  - Aggregate metrics table
  - Per-brief F1 scores
  - Top-5 worst-performing briefs (with excerpts)
  - Diff vs previous report (regressions highlighted in **bold red**)
  - Commit hash, model version, prompt version

**Regression detection:** any metric that drops below its threshold OR drops > 5 pp vs previous run
→ flagged as regression → defect filed via orchestrator.
