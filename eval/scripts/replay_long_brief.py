#!/usr/bin/env python3
"""replay_long_brief.py — deterministic offline replay of an archived mega-brief run.

No network, no LLM. Takes the `requirements` list from an archived
`eval/runs/long_brief_latest.json` artifact, re-runs the deterministic
`EvidenceValidator.validate_pipeline` (6 stages + lexical anchor + ADR-006 §3
generic-mention gate) on the brief text, applies the ADR-006 §2 review routing
(`split_for_review`) and reports pair metrics for the full / main / review lists
using `eval/run_eval.py::compute_metrics`.

This is the reproducible substitute for the live `eval/run_long_brief.py` run:
the LLM part is frozen in the artifact, everything after it is recomputed.

Usage:
    python eval/scripts/replay_long_brief.py
    python eval/scripts/replay_long_brief.py \
        --artifact eval/runs/long_brief_latest.json \
        --brief eval/long_brief2/brief.md

Determinism: two consecutive runs produce byte-identical stdout (no timestamps,
all sets sorted).
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parents[2]  # eval/scripts -> eval -> repo
ML_DIR = REPO_ROOT / "ml"
EVAL_DIR = REPO_ROOT / "eval"

DEFAULT_ARTIFACT = EVAL_DIR / "runs" / "long_brief_latest.json"
DEFAULT_BRIEF = EVAL_DIR / "long_brief2" / "brief.md"

# Requirements that must be routed to `requirements_for_review` (ADR-006 §2).
EXPECTED_REVIEW_PAIRS: tuple[tuple[str, str], ...] = (
    ("ux_ui_designer", "ui_figma"),
    ("ux_ui_designer", "ui_wireframing"),
    ("business_analyst", "an_requirements"),
)
# Requirement that must be dropped by the generic-mention gate (ADR-006 §3).
EXPECTED_DROPPED_PAIR: tuple[str, str] = ("devops_engineer", "cloud_docker")
# Scanner false positives that must never be proposed (ADR-006 §4).
FORBIDDEN_SCANNER_IDS: tuple[str, ...] = (
    "dom_telecom",
    "an_user_stories",
    "test_performance",
)

if str(ML_DIR) not in sys.path:
    sys.path.insert(0, str(ML_DIR))
if str(EVAL_DIR) not in sys.path:
    sys.path.insert(0, str(EVAL_DIR))

from app.extract.candidates import scan_unmatched_competencies  # noqa: E402
from app.extract.review import REVIEW_CONFIDENCE_MAX, split_for_review  # noqa: E402
from app.extract.taxonomy import get_registry  # noqa: E402
from app.extract.validator import EvidenceValidator  # noqa: E402

from run_eval import compute_metrics  # noqa: E402


def _pair(req: Any) -> tuple[str, str] | None:
    """Return `(role_id, competency_id)` for a requirement with a competency."""
    comp = req.competency
    if comp is None:
        return None
    return req.role.role_id, comp.competency_id


def replay(
    artifact_path: Path,
    brief_path: Path,
    gold_path: Path,
    max_requirements: int = 40,
) -> dict[str, Any]:
    """Recompute validation, review routing, pair metrics and ADR-006 checks."""
    artifact = json.loads(artifact_path.read_text(encoding="utf-8"))
    brief_text = brief_path.read_text(encoding="utf-8")
    gold = json.loads(gold_path.read_text(encoding="utf-8"))
    registry = get_registry()

    archived = artifact.get("requirements") or []
    valid, vstats = EvidenceValidator().validate_pipeline(
        archived, brief_text, registry, max_requirements=max_requirements
    )
    main, review = split_for_review(valid)

    main_pairs = sorted(p for p in (_pair(r) for r in main) if p)
    review_pairs = sorted(p for p in (_pair(r) for r in review) if p)
    valid_pairs = sorted(p for p in (_pair(r) for r in valid) if p)

    # Scanner candidate set as the live pipeline would compute it (before the
    # deterministic gate): `already_extracted_comp_ids` = primary-pass competencies.
    already = {
        str(r["competency"]["competency_id"])
        for r in archived
        if isinstance(r.get("competency"), dict) and r["competency"].get("competency_id")
    }
    candidates = sorted(
        c.competency_id for c in scan_unmatched_competencies(brief_text, registry, already)
    )

    full_pair = compute_metrics(gold, valid, registry, brief_text)["pair"]
    main_pair = compute_metrics(gold, main, registry, brief_text)["pair"]
    review_pair = compute_metrics(gold, review, registry, brief_text)["pair"]

    dropped_generic = [
        {"requirement_id": req_id, "quote": quote}
        for req_id, quote in vstats.dropped_generic_ids
    ]

    checks: dict[str, bool] = {
        "cloud_docker_dropped": (
            EXPECTED_DROPPED_PAIR not in valid_pairs
            and EXPECTED_DROPPED_PAIR not in main_pairs
            and EXPECTED_DROPPED_PAIR not in review_pairs
            and any("docker" in d["quote"].casefold() for d in dropped_generic)
        ),
        "expected_review_pairs_in_review": all(
            p in review_pairs for p in EXPECTED_REVIEW_PAIRS
        ),
        "expected_review_pairs_not_in_main": all(
            p not in main_pairs for p in EXPECTED_REVIEW_PAIRS
        ),
        "no_forbidden_scanner_candidates": not (
            set(FORBIDDEN_SCANNER_IDS) & set(candidates)
        ),
    }

    return {
        "artifact": str(artifact_path),
        "brief": str(brief_path),
        "gold": str(gold_path),
        "review_confidence_max": REVIEW_CONFIDENCE_MAX,
        "input_count": len(archived),
        "validated_count": len(valid),
        "main_count": len(main),
        "review_count": len(review),
        "dropped_generic_mention": vstats.dropped_generic_mention,
        "dropped_generic_ids": dropped_generic,
        "validation": {
            "dropped_schema_invalid": vstats.dropped_schema_invalid,
            "dropped_unknown_taxonomy": vstats.dropped_unknown_taxonomy,
            "dropped_no_evidence": vstats.dropped_no_evidence,
            "dropped_invalid_pair": vstats.dropped_invalid_pair,
            "flagged_no_lexical_anchor": vstats.flagged_no_lexical_anchor,
            "dropped_generic_mention": vstats.dropped_generic_mention,
            "dropped_duplicate": vstats.dropped_duplicate,
            "dropped_low_confidence": vstats.dropped_low_confidence,
            "dropped_over_limit": vstats.dropped_over_limit,
        },
        "pair_full": full_pair,
        "pair_main": main_pair,
        "pair_review": review_pair,
        "review_items": [
            {
                "role_id": r.role.role_id,
                "competency_id": r.competency.competency_id if r.competency else None,
                "confidence": r.confidence,
            }
            for r in review
        ],
        "review_pairs": [list(p) for p in review_pairs],
        "main_pairs": [list(p) for p in main_pairs],
        "scanner_candidates": candidates,
        "gold_count": len(gold),
        "checks": checks,
        "ok": all(checks.values()),
    }


def _pair_line(label: str, p: dict[str, Any]) -> str:
    return (
        f"  {label:9s} TP={p['tp']:2d} FP={p['fp']:2d} FN={p['fn']:2d} | "
        f"P={p['precision']:.4f} R={p['recall']:.4f} F1={p['f1']:.4f}"
    )


def render(result: dict[str, Any]) -> str:
    """Render a deterministic plain-text report (no timestamps)."""
    lines: list[str] = []
    lines.append("=" * 84)
    lines.append("  Offline replay — eval/long_brief2 (ADR-006, no LLM, deterministic)")
    lines.append("=" * 84)
    lines.append(f"  artifact:              {result['artifact']}")
    lines.append(f"  brief:                 {result['brief']}")
    lines.append(f"  gold:                  {result['gold']} ({result['gold_count']} requirements)")
    lines.append(f"  review threshold:      confidence <= {result['review_confidence_max']:.2f} + needs_review")
    lines.append("")
    lines.append(
        f"  archived requirements: {result['input_count']} -> validated {result['validated_count']} "
        f"-> main {result['main_count']} + review {result['review_count']}"
    )
    lines.append(f"  dropped_generic_mention: {result['dropped_generic_mention']}")
    for d in result["dropped_generic_ids"]:
        lines.append(f"    - {d['requirement_id']}: {d['quote']!r}")
    v = result["validation"]
    lines.append(
        "  validation drops: schema={dropped_schema_invalid} taxonomy={dropped_unknown_taxonomy} "
        "evidence={dropped_no_evidence} pair={dropped_invalid_pair} dup={dropped_duplicate} "
        "low_conf={dropped_low_confidence} over_limit={dropped_over_limit} "
        "lexical_flagged={flagged_no_lexical_anchor}".format(**v)
    )
    lines.append("")
    lines.append("  requirements_for_review (role_id, competency_id, confidence):")
    if result["review_items"]:
        for item in result["review_items"]:
            lines.append(
                f"    - {item['role_id']}/{item['competency_id']} conf={item['confidence']:.2f}"
            )
    else:
        lines.append("    (empty)")
    lines.append("")
    lines.append("  Pair metrics (against the full gold set):")
    lines.append(_pair_line("full", result["pair_full"]))
    lines.append(_pair_line("main", result["pair_main"]))
    lines.append(_pair_line("review", result["pair_review"]))
    lines.append("")
    lines.append("  Scanner candidates (pre-verification, sorted):")
    lines.append(
        "    " + (", ".join(result["scanner_candidates"]) if result["scanner_candidates"] else "(none)")
    )
    lines.append("")
    lines.append("  ADR-006 checks:")
    for name, passed in result["checks"].items():
        lines.append(f"    [{'PASS' if passed else 'FAIL'}] {name}")
    lines.append("")
    lines.append(f"  RESULT: {'PASS' if result['ok'] else 'FAIL'}")
    lines.append("=" * 84)
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        description="Deterministic offline replay of an archived mega-brief extraction run"
    )
    parser.add_argument(
        "--artifact",
        type=Path,
        default=DEFAULT_ARTIFACT,
        help=f"Archived run JSON (default: {DEFAULT_ARTIFACT})",
    )
    parser.add_argument(
        "--brief",
        type=Path,
        default=DEFAULT_BRIEF,
        help=f"Brief text used for evidence re-validation (default: {DEFAULT_BRIEF})",
    )
    parser.add_argument(
        "--gold",
        type=Path,
        default=None,
        help="Gold JSON (default: <brief_dir>/gold.json)",
    )
    parser.add_argument(
        "--max-requirements",
        type=int,
        default=40,
        help="max_requirements passed to validate_pipeline (default: 40, as in run_long_brief)",
    )
    args = parser.parse_args(argv)

    for path, label in ((args.artifact, "artifact"), (args.brief, "brief")):
        if not path.exists():
            print(f"ERROR: {label} not found: {path}", file=sys.stderr)
            return 2

    gold_path = args.gold if args.gold is not None else (args.brief.parent / "gold.json")
    if not gold_path.exists():
        print(f"ERROR: gold not found: {gold_path}", file=sys.stderr)
        return 2

    result = replay(args.artifact, args.brief, gold_path, args.max_requirements)
    print(render(result))
    return 0 if result["ok"] else 1


if __name__ == "__main__":
    sys.exit(main())
