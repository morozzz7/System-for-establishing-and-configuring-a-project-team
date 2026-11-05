# Metrics for Evaluating an Optimal Project Team

Defines what "optimal team" means and how to measure it. Implemented in `backend/internal/metrics`
(per-variant computation) and `eval/` (benchmark). All thresholds and weights are **starting hypotheses**
to be calibrated on expert PM choices (section 6).

Notation: T = team variant; R = requirements; r in R; w_r = requirement weight (must=1.0, should=0.5, nice=0.2);
L_r = required level (1-5); E_r = effort (person-days); i = employee; lvl_ir = employee level in competency r;
a_irw = load share of i on r in week w; cap_iw = free capacity of i in week w; D = deadline (if given).
All level-1 components are normalized to [0, 1], higher is better.

---

## Level 0. Hard constraints (gates)

Binary feasibility. A variant with any violation **does not exist** for the user.
Target: 0 violations on 100% of variants; measured on every run and in CI.

| ID | Condition | Metric |
|---|---|---|
| G1 | No assignments during absence/unavailability | `violations_G1` = person-weeks with a>0 while unavailable |
| G2 | No overload: sum_r a_irw <= cap_iw for all i, w | `violations_G2`, `max_overload_pct` |
| G3 | Every must requirement covered at level >= L_r **or** explicitly listed as deficit | `uncovered_must_not_in_deficit` |
| G4 | User constraints: force include/exclude, team size limit, budget | `violations_G4` |
| G5 | Data integrity: existing active employee, valid dates | `violations_G5` |

`Gate(T) = 1` if all violation counts are 0, else 0.

---

## Level 1. Team quality components

**M1. Competency Coverage**
`cov_r = max_{i in T} min(1, lvl_ir / L_r)`, `M1 = sum(w_r * cov_r) / sum(w_r)`.
Also report `must_coverage` (share of covered must requirements); target 1.0 or explicit deficit.

**M2. Effort Coverage**
`q_ir = min(1, lvl_ir / L_r)`, `covered_r = min(E_r, sum_{i,w}(a_irw * q_ir * d_w))` (d_w = working days in week w),
`M2 = sum(covered_r) / sum(E_r)`. Deficit: `deficit_r = E_r - covered_r` (person-days).

**M3. Level Fit**
`fit_ir = 1 - |lvl_ir - L_r| / 4`, asymmetric penalty: under-qualification x1.5, over-qualification (lvl - L >= 2) x1.0.
`M3` = effort-weighted mean of fit over assignments. Also report `overqualified_share`: share of effort done by people >= 2 levels above required (costly, demotivating).

**M4. Experience Relevance**
`exp_i` = mean of top-3 cosine similarities (brief embedding vs i's completed-project descriptions); `M4 = sum(pd_i * exp_i) / sum(pd_i)`.
If no project history exists, drop M4 from Score and renormalize weights.

**M5. Schedule**
`T50`, `T80` = duration (weeks) from Monte Carlo over {min, likely, max} (N=2000, fixed seed) with real capacity and absences.
If D is set: `M5 = P(T <= D)`. Otherwise `M5 = T_best / T50`, T_best = lowest T50 among feasible variants.
Also: `schedule_slack = (D - T80) / D`.

**M6. Cost**
`C = sum(person-hours_i * rate(grade_i))` (rates by grade, never personal).
`M6 = clamp(1 - (C - C_min) / (C_ref - C_min), 0, 1)`; C_ref = budget or max among feasible variants.

**M7. Load Health**
`u_iw` = resulting load of i in week w (including other projects).
`overheat` = mean over person-weeks of `min(1, max(0, u - 0.9) / 0.1)`, `M7 = 1 - overheat`.
Also: `peak_util`, `mean_util`, share of person-weeks in the 60-90% band.

**M8. Portfolio Impact**
`slip_p` = delay (working days) of each other project p caused by pulling people; `M8 = exp(-sum_p(slip_p) / tau)`, tau = 10 working days (configurable).
Also: `projects_affected`, `critical_projects_affected`.

**M9. Bus Factor**
`N_r` = number of members of T with lvl >= L_r. `M9 = |{r in must : N_r >= 2}| / |must|`.
Also: `min_N_r`, `single_point_of_failure_list`.

**M10. Focus (overhead)**
`frag` = share of assignments with load < 20%; `ctx = min(1, (mean concurrent projects per member - 1) / 3)`;
`size_pen = min(1, max(0, (|T| - n_opt) / n_opt))`, `n_opt = ceil(sum(E_r) / (T_ref * workdays_per_week * 0.8))`;
`M10 = 1 - (0.4*frag + 0.3*ctx + 0.3*size_pen)`. (Communication channels n(n-1)/2: informational.)

**M11. Bench Utilization**
`M11` = bench-employee hours assigned to T / total available bench hours in the project window (clipped to 1). Directly tied to the "bench" problem in the project passport.

**M12. Resilience**
For each i in T simulate a 2-week dropout. `M12` = share of scenarios where G1-G3 still hold and T80 grows by <= 20%.

**M13. Cohesion (optional, needs history)**
`M13` = share of member pairs who already worked together on a project for >= 4 weeks.

### Composite Score
```
Score(T) = Gate(T) * sum_k(lambda_k * M_k),   sum(lambda_k) = 1
```
Starting weight presets:

| Metric | Fast | Economical | Balanced | Low-risk |
|---|---|---|---|---|
| M1 Competency coverage | 0.15 | 0.15 | 0.15 | 0.12 |
| M2 Effort coverage | 0.15 | 0.12 | 0.13 | 0.10 |
| M3 Level fit | 0.08 | 0.10 | 0.10 | 0.08 |
| M4 Experience | 0.07 | 0.05 | 0.08 | 0.07 |
| M5 Schedule | 0.28 | 0.08 | 0.15 | 0.10 |
| M6 Cost | 0.04 | 0.27 | 0.10 | 0.05 |
| M7 Load health | 0.05 | 0.06 | 0.08 | 0.12 |
| M8 Portfolio impact | 0.06 | 0.06 | 0.08 | 0.10 |
| M9 Bus factor | 0.03 | 0.03 | 0.05 | 0.10 |
| M10 Focus | 0.03 | 0.04 | 0.04 | 0.04 |
| M11 Bench | 0.02 | 0.03 | 0.02 | 0.02 |
| M12 Resilience | 0.04 | 0.01 | 0.02 | 0.10 |

Rules for the set of 2-3 variants:
1. All variants pass Gate.
2. Variants are **non-dominated** on the vector (M1..M12); otherwise the weakest is replaced.
3. Variants **differ** in roster: Jaccard distance >= 0.25 (else it is one variant).
4. Always show the metric vector and the main trade-off alongside Score; Score never replaces the explanation.

---

## Level 2. Deficits and external cooperation

| Metric | Definition |
|---|---|
| `deficit_person_days[r]` | effort shortage per competency (from M2) |
| `deficit_weeks[r]` | duration shift if the deficit is closed internally |
| `external_cooperation_flag` | true if `deficit_must > 0` **or** `P(T <= D) < 0.5` despite available people |
| `recommended_action` | hire / contractor / move deadline / upskill / reallocate from project X (with rough cost of each) |

---

## 3. System metrics (not team): NLP, optimizer, explanations

Targets are initial prototype hypotheses.

**3.1 Brief parsing**
| Metric | Definition | Target |
|---|---|---|
| Precision / Recall / F1 | match vs gold annotation (role, competency, level) | F1 >= 0.85 |
| Normalization accuracy | top-1 / top-3 match with taxonomy | >= 0.80 / >= 0.95 |
| Hallucination rate | share of requirements without valid `evidence` in the brief | 0% (hard block) |
| JSON validity | responses passing schema within <= 2 retries | >= 99% |
| Effort MAPE | `abs(estimate - expert) / expert`; and expert value inside min-max range | MAPE <= 30%, hit >= 80% |
| `needs_review` share | how often a human check is requested | 10-30% (lower = overconfidence, higher = useless) |

**3.2 Optimizer and validator**
| Metric | Definition | Target |
|---|---|---|
| Hard-violation rate | output variants violating G1-G5 | **0** |
| Solver-validator disagreement | ML variants rejected by the Go validator | trend to 0, track as defect |
| Feasibility recall | feasible team found where one provably exists (planted solution) | >= 98% |
| Optimality gap | `(Score_ref - Score_sys) / Score_ref` vs reference on small problems | <= 5% |
| Solve time p95 | 500 employees x 30 requirements | <= 10 s per recalculation |
| Diversity | mean pairwise Jaccard distance of variants | >= 0.25 |
| Determinism | same input + seed -> same output | 100% |
| Baseline superiority | Score vs random feasible / greedy-by-skill / greedy-by-load | >= +10% |

**3.3 Deficits**
| Metric | Target |
|---|---|
| Deficit recall on synthetic with planted shortages | >= 0.90 |
| Deficit precision (no false alarms) | >= 0.90 |
| Deficit size error (person-days) | <= 20% |
| `external_cooperation_flag` correctness | >= 95% |

**3.4 Agreement with expert PM and usefulness**
| Metric | Definition | Target |
|---|---|---|
| Expert acceptability | PM rating 1-5; share >= 4 | >= 70% of variants |
| Top-k agreement | expert team within k=3 variants (exact / Jaccard >= 0.7) | >= 60% / >= 80% |
| Edit distance | people the PM replaced in the chosen variant | <= 1 on average |
| Time-to-shortlist | brief -> shortlist vs manual process | >= 70% reduction |
| Zero-touch acceptance | variants accepted without edits | track |

**3.5 Explainability**
| Metric | Definition | Target |
|---|---|---|
| Faithfulness | every number/claim derivable from facts (automated check) | 100% |
| Coverage | members and relevant excluded people with >= 1 reason | 100% |
| Counterfactual accuracy | predicted "what if" matches the actual rerun | >= 95% |
| Clarity | expert rating 1-5 | >= 4.0 |

**3.6 Recalculation and robustness (customer requirement: manual constraint edits)**
| Metric | Definition | Target |
|---|---|---|
| Responsiveness | constraint change -> expected result change (metamorphic tests) | 100% |
| Stability | small input perturbation (+-5% effort) -> share of changed members | <= 20% |
| Recalculation time p95 | | <= 5 s |

---

## 4. Mapping to customer acceptance checks
| Customer check | Metrics |
|---|---|
| Hard constraints respected | G1-G5, hard-violation rate |
| No unavailable employees assigned | G1 = 0 |
| Deficits detected correctly | 3.3, Level 2 |
| Result changes when load/absence changes | 3.6 Responsiveness |
| Several feasible variants with duration and deficits | M5, variant-set rules |
| Explanation and constraint-editing UI | 3.5, 3.6 |

## 5. Pilot business metrics (from project passport)
Bench idle-time reduction (days/month per 10 specialists), load conflicts prevented before project start,
time from request to shortlist, share of projects where external-hire need was detected in advance.

## 6. Evaluation protocol and calibration
1. Synthetic scenarios with planted solution (`eval/synth`): main automated benchmark, runs in CI.
2. Gold annotation of >= 15 briefs for extraction metrics.
3. Expert panel: 2-3 PMs rate variants blind (algorithm label hidden) and pick the best.
4. **Weight calibration:** fit lambda_k from experts' pairwise preferences (A better than B) via logistic regression (Bradley-Terry);
   metrics are considered valid if Score agrees with experts (Kendall tau >= 0.6).
5. Ablation: drop each M_k and measure the change in agreement with experts.
6. Each run's report is stored in `eval/reports/` and compared with the previous one (regression = defect).
