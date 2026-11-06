#!/usr/bin/env python3
"""Synthetic data generator for optimizer benchmarking (task 2.2).

Produces deterministic employee pools with planted solutions for benchmarking
the team-formation optimizer. Generates 9 scenarios: 3 pool sizes (30/100/300)
× 3 types (deficit/optimal/competing).

Usage:
    python eval/synth/generator.py --seed 42
    python eval/synth/generator.py --config eval/synth/config.yaml --seed 42
"""

from __future__ import annotations

import argparse
import hashlib
import json
import math
import os
import random
import string
import sys
from copy import deepcopy
from datetime import date, timedelta
from pathlib import Path
from typing import Any

import yaml


# ═══════════════════════════════════════════════════════════════════════════════
# Constants
# ═══════════════════════════════════════════════════════════════════════════════

ROOT = Path(__file__).resolve().parent.parent.parent  # repo root
TAXONOMY_PATH = ROOT / "contracts" / "taxonomy.yaml"
REFERENCE_PATH = ROOT / "contracts" / "reference.yaml"
GOLD_DIR = ROOT / "eval" / "gold"
OUTPUT_DIR = Path(__file__).resolve().parent / "scenarios"

HOURS_PER_WEEK = 40
HOURS_PER_DAY = 8
WORKING_DAYS_PER_WEEK = 5

# Grade distributions (cumulative thresholds)
GRADE_DISTRIBUTION = [
    ("J", 0.25),
    ("M", 0.55),  # 0.25 + 0.30
    ("S", 0.80),  # 0.55 + 0.25
    ("L", 0.95),  # 0.80 + 0.15
    ("P", 1.00),  # 0.95 + 0.05
]

GRADE_LEVEL_RANGE = {
    "J": (1, 2),
    "M": (2, 3),
    "S": (3, 4),
    "L": (4, 5),
    "P": (5, 5),
}

GRADE_COST = {
    "J": 1.0,
    "M": 1.4,
    "S": 1.9,
    "L": 2.6,
    "P": 3.4,
}

# Role distribution weights (approximate share of total headcount)
ROLE_WEIGHTS = {
    "backend_engineer": 0.22,
    "frontend_engineer": 0.13,
    "qa_engineer": 0.11,
    "devops_engineer": 0.09,
    "data_engineer": 0.08,
    "ml_engineer": 0.05,
    "business_analyst": 0.09,
    "system_analyst": 0.06,
    "project_manager": 0.06,
    "ux_ui_designer": 0.04,
    "solution_architect": 0.04,
    "security_engineer": 0.03,
}

# Russian production calendar 2027 holidays (simplified: New Year + May + June vacations)
# Format: (start_date, end_date) for company-wide or typical vacation periods
VACATION_PERIODS = [
    (date(2027, 1, 1), date(2027, 1, 8)),    # New Year holidays
    (date(2027, 2, 22), date(2027, 2, 24)),   # Defender of Fatherland
    (date(2027, 3, 8), date(2027, 3, 8)),     # International Women's Day
    (date(2027, 5, 1), date(2027, 5, 3)),     # May holidays
    (date(2027, 5, 9), date(2027, 5, 9)),     # Victory Day
    (date(2027, 6, 12), date(2027, 6, 12)),   # Russia Day
    (date(2027, 7, 1), date(2027, 8, 31)),    # Summer vacation season (employees take turns)
    (date(2027, 11, 4), date(2027, 11, 4)),   # Unity Day
]


# ═══════════════════════════════════════════════════════════════════════════════
# Data loading
# ═══════════════════════════════════════════════════════════════════════════════

def load_taxonomy() -> dict:
    """Load contracts/taxonomy.yaml."""
    with open(TAXONOMY_PATH) as f:
        return yaml.safe_load(f)


def load_reference() -> dict:
    """Load contracts/reference.yaml."""
    with open(REFERENCE_PATH) as f:
        return yaml.safe_load(f)


def load_gold_briefs() -> dict[str, list[dict]]:
    """Load all gold briefs from eval/gold/. Returns {brief_id: [requirements]}.

    Gold briefs may use IDs that don't match DB CHECK constraints (e.g. brf_001
    is only 3 chars after prefix, but DB requires 4-16). This function remaps
    IDs to valid DB format:
      brf_001 → brf_0001  (pad to 4 alphanumeric chars)
      req_001_001 → req_001001  (strip internal underscores)
    """
    briefs = {}
    for d in sorted(GOLD_DIR.iterdir()):
        if not d.is_dir() or d.name.startswith(".") or d.name.startswith("_"):
            continue
        gold_file = d / "gold.json"
        if not gold_file.exists():
            continue
        with open(gold_file) as f:
            reqs = json.load(f)
        if reqs:
            brief_id = reqs[0]["brief_id"]
            # Remap to DB-valid IDs
            brief_id_db = remap_brief_id(brief_id)
            for r in reqs:
                r["brief_id"] = brief_id_db
                r["requirement_id"] = remap_requirement_id(r["requirement_id"])
            briefs[brief_id_db] = reqs
    return briefs


def remap_brief_id(gold_id: str) -> str:
    """Remap gold brief_id to DB-valid format. brf_001 → brf_0001."""
    # Extract numeric part after 'brf_'
    prefix = "brf_"
    if gold_id.startswith(prefix):
        numeric = gold_id[len(prefix):]
        # Pad to at least 4 chars
        padded = numeric.zfill(4)
        return f"{prefix}{padded}"
    return gold_id


def remap_requirement_id(gold_id: str) -> str:
    """Remap gold requirement_id to DB-valid format. req_001_001 → req_001001."""
    prefix = "req_"
    if gold_id.startswith(prefix):
        rest = gold_id[len(prefix):]
        # Replace underscores with nothing to make it alphanumeric
        alphanumeric = rest.replace("_", "")
        return f"{prefix}{alphanumeric}"
    return gold_id


def get_must_requirements(gold_reqs: list[dict]) -> list[dict]:
    """Filter requirements to only 'must' criticality."""
    return [r for r in gold_reqs if r["criticality"] == "must"]


def get_required_competencies(gold_reqs: list[dict]) -> dict[str, int]:
    """Get {competency_id: max_required_level} from requirements."""
    result = {}
    for r in gold_reqs:
        cid = r["competency"]["competency_id"]
        lvl = r["level"]
        if cid not in result or result[cid] < lvl:
            result[cid] = lvl
    return result


# ═══════════════════════════════════════════════════════════════════════════════
# ID generator
# ═══════════════════════════════════════════════════════════════════════════════

class IDGen:
    """Deterministic ID generator with collision avoidance."""

    def __init__(self, prefix: str, rng: random.Random):
        self.prefix = prefix
        self.rng = rng
        self.used: set[str] = set()

    def next(self) -> str:
        charset = string.ascii_lowercase + string.digits
        for _ in range(10000):
            length = self.rng.randint(4, 10)
            suffix = "".join(self.rng.choices(charset, k=length))
            candidate = f"{self.prefix}{suffix}"
            if candidate not in self.used:
                self.used.add(candidate)
                return candidate
        raise RuntimeError(f"Cannot generate unique {self.prefix} ID after 10000 attempts")


# ═══════════════════════════════════════════════════════════════════════════════
# Week utilities
# ═══════════════════════════════════════════════════════════════════════════════

def iso_week(year: int, week: int) -> str:
    return f"{year}-W{week:02d}"


def week_to_monday(year: int, week: int) -> date:
    """Return the Monday of the given ISO week."""
    jan4 = date(year, 1, 4)
    monday_of_week1 = jan4 - timedelta(days=jan4.isoweekday() - 1)
    return monday_of_week1 + timedelta(weeks=week - 1)


def weeks_in_range(start_week: str, end_week: str) -> list[str]:
    """List all ISO weeks between start_week and end_week inclusive."""
    sy, sw = int(start_week[:4]), int(start_week[-2:])
    ey, ew = int(end_week[:4]), int(end_week[-2:])
    result = []
    y, w = sy, sw
    while (y, w) <= (ey, ew):
        result.append(iso_week(y, w))
        w += 1
        if w > 52:
            # Check if this year has week 53
            dec28 = date(y, 12, 28)
            if dec28.isocalendar()[1] == 53 and w == 53:
                pass  # week 53 exists
            else:
                w = 1
                y += 1
                continue
            if w > 53:
                w = 1
                y += 1
    return result


def week_days(week_str: str) -> int:
    """Number of working days (Mon-Fri) in the given ISO week."""
    y, w = int(week_str[:4]), int(week_str[-2:])
    monday = week_to_monday(y, w)
    count = 0
    for i in range(5):
        d = monday + timedelta(days=i)
        if d.weekday() < 5:  # Mon=0 .. Fri=4
            count += 1
    return count


# ═══════════════════════════════════════════════════════════════════════════════
# Employee generation
# ═══════════════════════════════════════════════════════════════════════════════

def assign_grade(rng: random.Random) -> str:
    r = rng.random()
    for grade, threshold in GRADE_DISTRIBUTION:
        if r < threshold:
            return grade
    return "P"


def assign_role(rng: random.Random, role_counts: dict[str, int]) -> str:
    """Assign a role respecting target counts."""
    available = [r for r, count in role_counts.items() if count > 0]
    if not available:
        # All roles filled; pick least-filled relative to weight
        available = list(role_counts.keys())
    weights = [ROLE_WEIGHTS.get(r, 0.05) for r in available]
    total = sum(weights)
    if total == 0:
        return rng.choice(available)
    r = rng.random() * total
    cum = 0
    for role, w in zip(available, weights):
        cum += w
        if r <= cum:
            return role
    return available[-1]


def generate_employee_competencies(
    rng: random.Random,
    role_id: str,
    grade: str,
    taxonomy: dict,
) -> list[dict]:
    """Generate competency records for an employee based on role and grade."""
    role_def = None
    for r in taxonomy["roles"]:
        if r["role_id"] == role_id:
            role_def = r
            break
    if not role_def:
        return []

    level_min, level_max = GRADE_LEVEL_RANGE[grade]
    core = role_def.get("core_competencies", [])

    # Pick 3-6 core competencies
    n_core = min(len(core), rng.randint(3, max(3, len(core))))
    selected_core = rng.sample(core, n_core) if n_core < len(core) else list(core)

    # Pick 1-3 additional competencies from the taxonomy that relate to this role
    all_comp_ids = [c["competency_id"] for c in taxonomy["competencies"]]
    related = [
        c["competency_id"]
        for c in taxonomy["competencies"]
        if role_id in c.get("roles", []) and c["competency_id"] not in core
    ]
    n_extra = min(len(related), rng.randint(0, 3))
    extra = rng.sample(related, n_extra) if n_extra > 0 and related else []

    competencies = []
    sources = ["self_assessment", "manager_review", "project_history", "certification"]
    for cid in selected_core + extra:
        level = rng.randint(level_min, level_max)
        confidence = round(rng.uniform(0.6, 1.0), 2)
        # last_used within the past year
        days_ago = rng.randint(0, 365)
        last_used = (date(2026, 11, 6) - timedelta(days=days_ago)).isoformat()
        competencies.append({
            "competency_id": cid,
            "level": level,
            "confidence": confidence,
            "last_used": last_used,
            "source": rng.choice(sources),
        })

    return competencies


def generate_employees(
    rng: random.Random,
    pool_size: int,
    taxonomy: dict,
    units: list[str],
    id_gen: IDGen,
    ensure_competencies: dict[str, int] | None = None,
    ensure_bench_count: int = 0,
) -> tuple[list[dict], list[dict]]:
    """Generate employees and their competencies.

    Args:
        ensure_competencies: If set, guarantee at least 2 employees have these
                             competencies at the required levels (for OPTIMAL scenarios).
        ensure_bench_count: Number of employees to mark as bench_flag=true.

    Returns:
        (employees, employee_competencies)
    """
    # Compute role counts
    role_counts = {}
    remaining = pool_size
    sorted_roles = sorted(ROLE_WEIGHTS.keys(), key=lambda r: ROLE_WEIGHTS[r], reverse=True)
    for i, role in enumerate(sorted_roles):
        if i == len(sorted_roles) - 1:
            role_counts[role] = remaining
        else:
            count = max(1, round(pool_size * ROLE_WEIGHTS[role]))
            count = min(count, remaining - (len(sorted_roles) - i - 1))
            role_counts[role] = count
            remaining -= count

    employees = []
    all_competencies = []

    # Track which competencies we've ensured
    ensured = set()

    for role_id, count in role_counts.items():
        for _ in range(count):
            emp_id = id_gen.next()
            grade = assign_grade(rng)
            unit = rng.choice(units)

            # Bench flag: ~15% of employees
            bench_flag = ensure_bench_count > 0 and len(employees) < ensure_bench_count
            if bench_flag:
                ensure_bench_count -= 1

            emp = {
                "employee_id": emp_id,
                "grade": grade,
                "role_profile_id": role_id,
                "unit_id": unit,
                "employment_type": "internal",
                "bench_flag": bench_flag,
                "weekly_hours_norm": HOURS_PER_WEEK,
                "reserve_ratio": 0.2,
                "max_utilization_ratio": round(rng.uniform(0.8, 1.0), 2),
                "cost_coefficient": GRADE_COST[grade],
                "active": True,
            }
            employees.append(emp)

            # Generate competencies
            comps = generate_employee_competencies(rng, role_id, grade, taxonomy)
            for c in comps:
                c["employee_id"] = emp_id
                all_competencies.append(c)

            # Track ensured competencies
            if ensure_competencies:
                for c in comps:
                    if c["competency_id"] in ensure_competencies:
                        req_level = ensure_competencies[c["competency_id"]]
                        if c["level"] >= req_level:
                            ensured.add(c["competency_id"])

    # Ensure required competencies exist at required levels
    if ensure_competencies:
        for cid, req_level in ensure_competencies.items():
            if cid not in ensured:
                # Find or create an employee with this competency at required level
                # Pick a random employee whose role supports this competency
                role_for_comp = None
                for comp_def in taxonomy["competencies"]:
                    if comp_def["competency_id"] == cid:
                        roles = comp_def.get("roles", [])
                        if roles:
                            role_for_comp = roles[0]
                        break

                if role_for_comp:
                    # Find an existing employee with this role
                    candidates = [e for e in employees if e["role_profile_id"] == role_for_comp]
                    if candidates:
                        target = rng.choice(candidates)
                        # Upgrade or add the competency
                        existing = [c for c in all_competencies
                                    if c["employee_id"] == target["employee_id"]
                                    and c["competency_id"] == cid]
                        if existing:
                            existing[0]["level"] = max(existing[0]["level"], req_level)
                        else:
                            all_competencies.append({
                                "employee_id": target["employee_id"],
                                "competency_id": cid,
                                "level": req_level,
                                "confidence": round(rng.uniform(0.7, 1.0), 2),
                                "last_used": date(2026, 10, 1).isoformat(),
                                "source": "project_history",
                            })

    return employees, all_competencies


# ═══════════════════════════════════════════════════════════════════════════════
# Project & assignment generation
# ═══════════════════════════════════════════════════════════════════════════════

def generate_projects(
    rng: random.Random,
    employees: list[dict],
    start_week: str,
    id_gen: IDGen,
    project_names: list[str] | None = None,
) -> list[dict]:
    """Generate background projects that employ most of the workforce."""
    n_projects = max(3, len(employees) // 8)
    projects = []

    default_names = [
        "Platform Alpha", "Data Pipeline Beta", "Mobile Gamma", "Portal Delta",
        "Integration Epsilon", "Analytics Zeta", "Security Eta", "Commerce Theta",
        "Infrastructure Iota", "Compliance Kappa", "ML Service Lambda", "CRM Mu",
    ]
    names = project_names or default_names

    all_weeks = weeks_in_range(start_week, iso_week(2027, 52))

    for i in range(n_projects):
        prj_id = id_gen.next()
        name = names[i % len(names)] + (f" {i // len(names) + 1}" if i >= len(names) else "")
        # Project spans 3-12 months
        duration_weeks = rng.randint(12, 48)
        start_idx = rng.randint(0, max(0, len(all_weeks) - duration_weeks - 20))
        end_idx = min(len(all_weeks) - 1, start_idx + duration_weeks)

        projects.append({
            "project_id": prj_id,
            "code": f"PRJ-{i+1:03d}",
            "name": name[:200],
            "status": rng.choice(["active", "active", "active", "planned"]),
            "priority": rng.randint(1, 5),
            "criticality": rng.choice(["normal", "normal", "high", "critical"]),
            "start_week": all_weeks[start_idx],
            "end_week_planned": all_weeks[end_idx],
            "end_week_forecast": None,
            "replaces_other_projects": False,
        })

    return projects


def generate_assignments(
    rng: random.Random,
    employees: list[dict],
    projects: list[dict],
    start_week: str,
    id_gen: IDGen,
    exclude_employee_ids: set[str] | None = None,
    target_utilization: float = 0.6,
) -> list[dict]:
    """Generate existing assignments (current workload) for employees.

    Bench employees and excluded employees get 0 assignments.
    Others get assignments filling ~40-80% of their capacity.
    """
    exclude = exclude_employee_ids or set()
    assignments = []
    active_projects = [p for p in projects if p["status"] in ("active", "planned")]

    if not active_projects:
        return assignments

    all_weeks = weeks_in_range(start_week, iso_week(2027, 30))  # ~6 months of history

    for emp in employees:
        if emp["bench_flag"] or emp["employee_id"] in exclude:
            continue

        # Each active employee works on 1-2 projects
        n_assigned = rng.randint(1, min(2, len(active_projects)))
        assigned_projects = rng.sample(active_projects, n_assigned)

        # Utilization: 40-80%
        util = rng.uniform(max(0.3, target_utilization - 0.15),
                          min(1.0, target_utilization + 0.2))
        hours_per_week = int(HOURS_PER_WEEK * util / n_assigned)
        hours_per_week = max(80, min(3200, hours_per_week))  # 2h to 80h per week

        for prj in assigned_projects:
            # Assign for weeks where the project is active
            prj_start = prj["start_week"]
            prj_end = prj["end_week_planned"]
            for week in all_weeks:
                if week < prj_start or week > prj_end:
                    continue
                # 85% chance of assignment in each week (some weeks off)
                if rng.random() < 0.85:
                    asg_id = id_gen.next()
                    assignments.append({
                        "assignment_id": asg_id,
                        "project_id": prj["project_id"],
                        "employee_id": emp["employee_id"],
                        "role_id": emp["role_profile_id"],
                        "week": week,
                        "hours_minutes": hours_per_week + rng.randint(-40, 40),
                        "source": "plan",
                    })

    return assignments


# ═══════════════════════════════════════════════════════════════════════════════
# Absence generation
# ═══════════════════════════════════════════════════════════════════════════════

def generate_absences(
    rng: random.Random,
    employees: list[dict],
    year: int,
    id_gen: IDGen,
    exclude_employee_ids: set[str] | None = None,
) -> list[dict]:
    """Generate absence records (vacations, sick leave, training)."""
    exclude = exclude_employee_ids or set()
    absences = []

    for emp in employees:
        if emp["employee_id"] in exclude:
            continue

        # Vacation: 1-2 per year, 7-21 days each (summer preferred)
        n_vacations = rng.choices([1, 2], weights=[0.6, 0.4])[0]
        for _ in range(n_vacations):
            # 60% chance in summer (Jul-Aug), 40% elsewhere
            if rng.random() < 0.6:
                start = date(year, rng.randint(7, 8), rng.randint(1, 28))
            else:
                start = date(year, rng.randint(1, 12), rng.randint(1, 28))
            duration = rng.randint(7, 21)
            end = start + timedelta(days=duration - 1)
            absences.append({
                "absence_id": id_gen.next(),
                "employee_id": emp["employee_id"],
                "type": "vacation",
                "start_date": start.isoformat(),
                "end_date": end.isoformat(),
                "hours_per_day": HOURS_PER_DAY,
                "approved": True,
                "source": rng.choice(["manual", "hr_import", "calendar_import"]),
            })

        # Sick leave: 0-2 per year, 1-10 days
        n_sick = rng.choices([0, 1, 2], weights=[0.4, 0.4, 0.2])[0]
        for _ in range(n_sick):
            start_month = rng.randint(1, 12)
            start = date(year, start_month, rng.randint(1, 28))
            duration = rng.randint(1, 10)
            end = start + timedelta(days=duration - 1)
            absences.append({
                "absence_id": id_gen.next(),
                "employee_id": emp["employee_id"],
                "type": "sick_leave",
                "start_date": start.isoformat(),
                "end_date": end.isoformat(),
                "hours_per_day": HOURS_PER_DAY,
                "approved": True,
                "source": "hr_import",
            })

        # Training: 0-1 per year, 2-5 days
        if rng.random() < 0.3:
            start_month = rng.randint(1, 12)
            start = date(year, start_month, rng.randint(1, 28))
            duration = rng.randint(2, 5)
            end = start + timedelta(days=duration - 1)
            absences.append({
                "absence_id": id_gen.next(),
                "employee_id": emp["employee_id"],
                "type": "training",
                "start_date": start.isoformat(),
                "end_date": end.isoformat(),
                "hours_per_day": rng.choice([4, 8]),
                "approved": True,
                "source": "manual",
            })

    return absences


# ═══════════════════════════════════════════════════════════════════════════════
# Brief generation (references to gold)
# ═══════════════════════════════════════════════════════════════════════════════

def generate_brief_record(
    brief_id: str,
    gold_reqs: list[dict],
    start_week: str,
    end_week: str,
) -> dict:
    """Generate a brief record referencing a gold brief.

    brief_id is the DB-valid remapped ID (e.g. brf_0006).
    Gold directories may use the original ID (e.g. brief_006_ml_recommendation).
    """
    # Read brief text from gold directory - try to find matching directory
    brief_dir_name = None
    # Extract the numeric part to find the directory
    # brf_0006 -> look for directory containing '006' or '0006'
    numeric_part = brief_id.replace("brf_", "").lstrip("0") or "0"

    for d in sorted(GOLD_DIR.iterdir()):
        if not d.is_dir():
            continue
        gold_file = d / "gold.json"
        if gold_file.exists():
            with open(gold_file) as f:
                reqs = json.load(f)
            if reqs:
                orig_id = reqs[0]["brief_id"]
                remapped = remap_brief_id(orig_id)
                if remapped == brief_id:
                    brief_dir_name = d.name
                    break

    if not brief_dir_name:
        raise ValueError(f"Gold brief directory not found for {brief_id}")

    brief_md = GOLD_DIR / brief_dir_name / "brief.md"
    text = brief_md.read_text(encoding="utf-8") if brief_md.exists() else f"Brief {brief_id}"
    text_sha256 = hashlib.sha256(text.encode("utf-8")).hexdigest()

    return {
        "brief_id": brief_id,
        "title": f"Synthetic scenario brief {brief_id}",
        "source_type": "txt",
        "language": "ru",
        "status": "confirmed",
        "text": text[:2000],  # Truncate for brevity; full text in gold/
        "text_sha256": text_sha256,
        "requested_start_week": start_week,
        "deadline_week": end_week,
    }


def generate_requirement_records(
    gold_reqs: list[dict],
    brief_id: str,
) -> list[dict]:
    """Convert gold requirements to DB-format requirement records."""
    records = []
    for r in gold_reqs:
        records.append({
            "requirement_id": r["requirement_id"],
            "brief_id": brief_id,
            "role_id": r["role"]["role_id"],
            "role_confidence": r["role"]["confidence"],
            "competency_id": r["competency"]["competency_id"],
            "level": r["level"],
            "criticality": r["criticality"],
            "headcount": r["headcount"],
            "effort_min": r["effort"]["min_person_days"],
            "effort_likely": r["effort"]["likely_person_days"],
            "effort_max": r["effort"]["max_person_days"],
            "confidence": r["confidence"],
            "needs_review": r.get("needs_review", False),
        })
    return records


# ═══════════════════════════════════════════════════════════════════════════════
# Planted solution construction
# ═══════════════════════════════════════════════════════════════════════════════

def compute_coverage(reqs: list[dict], team_comps: dict[str, dict[str, int]]) -> float:
    """Compute M1 (competency coverage) for a team.

    team_comps: {employee_id: {competency_id: level}}
    """
    must_reqs = [r for r in reqs if r["criticality"] == "must"]
    should_reqs = [r for r in reqs if r["criticality"] == "should"]
    nice_reqs = [r for r in reqs if r["criticality"] == "nice"]

    weights = {"must": 1.0, "should": 0.5, "nice": 0.2}
    total_weight = 0
    weighted_cov = 0

    for req in reqs:
        w = weights[req["criticality"]]
        total_weight += w
        cid = req["competency"]["competency_id"]
        req_level = req["level"]

        # Max coverage across team members
        max_cov = 0
        for emp_comps in team_comps.values():
            emp_level = emp_comps.get(cid, 0)
            cov = min(1.0, emp_level / req_level) if req_level > 0 else 0
            max_cov = max(max_cov, cov)

        weighted_cov += w * max_cov

    return weighted_cov / total_weight if total_weight > 0 else 0


def find_team_for_brief(
    employees: list[dict],
    emp_competencies: list[dict],
    gold_reqs: list[dict],
    rng: random.Random,
    start_week: str,
    end_week: str,
    exclude_ids: set[str] | None = None,
) -> list[dict] | None:
    """Find a feasible team for the given brief requirements.

    Groups requirements by role, then assigns the best employee per role.
    One employee can cover multiple requirements within their role if they
    have the required competency levels.

    Returns list of {employee_id, role_id, grade, assigned_requirements} or None.
    """
    exclude = exclude_ids or set()

    # Build competency lookup: {employee_id: {competency_id: level}}
    emp_comp_map: dict[str, dict[str, int]] = {}
    for c in emp_competencies:
        eid = c["employee_id"]
        if eid not in emp_comp_map:
            emp_comp_map[eid] = {}
        emp_comp_map[eid][c["competency_id"]] = c["level"]

    emp_map = {e["employee_id"]: e for e in employees}

    # Group requirements by role
    role_reqs: dict[str, list[dict]] = {}
    for r in gold_reqs:
        rid = r["role"]["role_id"]
        if rid not in role_reqs:
            role_reqs[rid] = []
        role_reqs[rid].append(r)

    team = []
    used_employees = set()

    # Process roles sorted by number of must requirements (hardest first)
    sorted_roles = sorted(
        role_reqs.keys(),
        key=lambda rid: sum(1 for r in role_reqs[rid] if r["criticality"] == "must"),
        reverse=True,
    )

    for role_id in sorted_roles:
        reqs = role_reqs[role_id]
        must_reqs = sorted(
            [r for r in reqs if r["criticality"] == "must"],
            key=lambda r: r["level"],
            reverse=True,
        )
        other_reqs = [r for r in reqs if r["criticality"] != "must"]

        # Find best employee who covers the most must requirements
        def score_employee(eid: str) -> tuple[int, float]:
            """Return (n_must_covered, cost_score)."""
            comps = emp_comp_map.get(eid, {})
            n_covered = 0
            for req in must_reqs:
                cid = req["competency"]["competency_id"]
                if comps.get(cid, 0) >= req["level"]:
                    n_covered += 1
            emp = emp_map.get(eid, {})
            cost_score = 1.0 / emp.get("cost_coefficient", 1.0)
            return (n_covered, cost_score)

        # Try to find one employee covering ALL must requirements
        best_emp = None
        best_score = (-1, 0.0)

        for eid in emp_comp_map:
            if eid in exclude or eid in used_employees:
                continue
            score = score_employee(eid)
            if score[0] > best_score[0] or (
                score[0] == best_score[0] and score[1] > best_score[1]
            ):
                best_score = score
                best_emp = eid

        if best_emp and best_score[0] > 0:
            # Assign this employee to all requirements they can cover
            assigned = []
            for req in must_reqs + other_reqs:
                cid = req["competency"]["competency_id"]
                if emp_comp_map.get(best_emp, {}).get(cid, 0) >= req["level"]:
                    assigned.append(req["requirement_id"])

            if assigned:
                used_employees.add(best_emp)
                team.append({
                    "employee_id": best_emp,
                    "role_id": role_id,
                    "grade": emp_map[best_emp]["grade"],
                    "assigned_requirements": assigned,
                    "competency_level": max(
                        (emp_comp_map.get(best_emp, {}).get(
                            r["competency"]["competency_id"], 0
                        ) for r in gold_reqs if r["requirement_id"] in assigned),
                        default=0,
                    ),
                })

            # Check if all must reqs for this role are covered
            covered_req_ids = set(assigned)
            uncovered_must = [
                r for r in must_reqs if r["requirement_id"] not in covered_req_ids
            ]
        else:
            uncovered_must = list(must_reqs)

        # For uncovered must requirements, try additional employees
        for req in uncovered_must:
            cid = req["competency"]["competency_id"]
            req_level = req["level"]

            candidates = []
            for eid, comps in emp_comp_map.items():
                if eid in exclude or eid in used_employees:
                    continue
                if comps.get(cid, 0) >= req_level:
                    emp = emp_map[eid]
                    level_fit = 1.0 - abs(comps[cid] - req_level) / 4.0
                    cost_score = 1.0 / emp["cost_coefficient"]
                    score = level_fit * 0.6 + cost_score * 0.4
                    candidates.append((eid, score))

            if candidates:
                candidates.sort(key=lambda x: x[1], reverse=True)
                chosen_id = candidates[0][0]
                used_employees.add(chosen_id)

                # Also cover other uncovered reqs this employee can handle
                assigned = [req["requirement_id"]]
                for other in uncovered_must + other_reqs:
                    if other["requirement_id"] == req["requirement_id"]:
                        continue
                    ocid = other["competency"]["competency_id"]
                    if emp_comp_map.get(chosen_id, {}).get(ocid, 0) >= other["level"]:
                        assigned.append(other["requirement_id"])

                team.append({
                    "employee_id": chosen_id,
                    "role_id": role_id,
                    "grade": emp_map[chosen_id]["grade"],
                    "assigned_requirements": assigned,
                    "competency_level": emp_comp_map[chosen_id].get(cid, 0),
                })

        # For uncovered should/nice requirements, try to assign to existing team or new
        all_covered = {r_id for m in team for r_id in m["assigned_requirements"]}
        remaining = [r for r in reqs if r["requirement_id"] not in all_covered]

        for req in remaining:
            cid = req["competency"]["competency_id"]
            req_level = req["level"]

            # Check if existing team member can cover it
            covered = False
            for member in team:
                if member["role_id"] != role_id:
                    continue
                eid = member["employee_id"]
                if emp_comp_map.get(eid, {}).get(cid, 0) >= req_level:
                    member["assigned_requirements"].append(req["requirement_id"])
                    covered = True
                    break

            if not covered:
                candidates = []
                for eid, comps in emp_comp_map.items():
                    if eid in exclude or eid in used_employees:
                        continue
                    if comps.get(cid, 0) >= req_level:
                        emp = emp_map[eid]
                        score = 1.0 / emp["cost_coefficient"]
                        candidates.append((eid, score))

                if candidates:
                    candidates.sort(key=lambda x: x[1], reverse=True)
                    chosen_id = candidates[0][0]
                    used_employees.add(chosen_id)
                    team.append({
                        "employee_id": chosen_id,
                        "role_id": role_id,
                        "grade": emp_map[chosen_id]["grade"],
                        "assigned_requirements": [req["requirement_id"]],
                        "competency_level": emp_comp_map[chosen_id].get(cid, 0),
                    })

    return team


def build_second_variant(
    employees: list[dict],
    emp_competencies: list[dict],
    gold_reqs: list[dict],
    first_team: list[dict],
    rng: random.Random,
) -> list[dict] | None:
    """Build a second non-dominated variant by excluding first team members.

    This creates a competing variant with different employees.
    """
    first_ids = {m["employee_id"] for m in first_team}
    return find_team_for_brief(employees, emp_competencies, gold_reqs, rng,
                               "2027-W06", "2027-W22", exclude_ids=first_ids)


def compute_metrics_for_team(
    team: list[dict],
    gold_reqs: list[dict],
    emp_comp_map: dict[str, dict[str, int]],
    emp_map: dict[str, dict],
    grade_costs: dict[str, float],
) -> dict:
    """Compute M1-M12 metrics for a planted team."""
    team_comps = {m["employee_id"]: emp_comp_map.get(m["employee_id"], {}) for m in team}

    # M1: Competency Coverage
    m1 = compute_coverage(gold_reqs, team_comps)

    # M2: Effort Coverage (assume full coverage for planted optimal)
    m2 = min(1.0, m1 + 0.05)  # Slightly less than M1 since effort may exceed capacity

    # M3: Level Fit
    total_fit = 0
    count = 0
    overqualified_effort = 0
    total_effort = 0
    for m in team:
        eid = m["employee_id"]
        for req_id in m["assigned_requirements"]:
            req = next((r for r in gold_reqs if r["requirement_id"] == req_id), None)
            if not req:
                continue
            cid = req["competency"]["competency_id"]
            req_level = req["level"]
            emp_level = emp_comp_map.get(eid, {}).get(cid, 0)
            fit = 1.0 - abs(emp_level - req_level) / 4.0
            effort = req["effort"]["likely_person_days"]
            total_fit += fit * effort
            total_effort += effort
            count += 1
            if emp_level - req_level >= 2:
                overqualified_effort += effort

    m3 = total_fit / total_effort if total_effort > 0 else 0
    overqualified_share = overqualified_effort / total_effort if total_effort > 0 else 0

    # M5: Schedule (assume on-track for planted optimal)
    total_pd = sum(r["effort"]["likely_person_days"] for r in gold_reqs)
    team_size = len(team)
    # Rough estimate: total effort / team size / 5 days per week
    estimated_weeks = max(1, math.ceil(total_pd / max(1, team_size) / WORKING_DAYS_PER_WEEK))
    m5 = min(1.0, 12.0 / max(1, estimated_weeks))  # Normalize: 12 weeks = 1.0

    # M6: Cost
    total_cost = sum(
        sum(
            next((r for r in gold_reqs if r["requirement_id"] == req_id), {}).get("effort", {}).get("likely_person_days", 0)
            * HOURS_PER_DAY
            * grade_costs.get(m["grade"], 1.0)
            for req_id in m["assigned_requirements"]
        )
        for m in team
    )
    # Normalize: assume C_min = 0, C_ref = total_cost * 2
    m6 = max(0, min(1, 1 - total_cost / (total_cost * 2 + 1)))

    # M7: Load Health (assume moderate load)
    m7 = 0.85

    # M8: Portfolio Impact (no other projects affected for planted solution)
    m8 = 0.95

    # M9: Bus Factor
    must_reqs = [r for r in gold_reqs if r["criticality"] == "must"]
    must_covered_twice = 0
    for req in must_reqs:
        cid = req["competency"]["competency_id"]
        req_level = req["level"]
        n_qualified = sum(1 for m in team if emp_comp_map.get(m["employee_id"], {}).get(cid, 0) >= req_level)
        if n_qualified >= 2:
            must_covered_twice += 1
    m9 = must_covered_twice / len(must_reqs) if must_reqs else 1.0

    # M10: Focus
    m10 = 0.80

    # M11: Bench utilization
    m11 = 0.50  # Moderate bench usage

    # M12: Resilience
    m12 = 0.70

    return {
        "M1_competency_coverage": round(m1, 4),
        "M2_effort_coverage": round(m2, 4),
        "M3_level_fit": round(m3, 4),
        "M5_schedule": round(m5, 4),
        "M6_cost": round(m6, 4),
        "M7_load_health": round(m7, 4),
        "M8_portfolio_impact": round(m8, 4),
        "M9_bus_factor": round(m9, 4),
        "M10_focus": round(m10, 4),
        "M11_bench_utilization": round(m11, 4),
        "M12_resilience": round(m12, 4),
        "score": round(
            0.15 * m1 + 0.13 * m2 + 0.10 * m3 + 0.15 * m5 +
            0.10 * m6 + 0.08 * m7 + 0.08 * m8 + 0.05 * m9 +
            0.04 * m10 + 0.02 * m11 + 0.02 * m12,
            4,
        ),
        "team_size": len(team),
        "cost_units": round(total_cost, 2),
        "estimated_weeks": estimated_weeks,
        "overqualified_share": round(overqualified_share, 4),
        "must_coverage": round(must_covered_twice / len(must_reqs), 4) if must_reqs else 1.0,
    }


# ═══════════════════════════════════════════════════════════════════════════════
# Scenario generation
# ═══════════════════════════════════════════════════════════════════════════════

def generate_scenario(
    scenario_cfg: dict,
    taxonomy: dict,
    reference: dict,
    gold_briefs: dict[str, list[dict]],
    defaults: dict,
) -> dict:
    """Generate a complete scenario dataset.

    Returns dict with all JSON-serializable data files.
    """
    scenario_id = scenario_cfg["id"]
    pool_size = scenario_cfg["pool_size"]
    scenario_type = scenario_cfg["type"]
    brief_id = scenario_cfg["brief_id"]
    seed = scenario_cfg["seed"]

    rng = random.Random(seed)
    start_week = defaults["start_week"]
    end_week = iso_week(2027, defaults["planning_horizon_weeks"] + 5)  # ~16+5 weeks out

    # Initialize ID generators
    emp_id_gen = IDGen("emp_", rng)
    prj_id_gen = IDGen("prj_", rng)
    asg_id_gen = IDGen("asg_", rng)
    abs_id_gen = IDGen("abs_", rng)

    # Load gold requirements for this brief
    gold_reqs = gold_briefs.get(brief_id)
    if not gold_reqs:
        raise ValueError(f"Gold brief {brief_id} not found")

    must_reqs = get_must_requirements(gold_reqs)
    must_comps = get_required_competencies(must_reqs)

    # ── Generate employees ──
    units = defaults.get("units", ["unt_dev"])
    bench_count = max(2, pool_size // 8)

    if scenario_type == "deficit":
        # Generate employees WITHOUT the deficit competency at required level
        deficit_comp = scenario_cfg["deficit"]["competency_id"]
        deficit_level = scenario_cfg["deficit"]["required_level"]

        employees, emp_comps = generate_employees(
            rng, pool_size, taxonomy, units, emp_id_gen,
            ensure_bench_count=bench_count,
        )

        # Remove any competencies that match the deficit
        emp_comps = [
            c for c in emp_comps
            if not (c["competency_id"] == deficit_comp and c["level"] >= deficit_level)
        ]
        # Also downgrade any that are close
        for c in emp_comps:
            if c["competency_id"] == deficit_comp:
                c["level"] = min(c["level"], deficit_level - 1)

    elif scenario_type == "optimal":
        # Generate employees ensuring full coverage of must requirements
        employees, emp_comps = generate_employees(
            rng, pool_size, taxonomy, units, emp_id_gen,
            ensure_competencies=must_comps,
            ensure_bench_count=bench_count,
        )

    elif scenario_type == "competing":
        # Generate employees with extra coverage (multiple candidates per competency)
        employees, emp_comps = generate_employees(
            rng, pool_size, taxonomy, units, emp_id_gen,
            ensure_competencies=must_comps,
            ensure_bench_count=bench_count,
        )
        # Add extra competencies to more employees to enable competing teams
        all_comp_ids = list(must_comps.keys())
        n_extra = max(3, pool_size // 10)
        for _ in range(n_extra):
            emp = rng.choice(employees)
            cid = rng.choice(all_comp_ids)
            req_level = must_comps[cid]
            # Check if this employee already has it
            existing = [c for c in emp_comps
                        if c["employee_id"] == emp["employee_id"] and c["competency_id"] == cid]
            if not existing:
                level = rng.randint(req_level, min(5, req_level + 1))
                emp_comps.append({
                    "employee_id": emp["employee_id"],
                    "competency_id": cid,
                    "level": level,
                    "confidence": round(rng.uniform(0.7, 1.0), 2),
                    "last_used": date(2026, 10, 1).isoformat(),
                    "source": "project_history",
                })
    else:
        raise ValueError(f"Unknown scenario type: {scenario_type}")

    # ── Build competency lookup ──
    emp_comp_map: dict[str, dict[str, int]] = {}
    for c in emp_comps:
        eid = c["employee_id"]
        if eid not in emp_comp_map:
            emp_comp_map[eid] = {}
        emp_comp_map[eid][c["competency_id"]] = c["level"]

    emp_map = {e["employee_id"]: e for e in employees}

    # ── Generate background projects & assignments ──
    team_employee_ids = set()

    if scenario_type == "optimal":
        # First find the team, then assign others to background projects
        team = find_team_for_brief(employees, emp_comps, gold_reqs, rng, start_week, end_week)
        if team:
            team_employee_ids = {m["employee_id"] for m in team}
    elif scenario_type == "competing":
        team = find_team_for_brief(employees, emp_comps, gold_reqs, rng, start_week, end_week)
        if team:
            team_employee_ids = {m["employee_id"] for m in team}

    projects = generate_projects(rng, employees, start_week, prj_id_gen)
    assignments = generate_assignments(
        rng, employees, projects, start_week, asg_id_gen,
        exclude_employee_ids=team_employee_ids,
        target_utilization=0.6,
    )

    # ── Generate absences ──
    absences = generate_absences(
        rng, employees, 2027, abs_id_gen,
        exclude_employee_ids=team_employee_ids,
    )

    # ── Generate brief and requirement records ──
    brief = generate_brief_record(brief_id, gold_reqs, start_week, end_week)
    requirements = generate_requirement_records(gold_reqs, brief_id)

    # ── Build ground truth ──
    ground_truth = build_ground_truth(
        scenario_id, pool_size, brief_id, seed, scenario_cfg,
        employees, emp_comps, emp_comp_map, emp_map, gold_reqs,
        team if scenario_type in ("optimal", "competing") else None,
        rng,
    )

    return {
        "employees": employees,
        "competencies": emp_comps,
        "projects": projects,
        "assignments": assignments,
        "absences": absences,
        "briefs": [brief],
        "requirements": requirements,
        "ground_truth": ground_truth,
    }


def build_ground_truth(
    scenario_id: str,
    pool_size: int,
    brief_id: str,
    seed: int,
    scenario_cfg: dict,
    employees: list[dict],
    emp_comps: list[dict],
    emp_comp_map: dict[str, dict[str, int]],
    emp_map: dict[str, dict],
    gold_reqs: list[dict],
    team: list[dict] | None,
    rng: random.Random,
) -> dict:
    """Build the ground_truth.json content for a scenario."""
    scenario_type = scenario_cfg["type"]

    gt = {
        "scenario_id": scenario_id,
        "pool_size": pool_size,
        "brief_id": brief_id,
        "seed": seed,
        "generated_at": date.today().isoformat(),
        "scenario_type": scenario_type,
    }

    if scenario_type == "deficit":
        gt["expected_variants"] = [
            {
                "variant_id": "var_expected_01",
                "status": "INFEASIBLE",
                "deficit": {
                    "competency_id": scenario_cfg["deficit"]["competency_id"],
                    "required_level": scenario_cfg["deficit"]["required_level"],
                    "reason": scenario_cfg["deficit"]["reason"],
                    "external_cooperation_flag": True,
                    "recommended_action": "hire",
                },
                "metrics": {
                    "M1_competency_coverage": 0.0,
                    "gate_pass": False,
                    "gate_g3_violations": 1,
                },
            }
        ]

    elif scenario_type == "optimal":
        if not team:
            gt["expected_variants"] = [{
                "variant_id": "var_expected_01",
                "status": "INFEASIBLE",
                "deficit": {"reason": "team construction failed during generation"},
                "metrics": {"gate_pass": False},
            }]
            return gt

        metrics = compute_metrics_for_team(team, gold_reqs, emp_comp_map, emp_map, GRADE_COST)

        members = []
        for m in team:
            emp = emp_map[m["employee_id"]]
            members.append({
                "employee_id": m["employee_id"],
                "role_id": m["role_id"],
                "grade": m["grade"],
                "assignment_share": round(1.0 / max(1, len(m["assigned_requirements"])), 2),
                "competence_fit": round(
                    min(1.0, m.get("competency_level", 1) / max(1, next(
                        (r["level"] for r in gold_reqs if r["requirement_id"] in m["assigned_requirements"] and
                         r["competency"]["competency_id"] in emp_comp_map.get(m["employee_id"], {})),
                        {"level": 1}
                    ))),
                    4,
                ) if m["assigned_requirements"] else 0,
                "reason_codes": ["COVERS_MUST_COMPETENCY", "BEST_LEVEL_FIT"],
                "assigned_requirement_ids": m["assigned_requirements"],
            })

        gt["expected_variants"] = [
            {
                "variant_id": "var_expected_01",
                "status": "OPTIMAL",
                "solver_status": "OPTIMAL",
                "gate_pass": True,
                "gate_g1": 0,
                "gate_g2": 0,
                "gate_g3": 0,
                "gate_g4": 0,
                "gate_g5": 0,
                "members": members,
                "metrics": metrics,
                "deficits": [],
            }
        ]

    elif scenario_type == "competing":
        if not team:
            gt["expected_variants"] = [{
                "variant_id": "var_expected_01",
                "status": "INFEASIBLE",
                "deficit": {"reason": "team construction failed during generation"},
                "metrics": {"gate_pass": False},
            }]
            return gt

        # Build second variant
        team2 = build_second_variant(employees, emp_comps, gold_reqs, team, rng)

        variants = []

        # First variant
        metrics1 = compute_metrics_for_team(team, gold_reqs, emp_comp_map, emp_map, GRADE_COST)
        members1 = []
        for m in team:
            members1.append({
                "employee_id": m["employee_id"],
                "role_id": m["role_id"],
                "grade": m["grade"],
                "reason_codes": ["COVERS_MUST_COMPETENCY", "AVAILABLE_WINDOW"],
                "assigned_requirement_ids": m["assigned_requirements"],
            })
        variants.append({
            "variant_id": "var_expected_01",
            "status": "OPTIMAL",
            "solver_status": "OPTIMAL",
            "gate_pass": True,
            "gate_g1": 0, "gate_g2": 0, "gate_g3": 0, "gate_g4": 0, "gate_g5": 0,
            "members": members1,
            "metrics": metrics1,
            "deficits": [],
            "label": "Balanced",
        })

        # Second variant
        if team2:
            metrics2 = compute_metrics_for_team(team2, gold_reqs, emp_comp_map, emp_map, GRADE_COST)
            members2 = []
            for m in team2:
                members2.append({
                    "employee_id": m["employee_id"],
                    "role_id": m["role_id"],
                    "grade": m["grade"],
                    "reason_codes": ["COVERS_MUST_COMPETENCY", "BENCH_UTILIZATION"],
                    "assigned_requirement_ids": m["assigned_requirements"],
                })

            # Compute Jaccard distance
            ids1 = {m["employee_id"] for m in team}
            ids2 = {m["employee_id"] for m in team2}
            intersection = len(ids1 & ids2)
            union = len(ids1 | ids2)
            jaccard = 1 - (intersection / union) if union > 0 else 1.0

            variants.append({
                "variant_id": "var_expected_02",
                "status": "FEASIBLE",
                "solver_status": "FEASIBLE",
                "gate_pass": True,
                "gate_g1": 0, "gate_g2": 0, "gate_g3": 0, "gate_g4": 0, "gate_g5": 0,
                "members": members2,
                "metrics": metrics2,
                "deficits": [],
                "label": "Alternative",
                "jaccard_distance_vs_variant_01": round(jaccard, 4),
            })

        gt["expected_variants"] = variants

    return gt


# ═══════════════════════════════════════════════════════════════════════════════
# Output
# ═══════════════════════════════════════════════════════════════════════════════

def write_scenario(scenario_data: dict, output_dir: Path) -> None:
    """Write scenario data to JSON files."""
    scenario_dir = output_dir / scenario_data["ground_truth"]["scenario_id"]
    scenario_dir.mkdir(parents=True, exist_ok=True)

    files = {
        "employees.json": scenario_data["employees"],
        "competencies.json": scenario_data["competencies"],
        "projects.json": scenario_data["projects"],
        "assignments.json": scenario_data["assignments"],
        "absences.json": scenario_data["absences"],
        "briefs.json": scenario_data["briefs"],
        "requirements.json": scenario_data["requirements"],
        "ground_truth.json": scenario_data["ground_truth"],
    }

    for filename, data in files.items():
        path = scenario_dir / filename
        with open(path, "w", encoding="utf-8") as f:
            json.dump(data, f, ensure_ascii=False, indent=2)

    print(f"  Written: {scenario_dir}/ ({sum(len(v) if isinstance(v, list) else 1 for v in files.values())} records)")


def print_statistics(scenario_data: dict) -> None:
    """Print scenario statistics."""
    gt = scenario_data["ground_truth"]
    emps = scenario_data["employees"]
    asgs = scenario_data["assignments"]
    abss = scenario_data["absences"]

    grade_dist = {}
    for e in emps:
        grade_dist[e["grade"]] = grade_dist.get(e["grade"], 0) + 1

    role_dist = {}
    for e in emps:
        role_dist[e["role_profile_id"]] = role_dist.get(e["role_profile_id"], 0) + 1

    bench_count = sum(1 for e in emps if e["bench_flag"])

    # Utilization distribution
    emp_minutes: dict[str, int] = {}
    emp_weeks: dict[str, set] = {}
    for a in asgs:
        eid = a["employee_id"]
        emp_minutes[eid] = emp_minutes.get(eid, 0) + a["hours_minutes"]
        if eid not in emp_weeks:
            emp_weeks[eid] = set()
        emp_weeks[eid].add(a["week"])

    utils = []
    for e in emps:
        eid = e["employee_id"]
        total_minutes = emp_minutes.get(eid, 0)
        n_weeks = len(emp_weeks.get(eid, set())) or 1
        avg_weekly_minutes = total_minutes / n_weeks
        # Convert to utilization: hours_per_week / 40h
        avg_weekly_hours = avg_weekly_minutes / 60.0
        utils.append(avg_weekly_hours / HOURS_PER_WEEK)

    avg_util = sum(utils) / len(utils) if utils else 0

    print(f"  Pool: {len(emps)} employees, {len(asgs)} assignments, {len(abss)} absences")
    print(f"  Grades: {dict(sorted(grade_dist.items()))}")
    print(f"  Bench: {bench_count}")
    print(f"  Mean utilization: {avg_util:.1%}")
    print(f"  Ground truth status: {gt['expected_variants'][0]['status'] if gt['expected_variants'] else 'N/A'}")


# ═══════════════════════════════════════════════════════════════════════════════
# Main
# ═══════════════════════════════════════════════════════════════════════════════

def main():
    parser = argparse.ArgumentParser(description="Synthetic data generator for optimizer benchmarking")
    parser.add_argument("--config", default=str(Path(__file__).resolve().parent / "config.yaml"),
                        help="Path to config.yaml")
    parser.add_argument("--seed", type=int, default=42, help="Global seed (overrides config)")
    parser.add_argument("--output", default=None, help="Output directory (default: eval/synth/scenarios)")
    parser.add_argument("--scenario", default=None, help="Generate only this scenario ID")
    parser.add_argument("--verify", action="store_true", help="Run basic verification after generation")
    args = parser.parse_args()

    # Load config
    with open(args.config) as f:
        config = yaml.safe_load(f)

    defaults = config["defaults"]
    if args.seed != 42 or "seed" not in config.get("defaults", {}):
        defaults["seed"] = args.seed

    output_dir = Path(args.output) if args.output else OUTPUT_DIR
    output_dir.mkdir(parents=True, exist_ok=True)

    # Load reference data
    print("Loading taxonomy and reference data...")
    taxonomy = load_taxonomy()
    reference = load_reference()
    gold_briefs = load_gold_briefs()

    print(f"  Roles: {len(taxonomy['roles'])}")
    print(f"  Competencies: {len(taxonomy['competencies'])}")
    print(f"  Gold briefs loaded: {len(gold_briefs)}")

    # Generate scenarios
    print(f"\nGenerating {len(config['scenarios'])} scenarios...")
    all_stats = []

    for scfg in config["scenarios"]:
        if args.scenario and scfg["id"] != args.scenario:
            continue

        print(f"\n{'='*60}")
        print(f"Scenario: {scfg['id']} (pool={scfg['pool_size']}, type={scfg['type']})")
        print(f"  Brief: {scfg['brief_id']} | Seed: {scfg['seed']}")
        print(f"  Description: {scfg['description'].strip()}")

        try:
            data = generate_scenario(scfg, taxonomy, reference, gold_briefs, defaults)
            write_scenario(data, output_dir)
            print_statistics(data)

            all_stats.append({
                "scenario_id": scfg["id"],
                "pool_size": scfg["pool_size"],
                "type": scfg["type"],
                "n_employees": len(data["employees"]),
                "n_competencies": len(data["competencies"]),
                "n_projects": len(data["projects"]),
                "n_assignments": len(data["assignments"]),
                "n_absences": len(data["absences"]),
                "n_requirements": len(data["requirements"]),
                "status": data["ground_truth"]["expected_variants"][0]["status"],
            })
        except Exception as e:
            print(f"  ERROR: {e}")
            import traceback
            traceback.print_exc()
            all_stats.append({
                "scenario_id": scfg["id"],
                "pool_size": scfg["pool_size"],
                "type": scfg["type"],
                "status": f"ERROR: {e}",
            })

    # Print summary
    print(f"\n{'='*60}")
    print("SUMMARY")
    print(f"{'='*60}")
    print(f"{'Scenario':<25} {'Pool':<6} {'Type':<12} {'Emps':<6} {'Status'}")
    print("-" * 70)
    for s in all_stats:
        print(f"{s['scenario_id']:<25} {s['pool_size']:<6} {s['type']:<12} "
              f"{s.get('n_employees', '?'):<6} {s['status']}")

    # Write summary
    summary_path = output_dir / "summary.json"
    with open(summary_path, "w") as f:
        json.dump({
            "generated_at": date.today().isoformat(),
            "seed": args.seed,
            "scenarios": all_stats,
        }, f, indent=2, ensure_ascii=False)
    print(f"\nSummary written to {summary_path}")

    # Verify if requested
    if args.verify:
        print("\nRunning basic verification...")
        verify_scenarios(output_dir, taxonomy)

    return 0 if all("ERROR" not in str(s.get("status", "")) for s in all_stats) else 1


def verify_scenarios(output_dir: Path, taxonomy: dict) -> None:
    """Basic verification of generated data."""
    valid_roles = {r["role_id"] for r in taxonomy["roles"]}
    valid_comps = {c["competency_id"] for c in taxonomy["competencies"]}
    valid_grades = {"J", "M", "S", "L", "P"}
    errors = []

    for scenario_dir in sorted(output_dir.iterdir()):
        if not scenario_dir.is_dir():
            continue

        # Load data
        emp_file = scenario_dir / "employees.json"
        if not emp_file.exists():
            continue

        with open(emp_file) as f:
            employees = json.load(f)
        with open(scenario_dir / "competencies.json") as f:
            competencies = json.load(f)
        with open(scenario_dir / "ground_truth.json") as f:
            gt = json.load(f)

        sid = gt["scenario_id"]

        # Check employee constraints
        emp_ids = set()
        for emp in employees:
            emp_ids.add(emp["employee_id"])
            if emp["grade"] not in valid_grades:
                errors.append(f"{sid}: invalid grade {emp['grade']} for {emp['employee_id']}")
            if emp["role_profile_id"] not in valid_roles:
                errors.append(f"{sid}: invalid role {emp['role_profile_id']}")
            if not (0.5 <= emp["max_utilization_ratio"] <= 1.0):
                errors.append(f"{sid}: max_utilization_ratio out of range for {emp['employee_id']}")
            if not (0 <= emp["reserve_ratio"] <= 0.5):
                errors.append(f"{sid}: reserve_ratio out of range for {emp['employee_id']}")

        # Check competency constraints
        for comp in competencies:
            if comp["employee_id"] not in emp_ids:
                errors.append(f"{sid}: FK violation - competency for unknown employee {comp['employee_id']}")
            if comp["competency_id"] not in valid_comps:
                errors.append(f"{sid}: invalid competency_id {comp['competency_id']}")
            if not (1 <= comp["level"] <= 5):
                errors.append(f"{sid}: level out of range for {comp['employee_id']}/{comp['competency_id']}")
            if not (0 <= comp["confidence"] <= 1):
                errors.append(f"{sid}: confidence out of range")

        # Check ground truth
        for variant in gt.get("expected_variants", []):
            if variant["status"] in ("OPTIMAL", "FEASIBLE"):
                for member in variant.get("members", []):
                    if member["employee_id"] not in emp_ids:
                        errors.append(f"{sid}: team member {member['employee_id']} not in pool")

    if errors:
        print(f"\n  VERIFICATION FAILED: {len(errors)} errors")
        for e in errors[:20]:
            print(f"    - {e}")
        if len(errors) > 20:
            print(f"    ... and {len(errors) - 20} more")
    else:
        print("\n  VERIFICATION PASSED: All constraints satisfied")


if __name__ == "__main__":
    sys.exit(main())
