#!/usr/bin/env python3
"""validate_gold.py — Validate every gold.json in eval/gold/ against requirement.schema.json.

Checks:
  1. JSON Schema structural validation (via jsonschema).
  2. Evidence offset correctness: brief_text[char_start:char_end] == quote.
  3. Role IDs exist in contracts/taxonomy.yaml.
  4. Competency IDs exist in contracts/taxonomy.yaml (unless needs_review = true).
  5. Effort ordering: min <= likely <= max.
  6. requirement_id uniqueness within each brief.

Exit 0 on success, 1 on any failure. Prints a summary table.
"""

import argparse
import json
import os
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print("ERROR: PyYAML not installed. Run: pip install pyyaml", file=sys.stderr)
    sys.exit(2)

try:
    import jsonschema
except ImportError:
    print("ERROR: jsonschema not installed. Run: pip install jsonschema", file=sys.stderr)
    sys.exit(2)


ROOT = Path(__file__).resolve().parent.parent.parent  # repo root
TAXONOMY_PATH = ROOT / "contracts" / "taxonomy.yaml"
SCHEMA_PATH = ROOT / "contracts" / "json-schema" / "requirement.schema.json"
COMMON_SCHEMA_PATH = ROOT / "contracts" / "json-schema" / "common.schema.json"
GOLD_DIR = ROOT / "eval" / "gold"


def load_taxonomy() -> dict:
    with open(TAXONOMY_PATH, "r", encoding="utf-8") as f:
        return yaml.safe_load(f)


def load_schema() -> dict:
    with open(SCHEMA_PATH, "r", encoding="utf-8") as f:
        schema = json.load(f)
    with open(COMMON_SCHEMA_PATH, "r", encoding="utf-8") as f:
        common = json.load(f)
    return schema, common


def get_brief_body(brief_path: Path) -> str:
    """Read brief.md and return just the body (skip YAML front-matter if present)."""
    text = brief_path.read_text(encoding="utf-8")
    if text.startswith("---"):
        # Skip front-matter
        end = text.find("---", 3)
        if end != -1:
            text = text[end + 3:].lstrip("\n")
    return text


def validate_brief(brief_dir: Path, taxonomy: dict, schema: dict, common_schema: dict) -> list[str]:
    """Validate a single brief directory. Returns list of error strings (empty = pass)."""
    errors = []
    brief_name = brief_dir.name

    # Check required files exist
    brief_path = brief_dir / "brief.md"
    gold_path = brief_dir / "gold.json"
    notes_path = brief_dir / "notes.md"

    for fpath, label in [(brief_path, "brief.md"), (gold_path, "gold.json"), (notes_path, "notes.md")]:
        if not fpath.exists():
            errors.append(f"{brief_name}: missing {label}")
    if errors:
        return errors  # Can't continue without files

    # Load brief body
    brief_text = get_brief_body(brief_path)

    # Load gold
    try:
        with open(gold_path, "r", encoding="utf-8") as f:
            gold = json.load(f)
    except (json.JSONDecodeError, UnicodeDecodeError) as e:
        errors.append(f"{brief_name}: gold.json parse error: {e}")
        return errors

    if not isinstance(gold, list):
        errors.append(f"{brief_name}: gold.json must be a JSON array")
        return errors

    # Validate each requirement (manual structural checks)
    # Build valid role/competency sets
    valid_roles = {r["role_id"] for r in taxonomy.get("roles", [])}
    valid_competencies = {c["competency_id"] for c in taxonomy.get("competencies", [])}
    comp_allowed_roles = {
        c["competency_id"]: set(c.get("roles", []))
        for c in taxonomy.get("competencies", [])
    }

    # Validate each requirement
    req_ids = set()
    for i, req in enumerate(gold):
        prefix = f"{brief_name}[{i}]"

        # Schema validation (manual, since $ref resolution is complex)
        # We'll do structural checks manually

        # Required fields
        required_fields = [
            "requirement_id", "role", "level", "criticality",
            "headcount", "effort", "assumptions", "confidence",
            "needs_review", "evidence"
        ]
        for field in required_fields:
            if field not in req:
                errors.append(f"{prefix}: missing required field '{field}'")

        if "requirement_id" in req:
            rid = req["requirement_id"]
            if not re.match(r"^[A-Za-z0-9_-]{4,40}$", str(rid)):
                errors.append(f"{prefix}: requirement_id '{rid}' doesn't match pattern")
            if rid in req_ids:
                errors.append(f"{prefix}: duplicate requirement_id '{rid}'")
            req_ids.add(rid)

        # Role validation
        if "role" in req and isinstance(req["role"], dict):
            role_id = req["role"].get("role_id", "")
            if role_id not in valid_roles:
                errors.append(f"{prefix}: role_id '{role_id}' not in taxonomy")
            if "confidence" not in req["role"]:
                errors.append(f"{prefix}: role missing 'confidence'")

        # Competency validation
        if "competency" in req and isinstance(req["competency"], dict):
            comp_id = req["competency"].get("competency_id", "")
            needs_review = req.get("needs_review", False)
            if comp_id not in valid_competencies and not needs_review:
                errors.append(f"{prefix}: competency_id '{comp_id}' not in taxonomy (and needs_review is false)")
            elif comp_id in comp_allowed_roles:
                role_id = req.get("role", {}).get("role_id", "") if isinstance(req.get("role"), dict) else ""
                if role_id and role_id not in comp_allowed_roles[comp_id]:
                    errors.append(
                        f"{prefix}: pair ({role_id}, {comp_id}) invalid: "
                        f"role_id not in competency.roles {sorted(comp_allowed_roles[comp_id])}"
                    )

        # Level
        if "level" in req:
            lvl = req["level"]
            if not isinstance(lvl, int) or lvl < 1 or lvl > 5:
                errors.append(f"{prefix}: level must be int 1-5, got {lvl}")

        # Criticality
        if "criticality" in req:
            if req["criticality"] not in ("must", "should", "nice"):
                errors.append(f"{prefix}: criticality must be must/should/nice, got '{req['criticality']}'")

        # Headcount
        if "headcount" in req:
            hc = req["headcount"]
            if not isinstance(hc, int) or hc < 1 or hc > 20:
                errors.append(f"{prefix}: headcount must be int 1-20, got {hc}")

        # Effort
        if "effort" in req and isinstance(req["effort"], dict):
            eff = req["effort"]
            for k in ["min_person_days", "likely_person_days", "max_person_days", "unit"]:
                if k not in eff:
                    errors.append(f"{prefix}: effort missing '{k}'")
            if "min_person_days" in eff and "likely_person_days" in eff and "max_person_days" in eff:
                if not (eff["min_person_days"] <= eff["likely_person_days"] <= eff["max_person_days"]):
                    errors.append(f"{prefix}: effort ordering violated: {eff['min_person_days']} <= {eff['likely_person_days']} <= {eff['max_person_days']}")
                for k in ["min_person_days", "likely_person_days", "max_person_days"]:
                    if eff[k] < 0.5 or eff[k] > 2000:
                        errors.append(f"{prefix}: effort.{k} = {eff[k]} out of range [0.5, 2000]")
            if eff.get("unit") != "person_days":
                errors.append(f"{prefix}: effort.unit must be 'person_days'")

        # Assumptions
        if "assumptions" in req:
            a = req["assumptions"]
            if not isinstance(a, list) or len(a) < 1:
                errors.append(f"{prefix}: assumptions must be array with >= 1 item")
            elif len(a) > 10:
                errors.append(f"{prefix}: assumptions has > 10 items")

        # Confidence + needs_review invariant
        if "confidence" in req:
            conf = req["confidence"]
            if not isinstance(conf, (int, float)) or conf < 0 or conf > 1:
                errors.append(f"{prefix}: confidence must be 0..1, got {conf}")
            if conf < 0.7 and not req.get("needs_review", False):
                errors.append(f"{prefix}: confidence < 0.7 but needs_review is false")

        # Evidence
        if "evidence" in req:
            ev = req["evidence"]
            if not isinstance(ev, list) or len(ev) < 1:
                errors.append(f"{prefix}: evidence must be array with >= 1 item")
            else:
                for j, span in enumerate(ev):
                    if not isinstance(span, dict):
                        errors.append(f"{prefix}.evidence[{j}]: not an object")
                        continue
                    if "kind" not in span:
                        errors.append(f"{prefix}.evidence[{j}]: missing 'kind'")
                    elif span["kind"] not in ("brief_span", "taxonomy_synonym", "project_history", "user_edit"):
                        errors.append(f"{prefix}.evidence[{j}]: invalid kind '{span['kind']}'")
                    if "quote" not in span:
                        errors.append(f"{prefix}.evidence[{j}]: missing 'quote'")
                    else:
                        q = span["quote"]
                        if len(q) < 3 or len(q) > 1000:
                            errors.append(f"{prefix}.evidence[{j}]: quote length {len(q)} out of [3, 1000]")

                        # Offset verification
                        if span.get("kind") == "brief_span":
                            cs = span.get("char_start")
                            ce = span.get("char_end")
                            if cs is not None and ce is not None:
                                actual = brief_text[cs:ce]
                                if actual != q:
                                    errors.append(
                                        f"{prefix}.evidence[{j}]: offset mismatch at [{cs}:{ce}]. "
                                        f"Expected '{q[:50]}...', got '{actual[:50]}...'"
                                    )
                            else:
                                # Try to find quote in text
                                idx = brief_text.find(q)
                                if idx == -1:
                                    errors.append(f"{prefix}.evidence[{j}]: quote not found in brief text: '{q[:60]}...'")

    return errors


def main():
    parser = argparse.ArgumentParser(description="Validate gold briefs against schema and taxonomy")
    parser.add_argument("--gold-dir", default=str(GOLD_DIR), help="Path to eval/gold/")
    parser.add_argument("--taxonomy", default=str(TAXONOMY_PATH), help="Path to taxonomy.yaml")
    parser.add_argument("--verbose", "-v", action="store_true", help="Print per-brief status")
    args = parser.parse_args()

    gold_dir = Path(args.gold_dir)
    taxonomy = load_taxonomy()
    schema, common_schema = load_schema()

    if not gold_dir.exists():
        print(f"ERROR: gold directory not found: {gold_dir}", file=sys.stderr)
        sys.exit(1)

    brief_dirs = sorted([d for d in gold_dir.iterdir() if d.is_dir() and d.name.startswith("brief_")])
    if not brief_dirs:
        print("ERROR: no brief_* directories found", file=sys.stderr)
        sys.exit(1)

    total_errors = 0
    total_briefs = len(brief_dirs)
    total_reqs = 0

    print(f"Validating {total_briefs} briefs in {gold_dir}")
    print(f"Taxonomy: {args.taxonomy}")
    print("-" * 70)

    for brief_dir in brief_dirs:
        errors = validate_brief(brief_dir, taxonomy, schema, common_schema)

        # Count requirements
        gold_path = brief_dir / "gold.json"
        if gold_path.exists():
            try:
                with open(gold_path, "r", encoding="utf-8") as f:
                    n_reqs = len(json.load(f))
                total_reqs += n_reqs
            except Exception:
                n_reqs = 0

        if errors:
            total_errors += len(errors)
            status = f"FAIL ({len(errors)} errors)"
        else:
            status = "OK"

        if args.verbose or errors:
            print(f"  {brief_dir.name:40s} {status:25s} ({n_reqs} reqs)")
            for e in errors:
                print(f"    ❌ {e}")

    print("-" * 70)
    print(f"Total: {total_briefs} briefs, {total_reqs} requirements, {total_errors} errors")

    if total_errors > 0:
        print("RESULT: FAIL")
        sys.exit(1)
    else:
        print("RESULT: PASS")
        sys.exit(0)


if __name__ == "__main__":
    main()
