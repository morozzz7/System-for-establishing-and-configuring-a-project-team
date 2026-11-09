# Extraction Evaluation & Baseline Report

- Generated: 2026-11-08T18:49:10.557000+00:00
- Mode: `fixtures`
- Runs per brief: `1`
- Prompt version: `extract-v3`
- Hold-out briefs (5): `brief_005_legacy_modernization, brief_007_bi_dashboards, brief_010_devops_platform, brief_015_telecom_migration, brief_016_ecommerce`
- Total wall time: 9.69s

## Aggregate metrics — All briefs (17 briefs, 116 gold requirements)

| Stage | Metric | TP | FP | FN | Precision | Recall | F1 / Acc | Spread (min..max, std) |
|-------|--------|----|----|----|-----------|--------|----------|------------------------|
| `rules` | role | 55 | 7 | 5 | 0.8871 | 0.9167 | 0.9016 | — |
|  | competency | 91 | 23 | 23 | 0.7982 | 0.7982 | 0.7982 | — |
|  | pair | 89 | 28 | 27 | 0.7607 | 0.7672 | 0.7639 | — |
|  | pair_relaxed | 92 | 25 | 24 | 0.7863 | 0.7931 | 0.7897 | — |
|  | role_accuracy | 88 | 3 | 3 | 0.9670 | 0.9670 | 0.9670 | — |
|  | headcount_accuracy | 62 | 27 | 27 | 0.6966 | 0.6966 | 0.6966 | — |
|  | headcount_multi | 13 | 9 | 9 | 0.5909 | 0.5909 | 0.5909 | — |
| `llm_raw` | role | 59 | 0 | 1 | 1.0000 | 0.9833 | 0.9916 | — |
|  | competency | 107 | 10 | 7 | 0.9145 | 0.9386 | 0.9264 | — |
|  | pair | 109 | 11 | 7 | 0.9083 | 0.9397 | 0.9237 | — |
|  | pair_relaxed | 109 | 11 | 7 | 0.9083 | 0.9397 | 0.9237 | — |
|  | role_accuracy | 107 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 102 | 7 | 7 | 0.9358 | 0.9358 | 0.9358 | — |
|  | headcount_multi | 20 | 2 | 2 | 0.9091 | 0.9091 | 0.9091 | — |
| `llm_validated` | role | 58 | 0 | 2 | 1.0000 | 0.9667 | 0.9831 | — |
|  | competency | 107 | 7 | 7 | 0.9386 | 0.9386 | 0.9386 | — |
|  | pair | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | pair_relaxed | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | role_accuracy | 107 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 101 | 8 | 8 | 0.9266 | 0.9266 | 0.9266 | — |
|  | headcount_multi | 20 | 2 | 2 | 0.9091 | 0.9091 | 0.9091 | — |
| `final_api` | role | 58 | 0 | 2 | 1.0000 | 0.9667 | 0.9831 | — |
|  | competency | 107 | 7 | 7 | 0.9386 | 0.9386 | 0.9386 | — |
|  | pair | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | pair_relaxed | 109 | 8 | 7 | 0.9316 | 0.9397 | 0.9356 | — |
|  | role_accuracy | 107 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 101 | 8 | 8 | 0.9266 | 0.9266 | 0.9266 | — |
|  | headcount_multi | 20 | 2 | 2 | 0.9091 | 0.9091 | 0.9091 | — |

## Aggregate metrics — Hold-out split (5 briefs, 30 gold requirements)

| Stage | Metric | TP | FP | FN | Precision | Recall | F1 / Acc | Spread (min..max, std) |
|-------|--------|----|----|----|-----------|--------|----------|------------------------|
| `rules` | role | 17 | 2 | 2 | 0.8947 | 0.8947 | 0.8947 | — |
|  | competency | 21 | 6 | 9 | 0.7778 | 0.7000 | 0.7368 | — |
|  | pair | 20 | 8 | 10 | 0.7143 | 0.6667 | 0.6897 | — |
|  | pair_relaxed | 21 | 7 | 9 | 0.7500 | 0.7000 | 0.7241 | — |
|  | role_accuracy | 20 | 1 | 1 | 0.9524 | 0.9524 | 0.9524 | — |
|  | headcount_accuracy | 13 | 7 | 7 | 0.6500 | 0.6500 | 0.6500 | — |
|  | headcount_multi | 3 | 4 | 4 | 0.4286 | 0.4286 | 0.4286 | — |
| `llm_raw` | role | 18 | 0 | 1 | 1.0000 | 0.9474 | 0.9730 | — |
|  | competency | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | pair | 26 | 5 | 4 | 0.8387 | 0.8667 | 0.8525 | — |
|  | pair_relaxed | 26 | 5 | 4 | 0.8387 | 0.8667 | 0.8525 | — |
|  | role_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 23 | 3 | 3 | 0.8846 | 0.8846 | 0.8846 | — |
|  | headcount_multi | 6 | 1 | 1 | 0.8571 | 0.8571 | 0.8571 | — |
| `llm_validated` | role | 18 | 0 | 1 | 1.0000 | 0.9474 | 0.9730 | — |
|  | competency | 26 | 3 | 4 | 0.8966 | 0.8667 | 0.8814 | — |
|  | pair | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | pair_relaxed | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | role_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 23 | 3 | 3 | 0.8846 | 0.8846 | 0.8846 | — |
|  | headcount_multi | 6 | 1 | 1 | 0.8571 | 0.8571 | 0.8571 | — |
| `final_api` | role | 18 | 0 | 1 | 1.0000 | 0.9474 | 0.9730 | — |
|  | competency | 26 | 3 | 4 | 0.8966 | 0.8667 | 0.8814 | — |
|  | pair | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | pair_relaxed | 26 | 4 | 4 | 0.8667 | 0.8667 | 0.8667 | — |
|  | role_accuracy | 26 | 0 | 0 | 1.0000 | 1.0000 | 1.0000 | — |
|  | headcount_accuracy | 23 | 3 | 3 | 0.8846 | 0.8846 | 0.8846 | — |
|  | headcount_multi | 6 | 1 | 1 | 0.8571 | 0.8571 | 0.8571 | — |

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
| brief_001_simple_web_mvp | dev | `rules` | 7 | 8 | 1.000 | 0.933 | 0.933 | 0.933 | 1.000 | 1.000 | 0/0/0 | — | 51 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/528 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/528 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/528 | 0 |  |
| brief_002_data_dwh | dev | `rules` | 7 | 9 | 1.000 | 0.667 | 0.667 | 0.667 | 1.000 | 1.000 | 0/0/0 | — | 52 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/563 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/563 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1925/563 | 0 |  |
| brief_003_enterprise_1c | dev | `rules` | 5 | 8 | 1.000 | 0.833 | 0.769 | 0.769 | 1.000 | 1.000 | 0/0/0 | — | 49 |  |
|  |  | `llm_raw` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/403 | 0 |  |
|  |  | `llm_validated` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/403 | 0 |  |
|  |  | `final_api` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/403 | 0 |  |
| brief_004_k8s_migration | dev | `rules` | 6 | 8 | 1.000 | 0.714 | 0.714 | 0.714 | 1.000 | 1.000 | 0/0/0 | — | 50 |  |
|  |  | `llm_raw` | 6 | 6 | 1.000 | 0.833 | 0.833 | 0.833 | 1.000 | 1.000 | 0/0/0 | 1910/475 | 0 |  |
|  |  | `llm_validated` | 6 | 6 | 1.000 | 0.833 | 0.833 | 0.833 | 1.000 | 1.000 | 0/0/0 | 1910/475 | 0 |  |
|  |  | `final_api` | 6 | 6 | 1.000 | 0.833 | 0.833 | 0.833 | 1.000 | 1.000 | 0/0/0 | 1910/475 | 0 |  |
| brief_005_legacy_modernization | hold-out | `rules` | 7 | 7 | 1.000 | 0.769 | 0.769 | 0.769 | 1.000 | 0.600 | 0/0/0 | — | 50 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 0.923 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | 1925/574 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 0.923 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | 1925/574 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 0.923 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | 1925/574 | 0 |  |
| brief_006_ml_recommendation | dev | `rules` | 9 | 9 | 1.000 | 0.875 | 0.889 | 0.889 | 1.000 | 1.000 | 0/0/0 | — | 60 |  |
|  |  | `llm_raw` | 9 | 10 | 1.000 | 0.941 | 0.947 | 0.947 | 1.000 | 1.000 | 0/0/0 | 1970/777 | 0 |  |
|  |  | `llm_validated` | 9 | 10 | 1.000 | 0.941 | 0.947 | 0.947 | 1.000 | 1.000 | 0/0/0 | 1970/777 | 0 |  |
|  |  | `final_api` | 9 | 10 | 1.000 | 0.941 | 0.947 | 0.947 | 1.000 | 1.000 | 0/0/0 | 1970/777 | 0 |  |
| brief_007_bi_dashboards | hold-out | `rules` | 3 | 2 | 0.500 | 0.400 | 0.400 | 0.400 | 1.000 | 1.000 | 0/0/0 | — | 49 |  |
|  |  | `llm_raw` | 3 | 3 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1865/245 | 0 |  |
|  |  | `llm_validated` | 3 | 3 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1865/245 | 0 |  |
|  |  | `final_api` | 3 | 3 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1865/245 | 0 |  |
| brief_008_mobile_banking | dev | `rules` | 8 | 9 | 0.833 | 0.625 | 0.500 | 0.625 | 0.800 | 0.750 | 0/0/0 | — | 50 |  |
|  |  | `llm_raw` | 8 | 9 | 1.000 | 0.706 | 0.706 | 0.706 | 1.000 | 1.000 | 0/0/0 | 1955/719 | 0 |  |
|  |  | `llm_validated` | 8 | 8 | 0.833 | 0.750 | 0.750 | 0.750 | 1.000 | 1.000 | 0/1/0 | 1955/719 | 0 |  |
|  |  | `final_api` | 8 | 8 | 0.833 | 0.750 | 0.750 | 0.750 | 1.000 | 1.000 | 0/1/0 | 1955/719 | 0 |  |
| brief_009_esb_messaging | dev | `rules` | 6 | 8 | 1.000 | 0.857 | 0.857 | 0.857 | 1.000 | 0.500 | 0/0/0 | — | 48 |  |
|  |  | `llm_raw` | 6 | 6 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.833 | 0/0/0 | 1910/491 | 0 |  |
|  |  | `llm_validated` | 6 | 6 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.667 | 0/0/0 | 1910/491 | 0 |  |
|  |  | `final_api` | 6 | 6 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.667 | 0/0/0 | 1910/491 | 0 |  |
| brief_010_devops_platform | hold-out | `rules` | 5 | 6 | 1.000 | 0.909 | 0.909 | 0.909 | 1.000 | 1.000 | 0/0/0 | — | 48 |  |
|  |  | `llm_raw` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/390 | 0 |  |
|  |  | `llm_validated` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/390 | 0 |  |
|  |  | `final_api` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/390 | 0 |  |
| brief_011_ambiguous_pipelines | dev | `rules` | 4 | 3 | 1.000 | 0.857 | 0.857 | 0.857 | 1.000 | 1.000 | 0/0/0 | — | 46 |  |
|  |  | `llm_raw` | 4 | 4 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1880/329 | 0 |  |
|  |  | `llm_validated` | 4 | 4 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1880/329 | 0 |  |
|  |  | `final_api` | 4 | 4 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1880/329 | 0 |  |
| brief_012_implicit_pci_dss | dev | `rules` | 7 | 6 | 0.750 | 0.833 | 0.833 | 0.833 | 1.000 | 0.400 | 0/0/0 | — | 48 |  |
|  |  | `llm_raw` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.714 | 0/0/0 | 1925/554 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.714 | 0/0/0 | 1925/554 | 0 |  |
|  |  | `final_api` | 7 | 7 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.714 | 0/0/0 | 1925/554 | 0 |  |
| brief_013_contradictory_junior | dev | `rules` | 5 | 3 | 0.667 | 0.750 | 0.750 | 0.750 | 1.000 | 1.000 | 0/0/0 | — | 45 |  |
|  |  | `llm_raw` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/397 | 0 |  |
|  |  | `llm_validated` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/397 | 0 |  |
|  |  | `final_api` | 5 | 5 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 1895/397 | 0 |  |
| brief_014_complex_portal | dev | `rules` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.286 | 0/0/0 | — | 51 |  |
|  |  | `llm_raw` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 2030/1103 | 0 |  |
|  |  | `llm_validated` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 2030/1103 | 0 |  |
|  |  | `final_api` | 14 | 14 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0/0/0 | 2030/1103 | 0 |  |
| brief_015_telecom_migration | hold-out | `rules` | 7 | 10 | 0.800 | 0.714 | 0.533 | 0.667 | 0.800 | 0.750 | 0/0/0 | — | 50 |  |
|  |  | `llm_raw` | 7 | 7 | 0.800 | 0.714 | 0.714 | 0.714 | 1.000 | 0.600 | 0/0/0 | 1925/556 | 0 |  |
|  |  | `llm_validated` | 7 | 7 | 0.800 | 0.714 | 0.714 | 0.714 | 1.000 | 0.600 | 0/0/0 | 1925/556 | 0 |  |
|  |  | `final_api` | 7 | 7 | 0.800 | 0.714 | 0.714 | 0.714 | 1.000 | 0.600 | 0/0/0 | 1925/556 | 0 |  |
| brief_016_ecommerce | hold-out | `rules` | 8 | 9 | 1.000 | 0.714 | 0.714 | 0.714 | 1.000 | 0.200 | 0/0/0 | — | 48 |  |
|  |  | `llm_raw` | 8 | 9 | 1.000 | 0.824 | 0.824 | 0.824 | 1.000 | 0.857 | 0/0/0 | 1955/703 | 0 |  |
|  |  | `llm_validated` | 8 | 8 | 1.000 | 0.875 | 0.875 | 0.875 | 1.000 | 0.857 | 0/1/0 | 1955/703 | 0 |  |
|  |  | `final_api` | 8 | 8 | 1.000 | 0.875 | 0.875 | 0.875 | 1.000 | 0.857 | 0/1/0 | 1955/703 | 0 |  |
| brief_017_data_lake | dev | `rules` | 8 | 8 | 1.000 | 0.714 | 0.533 | 0.667 | 0.800 | 0.250 | 0/0/0 | — | 50 |  |
|  |  | `llm_raw` | 8 | 9 | 1.000 | 0.933 | 0.941 | 0.941 | 1.000 | 0.875 | 0/0/0 | 1955/697 | 0 |  |
|  |  | `llm_validated` | 8 | 8 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.875 | 0/1/0 | 1955/697 | 0 |  |
|  |  | `final_api` | 8 | 8 | 1.000 | 1.000 | 1.000 | 1.000 | 1.000 | 0.875 | 0/1/0 | 1955/697 | 0 |  |

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
