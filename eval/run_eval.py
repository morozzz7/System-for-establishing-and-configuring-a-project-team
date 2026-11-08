#!/usr/bin/env python3
"""eval/run_eval.py — reproducible per-stage extraction benchmark.

Evaluates the current ML extraction code against `eval/gold` and reports:
  - rules-only stage
  - LLM-only stage (raw parsed LLM response, optional)
  - merger stage (optional)
  - final API stage (postprocessor + evidence validator)

Metrics per stage:
  - competency-level precision / recall / F1
  - pair-level (role, competency) precision / recall / F1
  - pair-level relaxed (role must be in competency.roles) precision / recall / F1

Usage:
    # Baseline without LLM (deterministic, no API key needed)
    python eval/run_eval.py --mode rules --out eval/BASELINE.md

    # Live LLM stage (requires LLM_API_KEY and network)
    python eval/run_eval.py --mode live --out eval/BASELINE.md

    # Use recorded LLM fixtures (one JSON per brief)
    python eval/run_eval.py --mode fixtures --llm-dir eval/runs/20261108_120000
"""

from __future__ import annotations

import argparse
import asyncio
import json
import os
import sys
import time
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# ---------------------------------------------------------------------------
# Paths
# ---------------------------------------------------------------------------

REPO_ROOT = Path(__file__).resolve().parent.parent
ML_DIR = REPO_ROOT / "ml"
GOLD_DIR = REPO_ROOT / "eval" / "gold"
RUNS_DIR = REPO_ROOT / "eval" / "runs"

sys.path.insert(0, str(ML_DIR))

# Disable LLM by default so the baseline is deterministic and does not leak keys.
if not os.environ.get("LLM_API_KEY"):
    os.environ["LLM_API_KEY"] = ""

from app.extract.diagnostics import build_diagnostics  # noqa: E402
from app.extract.effort import EffortEstimator  # noqa: E402
from app.extract.llm_extract import LLMStats, llm_extract  # noqa: E402
from app.extract.merger import Merger  # noqa: E402
from app.extract.postprocessor import RoleCompetencyValidator  # noqa: E402
from app.extract.prompt import PROMPT_VERSION, build_extract_prompt  # noqa: E402
from app.extract.rules import RulesEngine  # noqa: E402
from app.extract.schemas import ExtractedRequirement  # noqa: E402
from app.extract.taxonomy import TaxonomyRegistry, get_registry  # noqa: E402
from app.extract.validator import EvidenceValidator  # noqa: E402
from app.llm.client import LLMClient  # noqa: E402
from app.llm.config import get_settings  # noqa: E402

# ---------------------------------------------------------------------------
# Data loading
# ---------------------------------------------------------------------------


def load_gold_briefs() -> list[dict[str, Any]]:
    """Load all gold briefs."""
    briefs: list[dict[str, Any]] = []
    for d in sorted(GOLD_DIR.iterdir()):
        if not d.is_dir() or not d.name.startswith("brief_"):
            continue
        brief_path = d / "brief.md"
        gold_path = d / "gold.json"
        if not brief_path.exists() or not gold_path.exists():
            continue
        text = brief_path.read_text(encoding="utf-8")
        with open(gold_path, encoding="utf-8") as f:
            gold = json.load(f)
        briefs.append({"id": d.name, "text": text, "gold": gold})
    return briefs


def _req_pair(req: ExtractedRequirement | dict) -> tuple[str, str]:
    """Return (role_id, competency_id). competency_id='' for role-only."""
    if isinstance(req, ExtractedRequirement):
        role_id = req.role.role_id
        comp_id = req.competency.competency_id if req.competency else ""
    else:
        role = req.get("role") or {}
        comp = req.get("competency")
        role_id = role.get("role_id", "") if isinstance(role, dict) else str(role)
        comp_id = (
            comp.get("competency_id", "") if isinstance(comp, dict) else str(comp)
        )
    return role_id, comp_id


def _req_competency(req: ExtractedRequirement | dict) -> str | None:
    """Return competency_id or None for role-only requirements."""
    _, comp_id = _req_pair(req)
    return comp_id or None


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------


def _f1(precision: float, recall: float) -> float:
    if precision + recall <= 0:
        return 0.0
    return 2 * precision * recall / (precision + recall)


def _match_relaxed(
    gold_pairs: list[tuple[str, str]],
    pred_pairs: list[tuple[str, str]],
    registry: TaxonomyRegistry,
) -> tuple[int, int, int]:
    """Greedy relaxed matching: a pred pair matches gold if competency matches
    and the predicted role is in competency.roles (gold role is assumed valid).
    Returns (tp, fp, fn).
    """
    matched_gold: set[int] = set()
    tp = 0
    for pr, pc in pred_pairs:
        comp = registry.get_competency(pc)
        allowed_roles = set(comp.roles) if comp else {pr}
        # find first unmatched gold pair with same competency and allowed role
        for gi, (gr, gc) in enumerate(gold_pairs):
            if gi in matched_gold:
                continue
            if gc != pc:
                continue
            if pr in allowed_roles:
                matched_gold.add(gi)
                tp += 1
                break
    fp = len(pred_pairs) - tp
    fn = len(gold_pairs) - tp
    return tp, fp, fn


def compute_metrics(
    gold_reqs: list[dict],
    pred_reqs: list[ExtractedRequirement],
    registry: TaxonomyRegistry,
) -> dict[str, Any]:
    """Compute per-stage metrics for one brief."""
    # Competency-level sets (ignore role, ignore role-only requirements)
    gold_comps = {_req_competency(g) for g in gold_reqs if _req_competency(g)}
    pred_comps = {_req_competency(p) for p in pred_reqs if _req_competency(p)}

    tp_c = len(gold_comps & pred_comps)
    fp_c = len(pred_comps - gold_comps)
    fn_c = len(gold_comps - pred_comps)
    p_c = tp_c / (tp_c + fp_c) if (tp_c + fp_c) > 0 else 0.0
    r_c = tp_c / (tp_c + fn_c) if (tp_c + fn_c) > 0 else 0.0

    # Exact pair-level sets
    gold_pairs = [_req_pair(g) for g in gold_reqs]
    pred_pairs = [_req_pair(p) for p in pred_reqs]

    gold_pair_set = set(gold_pairs)
    pred_pair_set = set(pred_pairs)
    tp_p = len(gold_pair_set & pred_pair_set)
    fp_p = len(pred_pair_set - gold_pair_set)
    fn_p = len(gold_pair_set - pred_pair_set)
    p_p = tp_p / (tp_p + fp_p) if (tp_p + fp_p) > 0 else 0.0
    r_p = tp_p / (tp_p + fn_p) if (tp_p + fn_p) > 0 else 0.0

    # Relaxed pair-level (role ambiguity allowed)
    tp_r, fp_r, fn_r = _match_relaxed(gold_pairs, pred_pairs, registry)
    p_r = tp_r / (tp_r + fp_r) if (tp_r + fp_r) > 0 else 0.0
    r_r = tp_r / (tp_r + fn_r) if (tp_r + fn_r) > 0 else 0.0

    return {
        "gold_count": len(gold_reqs),
        "predicted_count": len(pred_reqs),
        "competency": {
            "tp": tp_c,
            "fp": fp_c,
            "fn": fn_c,
            "precision": round(p_c, 4),
            "recall": round(r_c, 4),
            "f1": round(_f1(p_c, r_c), 4),
        },
        "pair": {
            "tp": tp_p,
            "fp": fp_p,
            "fn": fn_p,
            "precision": round(p_p, 4),
            "recall": round(r_p, 4),
            "f1": round(_f1(p_p, r_p), 4),
        },
        "pair_relaxed": {
            "tp": tp_r,
            "fp": fp_r,
            "fn": fn_r,
            "precision": round(p_r, 4),
            "recall": round(r_r, 4),
            "f1": round(_f1(p_r, r_r), 4),
        },
    }


def aggregate_metrics(per_brief: list[dict[str, Any]]) -> dict[str, Any]:
    """Aggregate TP/FP/FN across briefs per metric."""
    out: dict[str, Any] = {}
    for key in ("competency", "pair", "pair_relaxed"):
        tp = sum(b[key]["tp"] for b in per_brief)
        fp = sum(b[key]["fp"] for b in per_brief)
        fn = sum(b[key]["fn"] for b in per_brief)
        precision = tp / (tp + fp) if (tp + fp) > 0 else 0.0
        recall = tp / (tp + fn) if (tp + fn) > 0 else 0.0
        out[key] = {
            "tp": tp,
            "fp": fp,
            "fn": fn,
            "precision": round(precision, 4),
            "recall": round(recall, 4),
            "f1": round(_f1(precision, recall), 4),
        }
    return out


# ---------------------------------------------------------------------------
# Pipeline stages
# ---------------------------------------------------------------------------


def _build_extracted_from_partial(partial) -> ExtractedRequirement:
    """Convert a PartialRequirement + EffortRange into a full requirement."""
    from app.extract.effort import EffortEstimator

    estimator = EffortEstimator()
    effort = estimator.estimate(partial, "")
    return ExtractedRequirement(
        requirement_id=partial.requirement_id,
        role=partial.role,
        competency=partial.competency,
        level=partial.level,
        criticality=partial.criticality,
        headcount=partial.headcount,
        effort=effort,
        assumptions=partial.assumptions if partial.assumptions else ["Rules-based extraction"],
        confidence=partial.confidence,
        needs_review=False,
        evidence=partial.evidence,
    )


def run_rules_stage(
    brief_text: str,
    registry: TaxonomyRegistry,
) -> list[ExtractedRequirement]:
    """Run rules engine, estimate effort, and convert to ExtractedRequirement."""
    engine = RulesEngine(registry)
    partials = engine.extract(brief_text, locale="ru")
    estimator = EffortEstimator()
    reqs: list[ExtractedRequirement] = []
    for p in partials:
        effort = estimator.estimate(p, brief_text)
        reqs.append(
            ExtractedRequirement(
                requirement_id=p.requirement_id,
                role=p.role,
                competency=p.competency,
                level=p.level,
                criticality=p.criticality,
                headcount=p.headcount,
                effort=effort,
                assumptions=p.assumptions if p.assumptions else ["Rules-based extraction"],
                confidence=p.confidence,
                needs_review=False,
                evidence=p.evidence,
            )
        )
    return reqs


def _normalize_for_dump(reqs: list[ExtractedRequirement]) -> list[dict]:
    """Serialize requirements to plain dicts."""
    return [r.model_dump(mode="json") for r in reqs]


async def run_llm_stage(
    brief_text: str,
    registry: TaxonomyRegistry,
    settings,
) -> tuple[list[ExtractedRequirement], LLMStats | None, str]:
    """Call the LLM and return raw parsed requirements before merger/postprocessor.

    Returns (reqs, stats, empty_reason). empty_reason is "" when LLM returned data.
    """
    if not settings.is_available:
        return [], None, "LLM_API_KEY not configured"

    client = LLMClient(settings)
    try:
        reqs, stats = await llm_extract(brief_text, "ru", registry, client, settings)
        return reqs, stats, ""
    except Exception as exc:
        return [], None, f"{type(exc).__name__}: {exc}"


def run_postprocess_and_validate(
    reqs: list[ExtractedRequirement],
    brief_text: str,
    registry: TaxonomyRegistry,
) -> tuple[list[ExtractedRequirement], int, int]:
    """Run postprocessor then evidence validator. Returns (valid, dropped_post, dropped_evidence)."""
    post = RoleCompetencyValidator(registry)
    postprocessed = post.validate_and_fix(reqs)
    validator = EvidenceValidator()
    valid, dropped = validator.validate(postprocessed, brief_text)
    return valid, post.dropped_count, dropped


# ---------------------------------------------------------------------------
# Gold-audit helpers
# ---------------------------------------------------------------------------


def audit_gold(registry: TaxonomyRegistry) -> dict[str, Any]:
    """Check gold labels against the taxonomy."""
    comp_roles = {
        cid: set(c.roles) for cid, c in registry.competencies.items()
    }
    core = {
        rid: set(r.core_competencies) for rid, r in registry.roles.items()
    }

    not_in_comp_roles: list[tuple[str, str, str]] = []
    not_in_core: list[tuple[str, str, str]] = []
    evidence_errors: list[tuple[str, int, str]] = []

    for d in sorted(GOLD_DIR.iterdir()):
        if not d.is_dir() or not d.name.startswith("brief_"):
            continue
        text = (d / "brief.md").read_text(encoding="utf-8")
        gold = json.loads((d / "gold.json").read_text(encoding="utf-8"))
        for i, req in enumerate(gold):
            rid = req.get("role", {}).get("role_id", "")
            comp = req.get("competency")
            cid = comp.get("competency_id") if isinstance(comp, dict) else None
            if cid:
                if rid not in comp_roles.get(cid, set()):
                    not_in_comp_roles.append((d.name, rid, cid))
                if rid not in registry.roles or cid not in core.get(rid, set()):
                    not_in_core.append((d.name, rid, cid))
            for j, ev in enumerate(req.get("evidence", [])):
                q = ev.get("quote", "")
                cs = ev.get("char_start")
                ce = ev.get("char_end")
                if q not in text:
                    evidence_errors.append((d.name, i, f"evidence[{j}] quote not in text"))
                elif cs is not None and ce is not None and text[cs:ce] != q:
                    evidence_errors.append((d.name, i, f"evidence[{j}] offset mismatch"))

    return {
        "total_requirements": sum(
            len(json.loads((d / "gold.json").read_text(encoding="utf-8")))
            for d in GOLD_DIR.iterdir()
            if d.is_dir() and d.name.startswith("brief_")
        ),
        "not_in_competency_roles": not_in_comp_roles,
        "not_in_role_core": not_in_core,
        "evidence_errors": evidence_errors,
    }


# ---------------------------------------------------------------------------
# Report rendering
# ---------------------------------------------------------------------------


def _stage_table(stage_results: dict[str, list[dict[str, Any]]]) -> str:
    """Render aggregate stage metrics as markdown."""
    lines = [
        "| Stage | Metric | TP | FP | FN | Precision | Recall | F1 |",
        "|-------|--------|----|----|----|-----------|--------|----|",
    ]
    stage_order = ["rules", "llm", "merger", "final_api"]
    for stage in stage_order:
        if stage not in stage_results:
            continue
        agg = aggregate_metrics(stage_results[stage])
        first = True
        for metric in ("competency", "pair", "pair_relaxed"):
            m = agg[metric]
            label = stage.replace("_", " ").title() if first else ""
            first = False
            lines.append(
                f"| {label} | {metric} | {m['tp']} | {m['fp']} | {m['fn']} | "
                f"{m['precision']:.4f} | {m['recall']:.4f} | {m['f1']:.4f} |"
            )
    return "\n".join(lines)


def _per_brief_table(stage_results: dict[str, list[dict[str, Any]]]) -> str:
    lines = [
        "| Brief | Stage | Gold | Pred | Comp F1 | Pair F1 | Pair* F1 | Notes |",
        "|-------|-------|------|------|---------|---------|----------|-------|",
    ]
    brief_count = len(stage_results["rules"])
    stages = ["rules", "llm", "merger", "final_api"]
    for i in range(brief_count):
        brief_id = stage_results["rules"][i].get("brief_id", f"brief_{i+1:03d}")
        first = True
        for stage in stages:
            if stage not in stage_results or i >= len(stage_results[stage]):
                continue
            m = stage_results[stage][i]
            notes = m.get("notes", "")
            label = brief_id if first else ""
            first = False
            lines.append(
                f"| {label} | {stage} | {m['gold_count']} | {m['predicted_count']} | "
                f"{m['competency']['f1']:.3f} | {m['pair']['f1']:.3f} | "
                f"{m['pair_relaxed']['f1']:.3f} | {notes} |"
            )
    return "\n".join(lines)


def render_baseline_markdown(
    stage_results: dict[str, list[dict[str, Any]]],
    audit: dict[str, Any],
    duration_s: float,
    mode: str,
) -> str:
    """Render the full BASELINE.md report."""
    lines = [
        "# Extraction Baseline Report",
        "",
        f"- Generated: {datetime.now(timezone.utc).isoformat()}",
        f"- Mode: `{mode}`",
        f"- Prompt version: `{PROMPT_VERSION}`",
        f"- Total wall time: {duration_s:.2f}s",
        "",
        "## Aggregate metrics by stage",
        "",
        _stage_table(stage_results),
        "",
        "*Pair* = exact `(role_id, competency_id)`. *Pair\** = relaxed: predicted role must be in `competency.roles`.",
        "",
        "## Per-brief metrics",
        "",
        _per_brief_table(stage_results),
        "",
        "## Gold audit",
        "",
        f"- Total gold requirements: {audit['total_requirements']}",
        "",
        "### Pairs not in `competency.roles`",
        "",
    ]
    if audit["not_in_competency_roles"]:
        lines.append("| Brief | role_id | competency_id |")
        lines.append("|-------|---------|---------------|")
        for brief, rid, cid in audit["not_in_competency_roles"]:
            lines.append(f"| {brief} | {rid} | {cid} |")
    else:
        lines.append("None.")
    lines.append("")

    lines.append("### Pairs not in `role.core_competencies` (informational)")
    lines.append("")
    if audit["not_in_role_core"]:
        lines.append("| Brief | role_id | competency_id |")
        lines.append("|-------|---------|---------------|")
        for brief, rid, cid in audit["not_in_role_core"]:
            lines.append(f"| {brief} | {rid} | {cid} |")
    else:
        lines.append("None.")
    lines.append("")

    lines.append("### Evidence errors in gold")
    lines.append("")
    if audit["evidence_errors"]:
        lines.append("| Brief | Requirement index | Issue |")
        lines.append("|-------|-------------------|-------|")
        for brief, idx, issue in audit["evidence_errors"]:
            lines.append(f"| {brief} | {idx} | {issue} |")
    else:
        lines.append("None.")
    lines.append("")
    return "\n".join(lines)


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


async def main() -> None:
    parser = argparse.ArgumentParser(description="Run extraction benchmark")
    parser.add_argument(
        "--mode",
        choices=["rules", "live", "fixtures"],
        default="rules",
        help="Evaluation mode: rules-only, live LLM, or recorded LLM fixtures",
    )
    parser.add_argument(
        "--llm-dir",
        type=Path,
        default=None,
        help="Directory with recorded LLM responses (fixtures mode)",
    )
    parser.add_argument(
        "--out",
        type=Path,
        default=REPO_ROOT / "eval" / "BASELINE.md",
        help="Output markdown report path",
    )
    parser.add_argument(
        "--dump-raw",
        action="store_true",
        help="Dump per-brief raw stage outputs to eval/runs/<timestamp>/",
    )
    args = parser.parse_args()

    t_start = time.monotonic()
    registry = get_registry()
    briefs = load_gold_briefs()

    settings = get_settings()
    if args.mode == "live" and not settings.is_available:
        print("ERROR: --mode live requires LLM_API_KEY", file=sys.stderr)
        sys.exit(1)
    if args.mode == "fixtures" and (not args.llm_dir or not args.llm_dir.exists()):
        print("ERROR: --mode fixtures requires --llm-dir", file=sys.stderr)
        sys.exit(1)

    stage_results: dict[str, list[dict[str, Any]]] = defaultdict(list)
    raw_dumps: dict[str, dict[str, Any]] = {}

    for brief in briefs:
        bid = brief["id"]
        text = brief["text"]
        gold = brief["gold"]

        # Rules stage
        t0 = time.monotonic()
        rules_reqs = run_rules_stage(text, registry)
        rules_duration_ms = int((time.monotonic() - t0) * 1000)

        # Optional LLM stage
        llm_reqs: list[ExtractedRequirement] = []
        llm_stats: LLMStats | None = None
        llm_empty_reason = "skipped (mode=rules)"
        if args.mode == "live":
            llm_reqs, llm_stats, llm_empty_reason = await run_llm_stage(
                text, registry, settings
            )
        elif args.mode == "fixtures":
            fixture_path = args.llm_dir / f"{bid}_llm.json"
            if fixture_path.exists():
                with open(fixture_path, encoding="utf-8") as f:
                    dump = json.load(f)
                llm_empty_reason = dump.get("empty_reason", "")
                if not llm_empty_reason:
                    llm_reqs = [ExtractedRequirement.model_validate(r) for r in dump.get("requirements", [])]
                    llm_stats = LLMStats(**dump.get("stats", {}))
            else:
                llm_empty_reason = f"fixture not found: {fixture_path}"

        # Validate rules evidence (minor clean-up, should be no-op)
        validator = EvidenceValidator()
        rules_reqs, rules_dropped = validator.validate(rules_reqs, text)

        # Merger stage
        if llm_reqs:
            merger = Merger()
            merged_reqs = merger.merge(
                rules_reqs, llm_reqs, max_requirements=30
            )
        else:
            merged_reqs = list(rules_reqs)

        # Final API stage
        final_reqs, post_dropped, evidence_dropped = run_postprocess_and_validate(
            merged_reqs, text, registry
        )

        # Metrics
        rules_metrics = compute_metrics(gold, rules_reqs, registry)
        rules_metrics.update(
            {
                "brief_id": bid,
                "duration_ms": rules_duration_ms,
                "dropped_no_evidence": rules_dropped,
                "post_processor_dropped": 0,
                "llm_empty_reason": "",
                "llm_tokens_in": None,
                "llm_tokens_out": None,
                "llm_duration_ms": None,
            }
        )
        stage_results["rules"].append(rules_metrics)

        llm_metrics = compute_metrics(gold, llm_reqs, registry)
        llm_metrics.update(
            {
                "brief_id": bid,
                "duration_ms": 0,
                "dropped_no_evidence": 0,
                "post_processor_dropped": 0,
                "llm_empty_reason": llm_empty_reason,
                "llm_tokens_in": llm_stats.tokens_in if llm_stats else None,
                "llm_tokens_out": llm_stats.tokens_out if llm_stats else None,
                "llm_duration_ms": llm_stats.duration_ms if llm_stats else None,
            }
        )
        stage_results["llm"].append(llm_metrics)

        merger_metrics = compute_metrics(gold, merged_reqs, registry)
        merger_metrics.update(
            {
                "brief_id": bid,
                "duration_ms": 0,
                "dropped_no_evidence": 0,
                "post_processor_dropped": 0,
                "llm_empty_reason": llm_empty_reason,
                "llm_tokens_in": llm_stats.tokens_in if llm_stats else None,
                "llm_tokens_out": llm_stats.tokens_out if llm_stats else None,
                "llm_duration_ms": llm_stats.duration_ms if llm_stats else None,
            }
        )
        stage_results["merger"].append(merger_metrics)

        final_metrics = compute_metrics(gold, final_reqs, registry)
        final_metrics.update(
            {
                "brief_id": bid,
                "duration_ms": 0,
                "dropped_no_evidence": evidence_dropped,
                "post_processor_dropped": post_dropped,
                "llm_empty_reason": llm_empty_reason,
                "llm_tokens_in": llm_stats.tokens_in if llm_stats else None,
                "llm_tokens_out": llm_stats.tokens_out if llm_stats else None,
                "llm_duration_ms": llm_stats.duration_ms if llm_stats else None,
            }
        )
        stage_results["final_api"].append(final_metrics)

        # Raw dumps
        raw_dumps[bid] = {
            "brief_id": bid,
            "brief_text": text,
            "gold": gold,
            "rules": _normalize_for_dump(rules_reqs),
            "llm": _normalize_for_dump(llm_reqs),
            "merger": _normalize_for_dump(merged_reqs),
            "final_api": _normalize_for_dump(final_reqs),
            "llm_empty_reason": llm_empty_reason,
            "llm_stats": (
                {
                    "tokens_in": llm_stats.tokens_in,
                    "tokens_out": llm_stats.tokens_out,
                    "duration_ms": llm_stats.duration_ms,
                    "retries": llm_stats.retries,
                    "json_valid": llm_stats.json_valid,
                }
                if llm_stats
                else None
            ),
        }

    duration_s = time.monotonic() - t_start
    audit = audit_gold(registry)

    md = render_baseline_markdown(stage_results, audit, duration_s, args.mode)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(md, encoding="utf-8")
    print(f"Wrote baseline report to {args.out}")

    if args.dump_raw:
        run_dir = RUNS_DIR / datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")
        run_dir.mkdir(parents=True, exist_ok=True)
        for bid, dump in raw_dumps.items():
            (run_dir / f"{bid}_stages.json").write_text(
                json.dumps(dump, ensure_ascii=False, indent=2), encoding="utf-8"
            )
        print(f"Wrote raw stage dumps to {run_dir}")


if __name__ == "__main__":
    asyncio.run(main())
