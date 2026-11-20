# Extraction Evaluation & Baseline Report

- Generated: 2026-11-12T13:15:27.649619+00:00
- Mode: `fixtures`
- Runs per brief: `1`
- Prompt version: `extract-v4`
- Review threshold (`REVIEW_CONFIDENCE_MAX`): `0.6` (ADR-006)
- Hold-out briefs (5): `brief_005_legacy_modernization, brief_007_bi_dashboards, brief_010_devops_platform, brief_015_telecom_migration, brief_016_ecommerce`
- Total wall time: 13.38s

## Aggregate metrics — All briefs (17 briefs, 116 gold requirements)

| Stage | Metric | TP | FP | FN | Precision | Recall | F1 / Acc | Spread (min..max, std) |
|-------|--------|----|----|----|-----------|--------|----------|------------------------|
| `rules` | role | 55 | 7 | 5 | 0.8871 | 0.9167 | 0.9016 | — |
|  | competency | 91 | 23 | 23 | 0.7982 | 0.7982 | 0.7982 | — |
|  | pair | 89 | 28 | 27 | 0.7607 | 0.7672 | 0.7639 | — |
|  | pair_relaxed | 92 | 25 | 24 | 0.7863 | 0.7931 | 0.7897 | — |
|  | role_accuracy | 88 | 3 | 3 | 0.9670 | 0.9670 | 0.9670 | — |
|  | headcount_accuracy | 63 | 26 | 26 | 0.7079 | 0.7079 | 0.7079 | — |
|  | headcount_multi | 8 | 4 | 4 | 0.6667 | 0.6667 | 0.6667 | — |
| `llm_raw` | role | 59 | 0 | 1 | 1.0000 | 0.9833 | 0.9916 | — |
|  | competency | 107 | 10 | 7 | 0.9145 | 0.9386 | 0.9264 | — |
|  | pair | 109 | 11 | 7 | 0.9083 | 0.9397 | 0.9237 | — |
|  | pair_relaxed | 109 | 11 | 7 | 0.9083 | 0.9397 | 0.9237 | — |
|  | role_accuracy | 107 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 109 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_multi | 12 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
| `llm_validated` | role | 58 | 0 | 2 | 1.0000 | 0.9667 | 0.9831 | — |
|  | competency | 107 | 7 | 7 | 0.9386 | 0.9386 | 0.9386 | — |
|  | pair | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | pair_relaxed | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | role_accuracy | 107 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 109 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_multi | 11 | 1 | 1 | 0.9167 | 0.9167 | 0.9167 | — |
| `final_api` | role | 58 | 0 | 2 | 1.0000 | 0.9667 | 0.9831 | — |
|  | competency | 107 | 7 | 7 | 0.9386 | 0.9386 | 0.9386 | — |
|  | pair | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | pair_relaxed | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | role_accuracy | 107 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 109 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_multi | 11 | 1 | 1 | 0.9167 | 0.9167 | 0.9167 | — |

## Aggregate metrics — Hold-out split (5 briefs, 30 gold requirements)

| Stage | Metric | TP | FP | FN | Precision | Recall | F1 / Acc | Spread (min..max, std) |
|-------|--------|----|----|----|-----------|--------|----------|------------------------|
| `rules` | role | 17 | 2 | 2 | 0.8947 | 0.8947 | 0.8947 | — |
|  | competency | 21 | 6 | 9 | 0.7778 | 0.7000 | 0.7368 | — |
|  | pair | 20 | 8 | 10 | 0.7143 | 0.6667 | 0.6897 | — |
|  | pair_relaxed | 21 | 7 | 9 | 0.7500 | 0.7000 | 0.7241 | — |
|  | role_accuracy | 20 | 1 | 1 | 0.9524 | 0.9524 | 0.9524 | — |
|  | headcount_accuracy | 12 | 8 | 8 | 0.6000 | 0.6000 | 0.6000 | — |
|  | headcount_multi | 2 | 2 | 2 | 0.5000 | 0.5000 | 0.5000 | — |
| `llm_raw` | role | 18 | 0 | 1 | 1.0000 | 0.9474 | 0.9730 | — |
|  | competency | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | pair | 26 | 5 | 4 | 0.8387 | 0.8667 | 0.8525 | — |
|  | pair_relaxed | 26 | 5 | 4 | 0.8387 | 0.8667 | 0.8525 | — |
|  | role_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_multi | 4 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
| `llm_validated` | role | 18 | 0 | 1 | 1.0000 | 0.9474 | 0.9730 | — |
|  | competency | 26 | 3 | 4 | 0.8966 | 0.8667 | 0.8814 | — |
|  | pair | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | pair_relaxed | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | role_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_multi | 4 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
| `final_api` | role | 18 | 0 | 1 | 1.0000 | 0.9474 | 0.9730 | — |
|  | competency | 26 | 3 | 4 | 0.8966 | 0.8667 | 0.8814 | — |
|  | pair | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | pair_relaxed | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | role_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_multi | 4 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |

*Metric definitions:*
- `role`: unique `role_id` set per brief (target Recall = 1.0).
- `competency`: unique `competency_id` set per brief.
- `pair`: exact `(role_id, competency_id)` set per brief (role-only items excluded unless in gold).
- `pair_relaxed`: `(role_id, competency_id)` where role matches if `role_id in competency.roles`.
- `role_accuracy`: share of matched competencies (`gold_comps & pred_comps`) assigned to the exact gold `role_id`.
- `headcount_accuracy`: exact `headcount` match on matched pairs; `headcount_multi`: accuracy on gold requirements with `headcount > 1`.

## Per-brief metrics (Run 1)

| Brief | Split | Stage | Gold | Pred | Role Rec | Comp F1 | Pair F1 | Pair* F1 | Role Acc | HC Acc | Drops (ev/pair/dup) | Tokens (in/out) | Time ms | Notes |
|-------|-------|-------|------|------|----------|---------|---------|----------|----------|--------|---------------------|-----------------|---------|-------|
| brief_001_simple_web_mvp | dev | `rules` | 7 | 8 | 1.000 | 0.933 | 0.933 | 0.933 | 1.000 | 1.000 | 0/0/0 | — | 47 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/528 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/528 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/528 | 0 |  |
| brief_002_data_dwh | dev | `rules` | 7 | 9 | 1.000 | 0.667 | 0.667 | 0.667 | 1.000 | 1.000 | 0/0/0 | — | 49 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/563 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/563 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/563 | 0 |  |
| brief_003_enterprise_1c | dev | `rules` | 5 | 8 | 1.000 | 0.833 | 0.769 | 0.769 | 1.000 | 1.000 | 0/0/0 | — | 48 |  |
|  |  | `llm_raw` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/403 | 0 |  |
|  |  | `llm_validated` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/403 | 0 |  |
|  |  | `final_api` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/403 | 0 |  |
| brief_004_k8s_migration | dev | `rules` | 6 | 8 | 1.000 | 0.714 | 0.714 | 0.714 | 1.000 | 1.000 | 0/0/0 | — | 46 |  |
|  |  | `llm_raw` | 6 | 6 | 1.000 | 0.833 | 0.833 | 0.833 | 1.000 | 1.000 | 0/0/0 | 1910/475 | 0 |  |
|  |  | `llm_validated` | 6 | 6 | 1.000 | 0.833 | 0.833 | 0.833 | 1.000 | 1.000 | 0/0/0 | 1910/475 | 0 |  |
|  |  | `final_api` | 6 | 6 | 1.000 | 0.833 | 0.833 | 0.833 | 1.000 | 1.000 | 0/0/0 | 1910/475 | 0 |  |
| brief_005_legacy_modernization | hold-out | `rules` | 7 | 7 | 1.000 | 0.769 | 0.769 | 0.769 | 1.000 | 0.600 | 0/0/0 | — | 53 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 0.923 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | 1925/574 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 0.923 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | 1925/574 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 0.923 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | 1925/574 | 0 |  |
| brief_006_ml_recommendation | dev | `rules` | 9 | 9 | 1.000 | 0.875 | 0.889 | 0.889 | 1.000 | 1.000 | 0/0/0 | — | 48 |  |
|  |  | `llm_raw` | 9 | 10 | 1.000 | 0.941 | 0.947 | 0.947 | 1.000 | 1.000 | 0/0/0 | 1970/777 | 0 |  |
|  |  | `llm_validated` | 9 | 10 | 1.000 | 0.941 | 0.947 | 0.947 | 1.000 | 1.000 | 0/0/0 | 1970/777 | 0 |  |
|  |  | `final_api` | 9 | 10 | 1.000 | 0.941 | 0.947 | 0.947 | 1.000 | 1.000 | 0/0/0 | 1970/777 | 0 |  |
| brief_007_bi_dashboards | hold-out | `rules` | 3 | 2 | 0.500 | 0.400 | 0.400 | 0.400 | 1.000 | 1.000 | 0/0/0 | — | 44 |  |
|  |  | `llm_raw` | 3 | 3 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1865/245 | 0 |  |
|  |  | `llm_validated` | 3 | 3 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1865/245 | 0 |  |
|  |  | `final_api` | 3 | 3 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1865/245 | 0 |  |
| brief_008_mobile_banking | dev | `rules` | 8 | 9 | 0.833 | 0.625 | 0.500 | 0.625 | 0.800 | 0.750 | 0/0/0 | — | 47 |  |
|  |  | `llm_raw` | 8 | 9 | 1.000 | 0.706 | 0.706 | 0.706 | 1.000 | 1.000 | 0/0/0 | 1955/719 | 0 |  |
|  |  | `llm_validated` | 8 | 8 | 0.833 | 0.750 | 0.750 | 0.750 | 1.000 | 1.000 | 0/1/0 | 1955/719 | 0 |  |
|  |  | `final_api` | 8 | 8 | 0.833 | 0.750 | 0.750 | 0.750 | 1.000 | 1.000 | 0/1/0 | 1955/719 | 0 |  |
| brief_009_esb_messaging | dev | `rules` | 6 | 8 | 1.000 | 0.857 | 0.857 | 0.857 | 1.000 | 0.833 | 0/0/0 | — | 46 |  |
|  |  | `llm_raw` | 6 | 6 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1910/491 | 0 |  |
|  |  | `llm_validated` | 6 | 6 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1910/491 | 0 |  |
|  |  | `final_api` | 6 | 6 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1910/491 | 0 |  |
| brief_010_devops_platform | hold-out | `rules` | 5 | 6 | 1.000 | 0.909 | 0.909 | 0.909 | 1.000 | 1.000 | 0/0/0 | — | 46 |  |
|  |  | `llm_raw` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/390 | 0 |  |
|  |  | `llm_validated` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/390 | 0 |  |
|  |  | `final_api` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/390 | 0 |  |
| brief_011_ambiguous_pipelines | dev | `rules` | 4 | 3 | 1.000 | 0.857 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | — | 45 |  |
|  |  | `llm_raw` | 4 | 4 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1880/329 | 0 |  |
|  |  | `llm_validated` | 4 | 4 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1880/329 | 0 |  |
|  |  | `final_api` | 4 | 4 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1880/329 | 0 |  |
| brief_012_implicit_pci_dss | dev | `rules` | 7 | 6 | 0.750 | 0.833 | 0.833 | 0.833 | 1.000 | 0.800 | 0/0/0 | — | 47 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/554 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/554 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/554 | 0 |  |
| brief_013_contradictory_junior | dev | `rules` | 5 | 3 | 0.667 | 0.750 | 0.750 | 0.750 | 1.000 | 1.000 | 0/0/0 | — | 44 |  |
|  |  | `llm_raw` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/397 | 0 |  |
|  |  | `llm_validated` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/397 | 0 |  |
|  |  | `final_api` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/397 | 0 |  |
| brief_014_complex_portal | dev | `rules` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.143 | 0/0/0 | — | 49 |  |
|  |  | `llm_raw` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 2030/1103 | 0 |  |
|  |  | `llm_validated` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 2030/1103 | 0 |  |
|  |  | `final_api` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 2030/1103 | 0 |  |
| brief_015_telecom_migration | hold-out | `rules` | 7 | 10 | 0.800 | 0.714 | 0.533 | 0.667 | 0.800 | 0.250 | 0/0/0 | — | 46 |  |
|  |  | `llm_raw` | 7 | 7 | 0.800 | 0.714 | 0.714 | 0.714 | 1.000 | 1.000 | 0/0/0 | 1925/556 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 0.800 | 0.714 | 0.714 | 0.714 | 1.000 | 1.000 | 0/0/0 | 1925/556 | 0 |  |
|  |  | `final_api` | 7 | 7 | 0.800 | 0.714 | 0.714 | 0.714 | 1.000 | 1.000 | 0/0/0 | 1925/556 | 0 |  |
| brief_016_ecommerce | hold-out | `rules` | 8 | 9 | 1.000 | 0.714 | 0.714 | 0.714 | 1.000 | 0.400 | 0/0/0 | — | 45 |  |
|  |  | `llm_raw` | 8 | 9 | 1.000 | 0.824 | 0.824 | 0.824 | 1.000 | 1.000 | 0/0/0 | 1955/703 | 0 |  |
|  |  | `llm_validated` | 8 | 8 | 1.000 | 0.875 | 0.875 | 0.875 | 1.000 | 1.000 | 0/1/0 | 1955/703 | 0 |  |
|  |  | `final_api` | 8 | 8 | 1.000 | 0.875 | 0.875 | 0.875 | 1.000 | 1.000 | 0/1/0 | 1955/703 | 0 |  |
| brief_017_data_lake | dev | `rules` | 8 | 8 | 1.000 | 0.714 | 0.533 | 0.667 | 0.800 | 0.250 | 0/0/0 | — | 47 |  |
|  |  | `llm_raw` | 8 | 9 | 1.000 | 0.933 | 0.941 | 0.941 | 1.000 | 1.000 | 0/0/0 | 1955/697 | 0 |  |
|  |  | `llm_validated` | 8 | 8 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/1/0 | 1955/697 | 0 |  |
|  |  | `final_api` | 8 | 8 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/1/0 | 1955/697 | 0 |  |

## ADR-006 review routing (`final_api`)

- Review threshold: `needs_review = true` and `confidence <= 0.6` (`app.extract.review.REVIEW_CONFIDENCE_MAX`, starting hypothesis).
- `pair` keeps its pre-ADR-006 semantics: computed on the full validated list (main + review).
  `pair_main` / `pair_review` are computed on the split lists against the same gold set,
  so `pair_review` recall is measured against all gold pairs (ADR-006 §2: 1/19, not 1/4).
- `verification`: `candidates_found` must equal `confirmed + rejected + skipped`;
  `failed_batches > 0` means a candidate batch got no LLM decision.

| Scope | Predicted | Main | Review | Pair (full) TP/FP/FN P/R/F1 | Pair (main) TP/FP/FN P/R/F1 | Pair (review) TP/FP/FN P/R/F1 | dropped_generic_mention |
|---|---|---|---|---|---|---|---|
| all briefs (Run 1) | 117 | 111 | 6 | 109/8/7 0.9316/0.9397/0.9356 | 105/6/11 0.9459/0.9052/0.9251 | 4/2/112 0.6667/0.0345/0.0656 | 0 |
| brief_001_simple_web_mvp | 7 | 7 | 0 | 7/0/0 1.0000/1.0000/1.0000 | 7/0/0 1.0000/1.0000/1.0000 | 0/0/7 0.0000/0.0000/0.0000 | 0 |
| brief_002_data_dwh | 7 | 7 | 0 | 7/0/0 1.0000/1.0000/1.0000 | 7/0/0 1.0000/1.0000/1.0000 | 0/0/7 0.0000/0.0000/0.0000 | 0 |
| brief_003_enterprise_1c | 5 | 5 | 0 | 5/0/0 1.0000/1.0000/1.0000 | 5/0/0 1.0000/1.0000/1.0000 | 0/0/5 0.0000/0.0000/0.0000 | 0 |
| brief_004_k8s_migration | 6 | 6 | 0 | 5/1/1 0.8333/0.8333/0.8333 | 5/1/1 0.8333/0.8333/0.8333 | 0/0/6 0.0000/0.0000/0.0000 | 0 |
| brief_005_legacy_modernization | 7 | 7 | 0 | 6/1/1 0.8571/0.8571/0.8571 | 6/1/1 0.8571/0.8571/0.8571 | 0/0/7 0.0000/0.0000/0.0000 | 0 |
| brief_006_ml_recommendation | 10 | 9 | 1 | 9/1/0 0.9000/1.0000/0.9474 | 8/1/1 0.8889/0.8889/0.8889 | 1/0/8 1.0000/0.1111/0.2000 | 0 |
| brief_007_bi_dashboards | 3 | 2 | 1 | 3/0/0 1.0000/1.0000/1.0000 | 2/0/1 1.0000/0.6667/0.8000 | 1/0/2 1.0000/0.3333/0.5000 | 0 |
| brief_008_mobile_banking | 8 | 7 | 1 | 6/2/2 0.7500/0.7500/0.7500 | 6/1/2 0.8571/0.7500/0.8000 | 0/1/8 0.0000/0.0000/0.0000 | 0 |
| brief_009_esb_messaging | 6 | 6 | 0 | 6/0/0 1.0000/1.0000/1.0000 | 6/0/0 1.0000/1.0000/1.0000 | 0/0/6 0.0000/0.0000/0.0000 | 0 |
| brief_010_devops_platform | 5 | 5 | 0 | 5/0/0 1.0000/1.0000/1.0000 | 5/0/0 1.0000/1.0000/1.0000 | 0/0/5 0.0000/0.0000/0.0000 | 0 |
| brief_011_ambiguous_pipelines | 4 | 4 | 0 | 4/0/0 1.0000/1.0000/1.0000 | 4/0/0 1.0000/1.0000/1.0000 | 0/0/4 0.0000/0.0000/0.0000 | 0 |
| brief_012_implicit_pci_dss | 7 | 6 | 1 | 7/0/0 1.0000/1.0000/1.0000 | 6/0/1 1.0000/0.8571/0.9231 | 1/0/6 1.0000/0.1429/0.2500 | 0 |
| brief_013_contradictory_junior | 5 | 5 | 0 | 5/0/0 1.0000/1.0000/1.0000 | 5/0/0 1.0000/1.0000/1.0000 | 0/0/5 0.0000/0.0000/0.0000 | 0 |
| brief_014_complex_portal | 14 | 14 | 0 | 14/0/0 1.0000/1.0000/1.0000 | 14/0/0 1.0000/1.0000/1.0000 | 0/0/14 0.0000/0.0000/0.0000 | 0 |
| brief_015_telecom_migration | 7 | 7 | 0 | 5/2/2 0.7143/0.7143/0.7143 | 5/2/2 0.7143/0.7143/0.7143 | 0/0/7 0.0000/0.0000/0.0000 | 0 |
| brief_016_ecommerce | 8 | 6 | 2 | 7/1/1 0.8750/0.8750/0.8750 | 6/0/2 1.0000/0.7500/0.8571 | 1/1/7 0.5000/0.1250/0.2000 | 0 |
| brief_017_data_lake | 8 | 8 | 0 | 8/0/0 1.0000/1.0000/1.0000 | 8/0/0 1.0000/1.0000/1.0000 | 0/0/8 0.0000/0.0000/0.0000 | 0 |

| candidates_found | confirmed | rejected | skipped | failed_batches | attempts | invariant |
|---|---|---|---|---|---|---|
| 0 | 0 | 0 | 0 | 0 | 0 | OK |

- `skipped` share: 0.0000 (ADR-006 §1: <= 0.10 across the 17 gold briefs).

## Gold audit

- Total gold requirements: 116

### Pairs not in `competency.roles`

None (0 errors; all 116 gold pairs satisfy `role_id in competency.roles`).

### Pairs not in `role.core_competencies` (informational)

| Brief | role_id | competency_id |
|-------|---------|---------------|
| brief_001_simple_web_mvp | devops_engineer | cloud_yandex |
| brief_003_enterprise_1c | system_analyst | an_integration |
| brief_003_enterprise_1c | backend_engineer | fw_spring |
| brief_003_enterprise_1c | backend_engineer | arch_integration_patterns |
| brief_004_k8s_migration | devops_engineer | obs_grafana |
| brief_004_k8s_migration | devops_engineer | obs_opentelemetry |
| brief_005_legacy_modernization | devops_engineer | cloud_docker |
| brief_008_mobile_banking | system_analyst | an_integration |
| brief_009_esb_messaging | backend_engineer | mq_kafka |
| brief_009_esb_messaging | system_analyst | an_integration |
| brief_009_esb_messaging | backend_engineer | arch_integration_patterns |
| brief_011_ambiguous_pipelines | devops_engineer | cloud_docker |
| brief_012_implicit_pci_dss | backend_engineer | dom_fintech |
| brief_014_complex_portal | frontend_engineer | web_accessibility |
| brief_015_telecom_migration | business_analyst | dom_telecom |
| brief_015_telecom_migration | backend_engineer | mq_kafka |
| brief_016_ecommerce | ml_engineer | ml_recsys |
| brief_016_ecommerce | backend_engineer | dom_retail |
| brief_017_data_lake | data_engineer | dwh_modeling |

### Evidence errors in gold

None.
