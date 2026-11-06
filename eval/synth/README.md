# Synthetic Data Generator (task 2.2)

Deterministic generator of employee pools with planted solutions for benchmarking the
team-formation optimizer.

## Quick start

```bash
# Generate all 9 scenarios (default seed=42)
python eval/synth/generator.py --seed 42

# Generate a specific scenario
python eval/synth/generator.py --scenario small_optimal_01

# Generate + verify constraints
python eval/synth/generator.py --seed 42 --verify

# Validate all generated scenarios
python eval/synth/validate_synth.py --verbose
```

**Note:** Generated scenarios are not committed to git (see `.gitignore`). Run the generator
before running tests or benchmarks that depend on `eval/synth/scenarios/`.

## Output structure

```
eval/synth/scenarios/
├── summary.json                          # Generation summary
├── small_deficit_01/
│   ├── employees.json                    # 30 employee records
│   ├── competencies.json                 # employee_competency records
│   ├── projects.json                     # Background projects
│   ├── assignments.json                  # Existing workload
│   ├── absences.json                     # Vacation/sick/training
│   ├── briefs.json                       # Brief record (ref to eval/gold/)
│   ├── requirements.json                 # Requirements from gold brief
│   └── ground_truth.json                 # Expected optimizer output
├── small_optimal_01/
│   └── ...
├── ... (9 scenario directories total)
```

## Scenario design

### Pool sizes
| Size | Employees | Use case |
|------|-----------|----------|
| small | 30 | Quick smoke tests, development |
| medium | 100 | Integration testing, benchmark |
| large | 300 | Scale testing, performance |

### Scenario types (3 per pool size)
| Type | Description | Expected solver status |
|------|-------------|----------------------|
| **deficit** | Required competency not available at required level | INFEASIBLE |
| **optimal** | Clean feasible case with obvious optimal team | OPTIMAL |
| **competing** | Multiple non-dominated team variants | OPTIMAL/FEASIBLE |

### Scenario table

| Scenario ID | Pool | Type | Brief | What it tests |
|---|---|---|---|---|
| `small_deficit_01` | 30 | deficit | brf_006 | ML NLP specialist missing → external hire needed |
| `small_optimal_01` | 30 | optimal | brf_001 | Simple web MVP: Go+React+PostgreSQL |
| `small_competing_01` | 30 | competing | brf_007 | BI dashboards: multiple data engineer options |
| `medium_deficit_01` | 100 | deficit | brf_006 | ML NLP deficit in larger pool |
| `medium_optimal_01` | 100 | optimal | brf_004 | K8s migration: DevOps with K8s+CI/CD+monitoring |
| `medium_competing_01` | 100 | competing | brf_002 | Retail DWH: cost vs schedule trade-off |
| `large_deficit_01` | 300 | deficit | brf_010 | Security engineer with sec_appsec ≥ 4 missing |
| `large_optimal_01` | 300 | optimal | brf_005 | Java→Go rewrite: clear optimal team |
| `large_competing_01` | 300 | competing | brf_009 | ESB: multiple experienced integration teams |

## Ground truth format

Each `ground_truth.json` contains:

```json
{
  "scenario_id": "small_optimal_01",
  "pool_size": 30,
  "brief_id": "brf_001",
  "seed": 43,
  "generated_at": "2026-11-06",
  "scenario_type": "optimal",
  "expected_variants": [
    {
      "variant_id": "var_expected_01",
      "status": "OPTIMAL",
      "solver_status": "OPTIMAL",
      "gate_pass": true,
      "gate_g1": 0, "gate_g2": 0, "gate_g3": 0, "gate_g4": 0, "gate_g5": 0,
      "members": [
        {
          "employee_id": "emp_abcd1234",
          "role_id": "backend_engineer",
          "grade": "S",
          "reason_codes": ["COVERS_MUST_COMPETENCY", "BEST_LEVEL_FIT"],
          "assigned_requirement_ids": ["req_001_001"]
        }
      ],
      "metrics": {
        "M1_competency_coverage": 1.0,
        "M2_effort_coverage": 0.95,
        "M3_level_fit": 0.92,
        "score": 0.85
      },
      "deficits": []
    }
  ]
}
```

## Database schema compliance

All generated data satisfies:
- **CHECK constraints**: ID patterns (`emp_*`, `prj_*`, `asg_*`, etc.), level ranges (1-5),
  grade enum, week ISO format, effort ordering, utilization ratios
- **FK constraints**: all references point to existing records
- **UNIQUE constraints**: no duplicate (employee, competency), (employee, project, week),
  (employee, start_date, end_date, type)

## Planted solution methodology

### Deficit scenarios
The deficit competency is deliberately excluded from all employees:
- No employee has the competency at the required level
- Other requirements may still be satisfiable
- Expected: solver returns INFEASIBLE with explicit deficit

### Optimal scenarios
The employee pool is constructed to contain the exact team needed:
- At least one employee per must-requirement with sufficient competency level
- Team members have no conflicting assignments or absences
- Expected: solver returns OPTIMAL with known team

### Competing scenarios
Multiple qualified candidates exist per requirement:
- At least 2 non-overlapping teams can be formed
- Teams differ in cost/schedule/risk trade-offs
- Expected: solver returns 2-3 non-dominated variants with Jaccard ≥ 0.25

## Reproducibility

- All randomness uses `random.Random(seed)` for deterministic generation
- Same seed + config → identical output
- Default seed: 42
- Each scenario has its own seed for independence

## Privacy

- No personal data in generated files
- Employee IDs are pseudonyms: `emp_XXXXXXXX`
- Names are not generated; only role, grade, competencies
- All data is synthetic and anonymized

## Validation

```bash
python eval/synth/validate_synth.py --verbose
```

Checks performed:
1. **File presence**: all required JSON files exist
2. **CHECK constraints**: ID patterns, ranges, enums
3. **FK constraints**: all references valid
4. **UNIQUE constraints**: no duplicates
5. **Planted solution feasibility**: team members exist, G1-G5 hold
6. **Deficit verification**: INFEASIBLE scenarios have no covering employee
7. **Privacy**: no personal data patterns detected
8. **Statistics**: utilization distribution, bench count, competency coverage

## Metrics (from METRICS.md §3.2)

| Metric | Target | How tested |
|--------|--------|-----------|
| Feasibility recall | ≥ 98% | Optimizer finds planted solution on OPTIMAL scenarios |
| Determinism | 100% | Same seed → same output (automated diff) |
| Baseline superiority | ≥ +10% vs random/greedy | Tested in task 4.6 |

## Configuration

Edit `config.yaml` to:
- Add new scenarios
- Change pool sizes
- Assign different briefs to scenarios
- Adjust deficit parameters
- Change seeds

## Dependencies

- Python 3.10+
- PyYAML (`pip install pyyaml`)
- No other dependencies (stdlib only besides YAML)
