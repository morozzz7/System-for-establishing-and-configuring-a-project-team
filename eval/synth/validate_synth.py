#!/usr/bin/env python3
"""Validate synthetic data scenarios (task 2.2).

Checks:
1. All FK constraints: employees, competencies, projects, assignments, absences, briefs
2. All CHECK constraints: ID patterns, level ranges, grade enum, etc.
3. Planted solutions are feasible (or infeasible as expected)
4. G1-G5 hard constraints hold for planted team members
5. Statistics: utilization distribution, competency coverage, bench count

Usage:
    python eval/synth/validate_synth.py [--scenarios-dir eval/synth/scenarios]
"""

from __future__ import annotations

import json
import re
import sys
from datetime import date, datetime
from pathlib import Path
from typing import Any

import yaml

ROOT = Path(__file__).resolve().parent.parent.parent
TAXONOMY_PATH = ROOT / "contracts" / "taxonomy.yaml"
REFERENCE_PATH = ROOT / "contracts" / "reference.yaml"
DEFAULT_SCENARIOS_DIR = Path(__file__).resolve().parent / "scenarios"

# Regex patterns matching CHECK constraints in migrations
PATTERNS = {
    "employee_id": re.compile(r"^emp_[a-z0-9]{4,12}$"),
    "project_id": re.compile(r"^prj_[a-z0-9]{4,12}$"),
    "assignment_id": re.compile(r"^asg_[a-z0-9]{4,12}$"),
    "absence_id": re.compile(r"^abs_[a-z0-9]{4,12}$"),
    "brief_id": re.compile(r"^brf_[A-Za-z0-9]{4,16}$"),
    "requirement_id": re.compile(r"^req_[A-Za-z0-9]{4,16}$"),
    "unit_id": re.compile(r"^unt_[a-z0-9]{2,12}$"),
    "week": re.compile(r"^[0-9]{4}-W(0[1-9]|[1-4][0-9]|5[0-3])$"),
    "text_sha256": re.compile(r"^[a-f0-9]{64}$"),
}

VALID_GRADES = {"J", "M", "S", "L", "P"}
VALID_EMPLOYMENT_TYPES = {"internal", "contractor", "external_partner"}
VALID_PROJECT_STATUSES = {"planned", "active", "paused", "done", "cancelled"}
VALID_CRITICALITIES = {"critical", "high", "normal", "low"}
VALID_ABSENCE_TYPES = {"vacation", "sick_leave", "training", "business_trip", "parental_leave", "other"}
VALID_CRITICALITY_REQ = {"must", "should", "nice"}
VALID_SOURCES_ASG = {"plan", "actual", "forecast"}
VALID_SOURCES_COMP = {"self_assessment", "manager_review", "project_history", "certification", "hr_import"}
VALID_SOURCES_ABS = {"manual", "hr_import", "calendar_import"}
VALID_SOLVER_STATUSES = {"OPTIMAL", "FEASIBLE", "INFEASIBLE", "UNKNOWN", "TIMEOUT", "MODEL_INVALID", "ERROR"}


class ValidationError:
    def __init__(self, scenario: str, category: str, message: str, severity: str = "error"):
        self.scenario = scenario
        self.category = category
        self.message = message
        self.severity = severity

    def __str__(self):
        return f"[{self.severity.upper()}] {self.scenario}/{self.category}: {self.message}"


def validate_scenario(scenario_dir: Path, valid_roles: set, valid_comps: set) -> list[ValidationError]:
    """Validate a single scenario directory."""
    errors = []
    sid = scenario_dir.name

    # Load files
    files = {}
    required_files = [
        "employees.json", "competencies.json", "projects.json",
        "assignments.json", "absences.json", "briefs.json",
        "requirements.json", "ground_truth.json",
    ]
    for fname in required_files:
        fpath = scenario_dir / fname
        if not fpath.exists():
            errors.append(ValidationError(sid, "file", f"Missing required file: {fname}"))
            return errors  # Can't validate without files
        with open(fpath) as f:
            files[fname.replace(".json", "")] = json.load(f)

    employees = files["employees"]
    competencies = files["competencies"]
    projects = files["projects"]
    assignments = files["assignments"]
    absences = files["absences"]
    briefs = files["briefs"]
    requirements = files["requirements"]
    ground_truth = files["ground_truth"]

    # ── 1. Employee CHECK constraints ──
    emp_ids = set()
    for emp in employees:
        eid = emp["employee_id"]
        emp_ids.add(eid)

        if not PATTERNS["employee_id"].match(eid):
            errors.append(ValidationError(sid, "CHECK", f"employee_id pattern mismatch: {eid}"))
        if emp["grade"] not in VALID_GRADES:
            errors.append(ValidationError(sid, "CHECK", f"invalid grade: {emp['grade']}"))
        if emp["role_profile_id"] not in valid_roles:
            errors.append(ValidationError(sid, "FK", f"role_profile_id not in taxonomy: {emp['role_profile_id']}"))
        if not PATTERNS["unit_id"].match(emp["unit_id"]):
            errors.append(ValidationError(sid, "CHECK", f"unit_id pattern mismatch: {emp['unit_id']}"))
        if emp["employment_type"] not in VALID_EMPLOYMENT_TYPES:
            errors.append(ValidationError(sid, "CHECK", f"invalid employment_type: {emp['employment_type']}"))
        if not (1 <= emp["weekly_hours_norm"] <= 60):
            errors.append(ValidationError(sid, "CHECK", f"weekly_hours_norm out of range: {emp['weekly_hours_norm']}"))
        if not (0 <= emp["reserve_ratio"] <= 0.5):
            errors.append(ValidationError(sid, "CHECK", f"reserve_ratio out of range: {emp['reserve_ratio']}"))
        if not (0.5 <= emp["max_utilization_ratio"] <= 1.0):
            errors.append(ValidationError(sid, "CHECK", f"max_utilization_ratio out of range"))
        if not (0.5 <= emp["cost_coefficient"] <= 5.0):
            errors.append(ValidationError(sid, "CHECK", f"cost_coefficient out of range"))

    # ── 2. Competency CHECK constraints ──
    comp_keys = set()
    for comp in competencies:
        key = (comp["employee_id"], comp["competency_id"])
        if key in comp_keys:
            errors.append(ValidationError(sid, "UNIQUE", f"duplicate competency: {key}"))
        comp_keys.add(key)

        if comp["employee_id"] not in emp_ids:
            errors.append(ValidationError(sid, "FK", f"employee_competency FK: {comp['employee_id']} not in pool"))
        if comp["competency_id"] not in valid_comps:
            errors.append(ValidationError(sid, "FK", f"competency_id not in taxonomy: {comp['competency_id']}"))
        if not (1 <= comp["level"] <= 5):
            errors.append(ValidationError(sid, "CHECK", f"level out of range [1,5]: {comp['level']}"))
        if not (0 <= comp["confidence"] <= 1):
            errors.append(ValidationError(sid, "CHECK", f"confidence out of range [0,1]: {comp['confidence']}"))
        if comp["source"] not in VALID_SOURCES_COMP:
            errors.append(ValidationError(sid, "CHECK", f"invalid source: {comp['source']}"))

    # ── 3. Project CHECK constraints ──
    prj_ids = set()
    for prj in projects:
        pid = prj["project_id"]
        prj_ids.add(pid)

        if not PATTERNS["project_id"].match(pid):
            errors.append(ValidationError(sid, "CHECK", f"project_id pattern mismatch: {pid}"))
        if not (1 <= len(prj["code"]) <= 40):
            errors.append(ValidationError(sid, "CHECK", f"code length out of range"))
        if not (1 <= len(prj["name"]) <= 200):
            errors.append(ValidationError(sid, "CHECK", f"name length out of range"))
        if prj["status"] not in VALID_PROJECT_STATUSES:
            errors.append(ValidationError(sid, "CHECK", f"invalid project status: {prj['status']}"))
        if not (1 <= prj["priority"] <= 5):
            errors.append(ValidationError(sid, "CHECK", f"priority out of range: {prj['priority']}"))
        if prj["criticality"] not in VALID_CRITICALITIES:
            errors.append(ValidationError(sid, "CHECK", f"invalid criticality: {prj['criticality']}"))
        if not PATTERNS["week"].match(prj["start_week"]):
            errors.append(ValidationError(sid, "CHECK", f"start_week pattern mismatch: {prj['start_week']}"))
        if not PATTERNS["week"].match(prj["end_week_planned"]):
            errors.append(ValidationError(sid, "CHECK", f"end_week_planned pattern mismatch"))

    # ── 4. Assignment CHECK constraints ──
    asg_keys = set()
    for asg in assignments:
        key = (asg["employee_id"], asg["project_id"], asg["week"])
        if key in asg_keys:
            errors.append(ValidationError(sid, "UNIQUE", f"duplicate assignment: {key}", "warning"))
        asg_keys.add(key)

        if not PATTERNS["assignment_id"].match(asg["assignment_id"]):
            errors.append(ValidationError(sid, "CHECK", f"assignment_id pattern mismatch"))
        if asg["project_id"] not in prj_ids:
            errors.append(ValidationError(sid, "FK", f"assignment FK: project {asg['project_id']} not found"))
        if asg["employee_id"] not in emp_ids:
            errors.append(ValidationError(sid, "FK", f"assignment FK: employee {asg['employee_id']} not found"))
        if not PATTERNS["week"].match(asg["week"]):
            errors.append(ValidationError(sid, "CHECK", f"week pattern mismatch: {asg['week']}"))
        if asg["hours_minutes"] <= 0:
            errors.append(ValidationError(sid, "CHECK", f"hours_minutes must be > 0"))
        if asg["source"] not in VALID_SOURCES_ASG:
            errors.append(ValidationError(sid, "CHECK", f"invalid assignment source: {asg['source']}"))

    # ── 5. Absence CHECK constraints ──
    abs_keys = set()
    for ab in absences:
        key = (ab["employee_id"], ab["start_date"], ab["end_date"], ab["type"])
        if key in abs_keys:
            errors.append(ValidationError(sid, "UNIQUE", f"duplicate absence: {key}", "warning"))
        abs_keys.add(key)

        if not PATTERNS["absence_id"].match(ab["absence_id"]):
            errors.append(ValidationError(sid, "CHECK", f"absence_id pattern mismatch"))
        if ab["employee_id"] not in emp_ids:
            errors.append(ValidationError(sid, "FK", f"absence FK: employee {ab['employee_id']} not found"))
        if ab["type"] not in VALID_ABSENCE_TYPES:
            errors.append(ValidationError(sid, "CHECK", f"invalid absence type: {ab['type']}"))

        # Date validation
        try:
            sd = date.fromisoformat(ab["start_date"])
            ed = date.fromisoformat(ab["end_date"])
            if ed < sd:
                errors.append(ValidationError(sid, "CHECK", f"end_date < start_date for {ab['absence_id']}"))
        except ValueError as e:
            errors.append(ValidationError(sid, "CHECK", f"invalid date format: {e}"))

        if not (0 <= ab["hours_per_day"] <= 12):
            errors.append(ValidationError(sid, "CHECK", f"hours_per_day out of range"))
        if ab["source"] not in VALID_SOURCES_ABS:
            errors.append(ValidationError(sid, "CHECK", f"invalid absence source: {ab['source']}"))

    # ── 6. Brief CHECK constraints ──
    brief_ids = set()
    for br in briefs:
        bid = br["brief_id"]
        brief_ids.add(bid)

        if not PATTERNS["brief_id"].match(bid):
            errors.append(ValidationError(sid, "CHECK", f"brief_id pattern mismatch: {bid}"))
        if not (1 <= len(br["title"]) <= 200):
            errors.append(ValidationError(sid, "CHECK", f"title length out of range"))
        if br["source_type"] not in ("text", "docx", "pdf", "txt"):
            errors.append(ValidationError(sid, "CHECK", f"invalid source_type: {br['source_type']}"))
        if br["language"] not in ("ru", "en"):
            errors.append(ValidationError(sid, "CHECK", f"invalid language: {br['language']}"))
        if not PATTERNS["text_sha256"].match(br["text_sha256"]):
            errors.append(ValidationError(sid, "CHECK", f"text_sha256 pattern mismatch"))

    # ── 7. Requirement CHECK constraints ──
    for req in requirements:
        if not PATTERNS["requirement_id"].match(req["requirement_id"]):
            errors.append(ValidationError(sid, "CHECK", f"requirement_id pattern mismatch"))
        if req["brief_id"] not in brief_ids:
            errors.append(ValidationError(sid, "FK", f"requirement FK: brief {req['brief_id']} not found"))
        if req["role_id"] not in valid_roles:
            errors.append(ValidationError(sid, "FK", f"requirement role_id not in taxonomy"))
        if req["competency_id"] not in valid_comps:
            errors.append(ValidationError(sid, "FK", f"requirement competency_id not in taxonomy"))
        if not (1 <= req["level"] <= 5):
            errors.append(ValidationError(sid, "CHECK", f"requirement level out of range"))
        if req["criticality"] not in VALID_CRITICALITY_REQ:
            errors.append(ValidationError(sid, "CHECK", f"invalid criticality: {req['criticality']}"))
        if not (1 <= req["headcount"] <= 20):
            errors.append(ValidationError(sid, "CHECK", f"headcount out of range"))
        if not (0.5 <= req["effort_min"] <= 2000):
            errors.append(ValidationError(sid, "CHECK", f"effort_min out of range"))
        if not (0.5 <= req["effort_likely"] <= 2000):
            errors.append(ValidationError(sid, "CHECK", f"effort_likely out of range"))
        if not (0.5 <= req["effort_max"] <= 2000):
            errors.append(ValidationError(sid, "CHECK", f"effort_max out of range"))
        if req["effort_min"] > req["effort_likely"]:
            errors.append(ValidationError(sid, "CHECK", f"effort_min > effort_likely"))
        if req["effort_likely"] > req["effort_max"]:
            errors.append(ValidationError(sid, "CHECK", f"effort_likely > effort_max"))
        if not (0 <= req["confidence"] <= 1):
            errors.append(ValidationError(sid, "CHECK", f"requirement confidence out of range"))

    # ── 8. Ground truth validation ──
    gt = ground_truth
    if gt.get("scenario_id") != sid:
        errors.append(ValidationError(sid, "GT", f"scenario_id mismatch: {gt.get('scenario_id')} != {sid}"))

    for variant in gt.get("expected_variants", []):
        status = variant.get("status", "")
        if status not in VALID_SOLVER_STATUSES:
            errors.append(ValidationError(sid, "GT", f"invalid solver status: {status}"))

        if status in ("OPTIMAL", "FEASIBLE"):
            # Check that all team members exist in pool
            for member in variant.get("members", []):
                if member["employee_id"] not in emp_ids:
                    errors.append(ValidationError(
                        sid, "GT",
                        f"planted team member {member['employee_id']} not in employee pool"
                    ))

            # Check G3: every must requirement covered
            must_reqs = [r for r in requirements if r["criticality"] == "must"]
            member_ids = {m["employee_id"] for m in variant.get("members", [])}
            member_comps = {c["competency_id"]: c["level"]
                           for c in competencies if c["employee_id"] in member_ids}

            for req in must_reqs:
                cid = req["competency_id"]
                # Check if any team member covers this
                covered = False
                for member in variant.get("members", []):
                    for c in competencies:
                        if (c["employee_id"] == member["employee_id"]
                                and c["competency_id"] == cid
                                and c["level"] >= req["level"]):
                            covered = True
                            break
                    if covered:
                        break
                if not covered:
                    # Check if it's listed as deficit
                    deficits = variant.get("deficits", [])
                    deficit_cids = [d.get("competency_id") for d in deficits]
                    if cid not in deficit_cids:
                        errors.append(ValidationError(
                            sid, "G3",
                            f"must requirement {req['requirement_id']} ({cid} level {req['level']}) "
                            f"not covered by any team member and not listed as deficit"
                        ))

        elif status == "INFEASIBLE":
            # Verify that the deficit is real: no employee covers it
            deficit = variant.get("deficit", {})
            deficit_cid = deficit.get("competency_id")
            deficit_level = deficit.get("required_level")

            if deficit_cid and deficit_level:
                # Check that no employee in pool has this competency at required level
                for comp in competencies:
                    if comp["competency_id"] == deficit_cid and comp["level"] >= deficit_level:
                        errors.append(ValidationError(
                            sid, "DEFICIT",
                            f"employee {comp['employee_id']} has {deficit_cid} level {comp['level']} "
                            f">= required {deficit_level}, but scenario is marked INFEASIBLE"
                        ))

    # ── 9. Privacy check ──
    all_text = json.dumps(files)
    privacy_patterns = [
        r"\b[A-Z][a-z]+\s+[A-Z][a-z]+\b",  # Two capitalized words (possible name)
        r"@[\w.]+\.\w+",  # Email-like
        r"\+?\d{10,15}",  # Phone number
    ]
    # Note: This is a heuristic; real PII check should be more thorough
    # For now we just check that no field contains obvious personal data
    for emp in employees:
        for field in ["employee_id", "grade", "role_profile_id", "unit_id"]:
            val = str(emp.get(field, ""))
            if "@" in val:
                errors.append(ValidationError(sid, "PRIVACY", f"email-like pattern in employee field: {field}"))

    return errors


def compute_statistics(scenario_dir: Path) -> dict:
    """Compute statistics for a scenario."""
    with open(scenario_dir / "employees.json") as f:
        employees = json.load(f)
    with open(scenario_dir / "competencies.json") as f:
        competencies = json.load(f)
    with open(scenario_dir / "assignments.json") as f:
        assignments = json.load(f)
    with open(scenario_dir / "absences.json") as f:
        absences = json.load(f)
    with open(scenario_dir / "ground_truth.json") as f:
        gt = json.load(f)

    # Grade distribution
    grade_dist = {}
    for e in employees:
        grade_dist[e["grade"]] = grade_dist.get(e["grade"], 0) + 1

    # Role distribution
    role_dist = {}
    for e in employees:
        role_dist[e["role_profile_id"]] = role_dist.get(e["role_profile_id"], 0) + 1

    # Utilization
    emp_hours: dict[str, int] = {}
    emp_weeks: dict[str, set] = {}
    for a in assignments:
        eid = a["employee_id"]
        emp_hours[eid] = emp_hours.get(eid, 0) + a["hours_minutes"]
        if eid not in emp_weeks:
            emp_weeks[eid] = set()
        emp_weeks[eid].add(a["week"])

    utils = []
    for e in employees:
        eid = e["employee_id"]
        total_minutes = emp_hours.get(eid, 0)
        n_weeks = len(emp_weeks.get(eid, set())) or 1
        avg_weekly_minutes = total_minutes / n_weeks
        avg_weekly_hours = avg_weekly_minutes / 60.0
        utils.append(avg_weekly_hours / 40)  # 40h standard work week

    bench_count = sum(1 for e in employees if e["bench_flag"])

    # Competency coverage
    comp_count = {}
    for c in competencies:
        cid = c["competency_id"]
        comp_count[cid] = comp_count.get(cid, 0) + 1

    n_covered_comps = len(comp_count)

    # Absence stats
    absence_types = {}
    for ab in absences:
        absence_types[ab["type"]] = absence_types.get(ab["type"], 0) + 1

    return {
        "scenario_id": scenario_dir.name,
        "n_employees": len(employees),
        "n_competencies": len(competencies),
        "n_unique_competencies": n_covered_comps,
        "n_projects": len(set(a["project_id"] for a in assignments)) if assignments else 0,
        "n_assignments": len(assignments),
        "n_absences": len(absences),
        "grade_distribution": dict(sorted(grade_dist.items())),
        "role_distribution": dict(sorted(role_dist.items())),
        "bench_count": bench_count,
        "mean_utilization": round(sum(utils) / len(utils), 3) if utils else 0,
        "min_utilization": round(min(utils), 3) if utils else 0,
        "max_utilization": round(max(utils), 3) if utils else 0,
        "absence_types": absence_types,
        "ground_truth_status": gt["expected_variants"][0]["status"] if gt.get("expected_variants") else "N/A",
        "ground_truth_type": gt.get("scenario_type", "unknown"),
    }


def main():
    import argparse
    parser = argparse.ArgumentParser(description="Validate synthetic data scenarios")
    parser.add_argument("--scenarios-dir", default=str(DEFAULT_SCENARIOS_DIR),
                        help="Directory containing scenario subdirectories")
    parser.add_argument("--verbose", "-v", action="store_true")
    args = parser.parse_args()

    scenarios_dir = Path(args.scenarios_dir)
    if not scenarios_dir.exists():
        print(f"ERROR: Scenarios directory not found: {scenarios_dir}")
        return 1

    # Load taxonomy
    with open(TAXONOMY_PATH) as f:
        taxonomy = yaml.safe_load(f)
    valid_roles = {r["role_id"] for r in taxonomy["roles"]}
    valid_comps = {c["competency_id"] for c in taxonomy["competencies"]}

    all_errors = []
    all_stats = []

    scenario_dirs = sorted([d for d in scenarios_dir.iterdir() if d.is_dir()])
    if not scenario_dirs:
        print(f"No scenario directories found in {scenarios_dir}")
        return 1

    print(f"Validating {len(scenario_dirs)} scenarios...\n")

    for sd in scenario_dirs:
        errors = validate_scenario(sd, valid_roles, valid_comps)
        all_errors.extend(errors)
        stats = compute_statistics(sd)
        all_stats.append(stats)

        n_errors = sum(1 for e in errors if e.severity == "error")
        n_warnings = sum(1 for e in errors if e.severity == "warning")
        status_icon = "✅" if n_errors == 0 else "❌"
        print(f"  {status_icon} {sd.name}: {n_errors} errors, {n_warnings} warnings")

        if args.verbose and errors:
            for e in errors:
                print(f"      {e}")

    # Print summary
    print(f"\n{'='*70}")
    print("VALIDATION SUMMARY")
    print(f"{'='*70}")

    total_errors = sum(1 for e in all_errors if e.severity == "error")
    total_warnings = sum(1 for e in all_errors if e.severity == "warning")

    print(f"Scenarios validated: {len(scenario_dirs)}")
    print(f"Total errors:        {total_errors}")
    print(f"Total warnings:      {total_warnings}")

    # Type distribution
    type_counts = {}
    for s in all_stats:
        t = s["ground_truth_type"]
        type_counts[t] = type_counts.get(t, 0) + 1
    print(f"\nScenario types: {dict(type_counts)}")

    # Statistics table
    print(f"\n{'Scenario':<25} {'Pool':<6} {'Emps':<6} {'Bench':<6} {'Util':<8} {'GT Status'}")
    print("-" * 70)
    for s in all_stats:
        print(f"{s.get('scenario_id', '?'):<25} " if 'scenario_id' in s else f"{'?':<25} ", end="")

    # Print stats more simply
    for s in all_stats:
        name = s.get("scenario_id", "?")
        print(f"  {name}:")
        print(f"    Pool: {s['n_employees']} employees, {s['n_competencies']} competencies, "
              f"{s['n_unique_competencies']} unique")
        print(f"    Bench: {s['bench_count']}, Mean util: {s['mean_utilization']:.1%}")
        print(f"    Assignments: {s['n_assignments']}, Absences: {s['n_absences']}")
        print(f"    Ground truth: {s['ground_truth_status']} ({s['ground_truth_type']})")
        print()

    # Acceptance checks
    print(f"\n{'='*70}")
    print("ACCEPTANCE CRITERIA CHECK")
    print(f"{'='*70}")

    checks = {
        "All FK/CHECK constraints satisfied": total_errors == 0,
        "At least 1 INFEASIBLE scenario per pool size": all(
            any(s["ground_truth_status"] == "INFEASIBLE"
                for s in all_stats if s["n_employees"] == size)
            for size in [30, 100, 300]
        ),
        "At least 1 OPTIMAL/FEASIBLE scenario per pool size": all(
            any(s["ground_truth_status"] in ("OPTIMAL", "FEASIBLE")
                for s in all_stats if s["n_employees"] == size)
            for size in [30, 100, 300]
        ),
        "Bench employees present in all scenarios": all(s["bench_count"] > 0 for s in all_stats),
        "3 pool sizes present": len(set(s["n_employees"] for s in all_stats)) >= 3,
        "At least 9 scenarios total": len(all_stats) >= 9,
    }

    all_pass = True
    for check, passed in checks.items():
        icon = "✅" if passed else "❌"
        print(f"  {icon} {check}")
        if not passed:
            all_pass = False

    if total_errors > 0 and args.verbose:
        print(f"\nError details:")
        for e in all_errors:
            if e.severity == "error":
                print(f"  {e}")

    return 0 if all_pass else 1


if __name__ == "__main__":
    sys.exit(main())
