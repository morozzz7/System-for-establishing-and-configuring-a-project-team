# Extraction Baseline Report

- Generated: 2026-11-08T09:18:17.014362+00:00
- Mode: `rules`
- Prompt version: `extract-v2`
- Total wall time: 0.55s

## Aggregate metrics by stage

| Stage | Metric | TP | FP | FN | Precision | Recall | F1 |
|-------|--------|----|----|----|-----------|--------|----|
| Rules | competency | 91 | 29 | 23 | 0.7583 | 0.7982 | 0.7778 |
|  | pair | 74 | 59 | 42 | 0.5564 | 0.6379 | 0.5944 |
|  | pair_relaxed | 91 | 42 | 25 | 0.6842 | 0.7845 | 0.7309 |
| Llm | competency | 0 | 0 | 114 | 0.0000 | 0.0000 | 0.0000 |
|  | pair | 0 | 0 | 116 | 0.0000 | 0.0000 | 0.0000 |
|  | pair_relaxed | 0 | 0 | 116 | 0.0000 | 0.0000 | 0.0000 |
| Merger | competency | 91 | 29 | 23 | 0.7583 | 0.7982 | 0.7778 |
|  | pair | 74 | 59 | 42 | 0.5564 | 0.6379 | 0.5944 |
|  | pair_relaxed | 91 | 42 | 25 | 0.6842 | 0.7845 | 0.7309 |
| Final Api | competency | 69 | 7 | 45 | 0.9079 | 0.6053 | 0.7263 |
|  | pair | 63 | 26 | 53 | 0.7079 | 0.5431 | 0.6146 |
|  | pair_relaxed | 69 | 20 | 47 | 0.7753 | 0.5948 | 0.6732 |

*Pair* = exact `(role_id, competency_id)`. *Pair\** = relaxed: predicted role must be in `competency.roles`.

## Per-brief metrics

| Brief | Stage | Gold | Pred | Comp F1 | Pair F1 | Pair* F1 | Notes |
|-------|-------|------|------|---------|---------|----------|-------|
| brief_001_simple_web_mvp | rules | 7 | 8 | 0.933 | 0.667 | 0.933 |  |
|  | llm | 7 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 7 | 8 | 0.933 | 0.667 | 0.933 |  |
|  | final_api | 7 | 6 | 0.923 | 0.615 | 0.923 |  |
| brief_002_data_dwh | rules | 7 | 10 | 0.625 | 0.588 | 0.588 |  |
|  | llm | 7 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 7 | 10 | 0.625 | 0.588 | 0.588 |  |
|  | final_api | 7 | 6 | 0.833 | 0.769 | 0.769 |  |
| brief_003_enterprise_1c | rules | 5 | 7 | 0.833 | 0.667 | 0.833 |  |
|  | llm | 5 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 5 | 7 | 0.833 | 0.667 | 0.833 |  |
|  | final_api | 5 | 2 | 0.571 | 0.286 | 0.571 |  |
| brief_004_k8s_migration | rules | 6 | 8 | 0.714 | 0.714 | 0.714 |  |
|  | llm | 6 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 6 | 8 | 0.714 | 0.714 | 0.714 |  |
|  | final_api | 6 | 5 | 0.545 | 0.545 | 0.545 |  |
| brief_005_legacy_modernization | rules | 7 | 8 | 0.714 | 0.533 | 0.667 |  |
|  | llm | 7 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 7 | 8 | 0.714 | 0.533 | 0.667 |  |
|  | final_api | 7 | 6 | 0.667 | 0.615 | 0.615 |  |
| brief_006_ml_recommendation | rules | 9 | 9 | 0.824 | 0.778 | 0.778 |  |
|  | llm | 9 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 9 | 9 | 0.824 | 0.778 | 0.778 |  |
|  | final_api | 9 | 7 | 0.933 | 0.875 | 0.875 |  |
| brief_007_bi_dashboards | rules | 3 | 2 | 0.400 | 0.400 | 0.400 |  |
|  | llm | 3 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 3 | 2 | 0.400 | 0.400 | 0.400 |  |
|  | final_api | 3 | 1 | 0.500 | 0.500 | 0.500 |  |
| brief_008_mobile_banking | rules | 8 | 11 | 0.588 | 0.421 | 0.526 |  |
|  | llm | 8 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 8 | 11 | 0.588 | 0.421 | 0.526 |  |
|  | final_api | 8 | 8 | 0.571 | 0.500 | 0.500 |  |
| brief_009_esb_messaging | rules | 6 | 9 | 0.857 | 0.533 | 0.800 |  |
|  | llm | 6 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 6 | 9 | 0.857 | 0.533 | 0.800 |  |
|  | final_api | 6 | 4 | 0.667 | 0.600 | 0.600 |  |
| brief_010_devops_platform | rules | 5 | 7 | 0.909 | 0.667 | 0.833 |  |
|  | llm | 5 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 5 | 7 | 0.909 | 0.667 | 0.833 |  |
|  | final_api | 5 | 5 | 0.889 | 0.800 | 0.800 |  |
| brief_011_ambiguous_pipelines | rules | 4 | 4 | 0.750 | 0.500 | 0.750 |  |
|  | llm | 4 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 4 | 4 | 0.750 | 0.500 | 0.750 |  |
|  | final_api | 4 | 2 | 0.667 | 0.333 | 0.667 |  |
| brief_012_implicit_pci_dss | rules | 7 | 8 | 0.769 | 0.667 | 0.667 |  |
|  | llm | 7 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 7 | 8 | 0.769 | 0.667 | 0.667 |  |
|  | final_api | 7 | 6 | 0.727 | 0.615 | 0.615 |  |
| brief_013_contradictory_junior | rules | 5 | 3 | 0.750 | 0.750 | 0.750 |  |
|  | llm | 5 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 5 | 3 | 0.750 | 0.750 | 0.750 |  |
|  | final_api | 5 | 3 | 0.750 | 0.750 | 0.750 |  |
| brief_014_complex_portal | rules | 14 | 14 | 1.000 | 0.786 | 1.000 |  |
|  | llm | 14 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 14 | 14 | 1.000 | 0.786 | 1.000 |  |
|  | final_api | 14 | 11 | 0.880 | 0.880 | 0.880 |  |
| brief_015_telecom_migration | rules | 7 | 8 | 0.714 | 0.133 | 0.667 |  |
|  | llm | 7 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 7 | 8 | 0.714 | 0.133 | 0.667 |  |
|  | final_api | 7 | 5 | 0.545 | 0.167 | 0.500 |  |
| brief_016_ecommerce | rules | 8 | 9 | 0.714 | 0.588 | 0.588 |  |
|  | llm | 8 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 8 | 9 | 0.714 | 0.588 | 0.588 |  |
|  | final_api | 8 | 6 | 0.545 | 0.429 | 0.429 |  |
| brief_017_data_lake | rules | 8 | 8 | 0.714 | 0.500 | 0.625 |  |
|  | llm | 8 | 0 | 0.000 | 0.000 | 0.000 |  |
|  | merger | 8 | 8 | 0.714 | 0.500 | 0.625 |  |
|  | final_api | 8 | 6 | 0.667 | 0.571 | 0.571 |  |

## Gold audit

- Total gold requirements: 116

### Pairs not in `competency.roles`

None.

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
