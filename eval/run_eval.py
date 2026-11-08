#!/usr/bin/env python3
"""eval/run_eval.py — reproducible per-stage extraction benchmark.

Evaluates the ML extraction pipeline against `eval/gold` and reports:
  1. `rules`: current RulesEngine (reference comparison point only)
  2. `llm_raw`: parsed LLM response before deterministic validation/post-processing
  3. `llm_validated`: LLM after deterministic validation (3.6)
  4. `final_api`: final `/v1/extract` output

Metrics per brief and in aggregate (All 17 briefs + Hold-out 5 briefs):
  - `competency`: set of `competency_id` per brief (TP/FP/FN, Precision, Recall, F1)
  - `pair`: set of exact `(role_id, competency_id)` per brief
  - `pair_relaxed`: `(role_id, competency_id)` where role counts if `role_id in competency.roles`
  - `role_accuracy`: accuracy of `role_id` on correctly found competencies (`correct_roles / matched_comps`)

Usage:
    # Auto-detect (runs live if LLM_API_KEY is in env / ml/.env / .env, else rules)
    python eval/run_eval.py

    # Explicit live LLM run (defaults to --runs 3; use --runs 1 for a quick single pass)
    python eval/run_eval.py --mode live --runs 1

    # Baseline without LLM (deterministic, no API key needed)
    python eval/run_eval.py --mode rules --out eval/BASELINE.md --dump-raw

    # Use recorded LLM fixtures
    python eval/run_eval.py --mode fixtures --llm-dir ml/tests/fixtures/llm --out eval/BASELINE.md
"""

from __future__ import annotations

import argparse
import asyncio
import json
import math
import sys
import time
from collections import defaultdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

# ---------------------------------------------------------------------------
# Paths & Hold-out split
# ---------------------------------------------------------------------------

REPO_ROOT = Path(__file__).resolve().parent.parent
ML_DIR = REPO_ROOT / "ml"
GOLD_DIR = REPO_ROOT / "eval" / "gold"
RUNS_DIR = REPO_ROOT / "eval" / "runs"

HOLD_OUT_BRIEFS: list[str] = [
    "brief_005_legacy_modernization",
    "brief_007_bi_dashboards",
    "brief_010_devops_platform",
    "brief_015_telecom_migration",
    "brief_016_ecommerce",
]

STAGE_ORDER: list[str] = ["rules", "llm_raw", "llm_validated", "final_api"]
METRIC_KEYS: tuple[str, ...] = ("competency", "pair", "pair_relaxed", "role_accuracy")

sys.path.insert(0, str(ML_DIR))

from app.extract.effort import EffortEstimator  # noqa: E402
from app.extract.llm_extract import LLMStats, llm_extract  # noqa: E402
from app.extract.postprocessor import RoleCompetencyValidator  # noqa: E402
from app.extract.prompt import PROMPT_VERSION  # noqa: E402
from app.extract.rules import RulesEngine  # noqa: E402
from app.extract.schemas import ExtractedRequirement  # noqa: E402
from app.extract.taxonomy import TaxonomyRegistry, get_registry  # noqa: E402
from app.extract.validator import EvidenceValidator  # noqa: E402
from app.llm.client import LLMClient, LLMResponse  # noqa: E402
from app.llm.config import LLMSettings, clear_settings_cache, get_settings  # noqa: E402


def _mask_key(key: str) -> str:
    s = key.strip()
    if not s:
        return "not set"
    if len(s) <= 12:
        return "***"
    return f"{s[:8]}...{s[-4:]}"


# ---------------------------------------------------------------------------
# Data loading
# ---------------------------------------------------------------------------


def load_gold_briefs() -> list[dict[str, Any]]:
    """Load all gold briefs sorted by directory name."""
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


def _req_pair(req: ExtractedRequirement | dict[str, Any]) -> tuple[str, str]:
    """Return (role_id, competency_id). competency_id='' for role-only."""
    if isinstance(req, ExtractedRequirement):
        role_id = req.role.role_id
        comp_id = req.competency.competency_id if req.competency else ""
    else:
        if "role_id" in req or "competency_id" in req:
            role_id = str(req.get("role_id") or "")
            comp_id = str(req.get("competency_id") or "")
        else:
            role = req.get("role") or {}
            comp = req.get("competency")
            role_id = role.get("role_id", "") if isinstance(role, dict) else str(role)
            comp_id = (
                comp.get("competency_id", "") if isinstance(comp, dict) else str(comp or "")
            )
    return role_id, comp_id


def _req_competency(req: ExtractedRequirement | dict[str, Any]) -> str | None:
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
        allowed_roles = set(comp.roles) if comp else set()
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
    gold_reqs: list[dict[str, Any]],
    pred_reqs: list[ExtractedRequirement | dict[str, Any]],
    registry: TaxonomyRegistry,
) -> dict[str, Any]:
    """Compute per-stage metrics for one brief."""
    # 1. Competency-level sets (ignore role, ignore role-only requirements)
    gold_comps = {c for g in gold_reqs if (c := _req_competency(g))}
    pred_comps = {c for p in pred_reqs if (c := _req_competency(p))}

    tp_c = len(gold_comps & pred_comps)
    fp_c = len(pred_comps - gold_comps)
    fn_c = len(gold_comps - pred_comps)
    p_c = tp_c / (tp_c + fp_c) if (tp_c + fp_c) > 0 else 0.0
    r_c = tp_c / (tp_c + fn_c) if (tp_c + fn_c) > 0 else 0.0

    # 2. Exact pair-level sets
    gold_pairs = [_req_pair(g) for g in gold_reqs]
    pred_pairs = [_req_pair(p) for p in pred_reqs]

    gold_pair_set = set(gold_pairs)
    pred_pair_set = set(pred_pairs)
    tp_p = len(gold_pair_set & pred_pair_set)
    fp_p = len(pred_pair_set - gold_pair_set)
    fn_p = len(gold_pair_set - pred_pair_set)
    p_p = tp_p / (tp_p + fp_p) if (tp_p + fp_p) > 0 else 0.0
    r_p = tp_p / (tp_p + fn_p) if (tp_p + fn_p) > 0 else 0.0

    # 3. Relaxed pair-level (role_id in competency.roles)
    dedup_gold_pairs = list(dict.fromkeys(gold_pairs))
    dedup_pred_pairs = list(dict.fromkeys(pred_pairs))
    tp_r, fp_r, fn_r = _match_relaxed(dedup_gold_pairs, dedup_pred_pairs, registry)
    p_r = tp_r / (tp_r + fp_r) if (tp_r + fp_r) > 0 else 0.0
    r_r = tp_r / (tp_r + fn_r) if (tp_r + fn_r) > 0 else 0.0

    # 4. Conditional role accuracy on matched competencies (gold_comps & pred_comps)
    matched_comps = gold_comps & pred_comps
    correct_roles = 0
    for cid in matched_comps:
        gold_roles_for_c = {gr for gr, gc in gold_pairs if gc == cid}
        pred_roles_for_c = {pr for pr, pc in pred_pairs if pc == cid}
        if gold_roles_for_c & pred_roles_for_c:
            correct_roles += 1
    wrong_roles = len(matched_comps) - correct_roles
    role_acc = correct_roles / len(matched_comps) if matched_comps else 0.0

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
        "role_accuracy": {
            "tp": correct_roles,
            "fp": wrong_roles,
            "fn": wrong_roles,
            "matched_comps": len(matched_comps),
            "correct_roles": correct_roles,
            "accuracy": round(role_acc, 4),
            "precision": round(role_acc, 4),
            "recall": round(role_acc, 4),
            "f1": round(role_acc, 4),
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

    correct = sum(b["role_accuracy"]["tp"] for b in per_brief)
    wrong = sum(b["role_accuracy"]["fp"] for b in per_brief)
    matched = correct + wrong
    acc = correct / matched if matched > 0 else 0.0
    out["role_accuracy"] = {
        "tp": correct,
        "fp": wrong,
        "fn": wrong,
        "matched_comps": matched,
        "correct_roles": correct,
        "accuracy": round(acc, 4),
        "precision": round(acc, 4),
        "recall": round(acc, 4),
        "f1": round(acc, 4),
    }
    return out


def aggregate_multi_run_metrics(
    runs_per_brief: list[list[dict[str, Any]]],
) -> dict[str, Any]:
    """Aggregate across multiple runs, computing mean and spread (min/max/std) for F1."""
    if not runs_per_brief:
        return aggregate_metrics([])
    per_run_aggs = [aggregate_metrics(run_briefs) for run_briefs in runs_per_brief]
    if len(per_run_aggs) == 1:
        return per_run_aggs[0]

    out: dict[str, Any] = {}
    n = len(per_run_aggs)
    for key in METRIC_KEYS:
        tp_mean = round(sum(a[key]["tp"] for a in per_run_aggs) / n, 1)
        fp_mean = round(sum(a[key]["fp"] for a in per_run_aggs) / n, 1)
        fn_mean = round(sum(a[key]["fn"] for a in per_run_aggs) / n, 1)
        p_vals = [a[key]["precision"] for a in per_run_aggs]
        r_vals = [a[key]["recall"] for a in per_run_aggs]
        f1_vals = [a[key]["f1"] for a in per_run_aggs]
        f1_mean = sum(f1_vals) / n
        f1_std = math.sqrt(sum((x - f1_mean) ** 2 for x in f1_vals) / n)
        out[key] = {
            "tp": tp_mean,
            "fp": fp_mean,
            "fn": fn_mean,
            "precision": round(sum(p_vals) / n, 4),
            "recall": round(sum(r_vals) / n, 4),
            "f1": round(f1_mean, 4),
            "f1_min": round(min(f1_vals), 4),
            "f1_max": round(max(f1_vals), 4),
            "f1_std": round(f1_std, 4),
        }
    return out


# ---------------------------------------------------------------------------
# Pipeline stages
# ---------------------------------------------------------------------------


def run_rules_stage(
    brief_text: str,
    registry: TaxonomyRegistry,
) -> tuple[list[ExtractedRequirement], int]:
    """Run rules engine, estimate effort, and validate evidence."""
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
    validator = EvidenceValidator()
    valid_reqs, dropped = validator.validate(reqs, brief_text)
    return valid_reqs, dropped


def _normalize_for_dump(reqs: list[ExtractedRequirement | dict[str, Any]]) -> list[dict[str, Any]]:
    """Serialize requirements to plain dicts."""
    out: list[dict[str, Any]] = []
    for r in reqs:
        if isinstance(r, ExtractedRequirement):
            out.append(r.model_dump(mode="json"))
        elif isinstance(r, dict):
            out.append(r)
    return out


def run_deterministic_validation(
    reqs: list[ExtractedRequirement],
    brief_text: str,
    registry: TaxonomyRegistry,
) -> tuple[list[ExtractedRequirement], dict[str, int]]:
    """Run deterministic validation (postprocessor + evidence validator)."""
    validator = EvidenceValidator()
    if hasattr(validator, "validate_pipeline"):
        valid, vstats = validator.validate_pipeline(reqs, brief_text, registry)
        return valid, {
            "dropped_schema_invalid": vstats.dropped_schema_invalid,
            "dropped_unknown_taxonomy": vstats.dropped_unknown_taxonomy,
            "dropped_no_evidence": vstats.dropped_no_evidence,
            "post_processor_dropped": vstats.dropped_invalid_pair,
            "dropped_duplicate": vstats.dropped_duplicate,
            "dropped_over_limit": vstats.dropped_over_limit,
        }

    post = RoleCompetencyValidator(registry)
    postprocessed = post.validate_and_fix(reqs)
    valid, dropped_ev = validator.validate(postprocessed, brief_text)
    return valid, {
        "dropped_schema_invalid": 0,
        "dropped_unknown_taxonomy": 0,
        "dropped_no_evidence": dropped_ev,
        "post_processor_dropped": post.dropped_count,
        "dropped_duplicate": 0,
        "dropped_over_limit": 0,
    }


class _FixtureLLMClient:
    """Mock LLMClient that replays a recorded fixture response."""

    def __init__(self, fixture_data: dict[str, Any]) -> None:
        self._data = fixture_data

    async def chat(self, messages: Any, **kwargs: Any) -> LLMResponse:
        content = self._data.get("content", "[]")
        if not isinstance(content, str):
            content = json.dumps(content, ensure_ascii=False)
        return LLMResponse(
            content=content,
            tokens_in=int(self._data.get("tokens_in", 0)),
            tokens_out=int(self._data.get("tokens_out", 0)),
            model=str(self._data.get("model", "fixture")),
        )


async def run_single_llm_eval(
    brief_id: str,
    brief_text: str,
    registry: TaxonomyRegistry,
    mode: str,
    settings: LLMSettings,
    llm_dir: Path | None,
) -> tuple[
    list[ExtractedRequirement | dict[str, Any]],
    list[ExtractedRequirement],
    LLMStats | None,
    str,
    dict[str, int],
]:
    """Run LLM extraction for one brief and return:
    (llm_raw_reqs, llm_validated_reqs, llm_stats, empty_reason, drop_counts).
    """
    drop_counts = {
        "dropped_schema_invalid": 0,
        "dropped_unknown_taxonomy": 0,
        "dropped_no_evidence": 0,
        "post_processor_dropped": 0,
        "dropped_duplicate": 0,
        "dropped_over_limit": 0,
    }
    if mode == "rules":
        return [], [], None, "skipped (mode=rules)", drop_counts

    client: Any
    if mode == "live":
        if not settings.is_available:
            return [], [], None, "LLM_API_KEY not configured", drop_counts
        client = LLMClient(settings)
    elif mode == "fixtures":
        assert llm_dir is not None
        num = brief_id.split("_")[1] if "_" in brief_id else brief_id
        candidates = [
            llm_dir / f"{brief_id}_llm.json",
            llm_dir / f"{brief_id}_stages.json",
            llm_dir / f"brief_{num}_response.json",
        ]
        fixture_path = next((p for p in candidates if p.exists()), None)
        if fixture_path is None:
            return [], [], None, f"fixture not found in {llm_dir}", drop_counts
        dump = json.loads(fixture_path.read_text(encoding="utf-8"))
        if "content" in dump:
            client = _FixtureLLMClient(dump)
        elif "requirements" in dump or "llm_raw" in dump or "llm" in dump:
            empty_reason = dump.get("empty_reason") or dump.get("llm_empty_reason") or ""
            raw_items = dump.get("llm_raw") or dump.get("llm") or dump.get("requirements") or []
            if empty_reason and not raw_items:
                return [], [], None, str(empty_reason), drop_counts
            client = _FixtureLLMClient(
                {
                    "content": json.dumps(raw_items, ensure_ascii=False),
                    "tokens_in": (dump.get("llm_stats") or dump.get("stats") or {}).get("tokens_in", 0),
                    "tokens_out": (dump.get("llm_stats") or dump.get("stats") or {}).get("tokens_out", 0),
                    "model": "fixture",
                }
            )
        else:
            return [], [], None, f"unrecognized fixture format: {fixture_path.name}", drop_counts
    else:
        return [], [], None, f"unknown mode: {mode}", drop_counts

    try:
        effective_settings = (
            settings.model_copy(update={"max_retries": 0})
            if mode == "fixtures"
            else settings
        )
        reqs, stats = await llm_extract(
            brief_text, "ru", registry, client, effective_settings
        )
    except Exception as exc:
        return [], [], None, f"{type(exc).__name__}: {exc}", drop_counts

    raw_items: list[ExtractedRequirement | dict[str, Any]] = (
        getattr(stats, "raw_items", None) or list(reqs)
    )
    drop_counts["dropped_schema_invalid"] = getattr(stats, "dropped_schema_invalid", 0)
    drop_counts["dropped_unknown_taxonomy"] = getattr(
        stats, "dropped_unknown_taxonomy", 0
    )
    drop_counts["dropped_no_evidence"] = getattr(stats, "dropped_no_evidence", 0)
    drop_counts["post_processor_dropped"] = getattr(stats, "post_processor_dropped", 0)
    drop_counts["dropped_duplicate"] = getattr(stats, "dropped_duplicate", 0)
    drop_counts["dropped_over_limit"] = getattr(stats, "dropped_over_limit", 0)

    if not reqs and not raw_items:
        reason = getattr(stats, "empty_reason", "") or (
            "invalid JSON / schema" if not stats.json_valid else "empty LLM array"
        )
        return [], [], stats, reason, drop_counts

    empty_reason = getattr(stats, "empty_reason", "")
    return raw_items, list(reqs), stats, empty_reason, drop_counts


# ---------------------------------------------------------------------------
# Gold-audit helpers
# ---------------------------------------------------------------------------


def audit_gold(registry: TaxonomyRegistry) -> dict[str, Any]:
    """Check gold labels against the taxonomy."""
    comp_roles = {cid: set(c.roles) for cid, c in registry.competencies.items()}
    core = {rid: set(r.core_competencies) for rid, r in registry.roles.items()}

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


def _stage_table(
    stage_runs: dict[str, list[list[dict[str, Any]]]],
    brief_filter: set[str] | None = None,
) -> str:
    """Render aggregate stage metrics as markdown (with multi-run spread if runs > 1)."""
    lines = [
        "| Stage | Metric | TP | FP | FN | Precision | Recall | F1 | Spread (min..max, std) |",
        "|-------|--------|----|----|----|-----------|--------|----|------------------------|",
    ]
    for stage in STAGE_ORDER:
        if stage not in stage_runs:
            continue
        runs_for_stage = stage_runs[stage]
        if brief_filter is not None:
            filtered_runs = [
                [b for b in run_list if b.get("brief_id") in brief_filter]
                for run_list in runs_for_stage
            ]
        else:
            filtered_runs = runs_for_stage

        agg = aggregate_multi_run_metrics(filtered_runs)
        first = True
        for metric in METRIC_KEYS:
            m = agg[metric]
            label = f"`{stage}`" if first else ""
            first = False
            if "f1_min" in m:
                spread = f"{m['f1_min']:.4f}..{m['f1_max']:.4f} (±{m['f1_std']:.4f})"
            else:
                spread = "—"
            lines.append(
                f"| {label} | {metric} | {m['tp']} | {m['fp']} | {m['fn']} | "
                f"{m['precision']:.4f} | {m['recall']:.4f} | {m['f1']:.4f} | {spread} |"
            )
    return "\n".join(lines)


def _per_brief_table(stage_runs: dict[str, list[list[dict[str, Any]]]]) -> str:
    lines = [
        "| Brief | Split | Stage | Gold | Pred | Comp F1 | Pair F1 | Pair* F1 | Role Acc | Drops (ev/pair/dup) | Tokens (in/out) | Time ms | Notes |",
        "|-------|-------|-------|------|------|---------|---------|----------|----------|---------------------|-----------------|---------|-------|",
    ]
    rules_run0 = stage_runs["rules"][0]
    brief_count = len(rules_run0)
    holdout_set = set(HOLD_OUT_BRIEFS)

    for i in range(brief_count):
        brief_id = rules_run0[i].get("brief_id", f"brief_{i+1:03d}")
        split_label = "hold-out" if brief_id in holdout_set else "dev"
        first = True
        for stage in STAGE_ORDER:
            if stage not in stage_runs or not stage_runs[stage]:
                continue
            m = stage_runs[stage][0][i]
            notes = m.get("llm_empty_reason", "") or m.get("notes", "")
            label = brief_id if first else ""
            s_label = split_label if first else ""
            first = False
            drops_str = (
                f"{m.get('dropped_no_evidence', 0)}/"
                f"{m.get('post_processor_dropped', 0)}/"
                f"{m.get('dropped_duplicate', 0)}"
            )
            tok_in = m.get("llm_tokens_in")
            tok_out = m.get("llm_tokens_out")
            tok_str = f"{tok_in}/{tok_out}" if tok_in is not None else "—"
            dur_ms = m.get("llm_duration_ms") if m.get("llm_duration_ms") is not None else m.get("duration_ms", 0)
            lines.append(
                f"| {label} | {s_label} | `{stage}` | {m['gold_count']} | {m['predicted_count']} | "
                f"{m['competency']['f1']:.3f} | {m['pair']['f1']:.3f} | "
                f"{m['pair_relaxed']['f1']:.3f} | {m['role_accuracy']['accuracy']:.3f} | "
                f"{drops_str} | {tok_str} | {dur_ms} | {notes} |"
            )
    return "\n".join(lines)


def render_baseline_markdown(
    stage_runs: dict[str, list[list[dict[str, Any]]]],
    audit: dict[str, Any],
    duration_s: float,
    mode: str,
    runs: int,
) -> str:
    """Render the full evaluation report."""
    holdout_set = set(HOLD_OUT_BRIEFS)
    lines = [
        "# Extraction Evaluation & Baseline Report",
        "",
        f"- Generated: {datetime.now(timezone.utc).isoformat()}",
        f"- Mode: `{mode}`",
        f"- Runs per brief: `{runs}`",
        f"- Prompt version: `{PROMPT_VERSION}`",
        f"- Hold-out briefs ({len(HOLD_OUT_BRIEFS)}): `{', '.join(HOLD_OUT_BRIEFS)}`",
        f"- Total wall time: {duration_s:.2f}s",
        "",
        "## Aggregate metrics — All briefs (17 briefs, 116 gold requirements)",
        "",
        _stage_table(stage_runs, brief_filter=None),
        "",
        "## Aggregate metrics — Hold-out split (5 briefs, 30 gold requirements)",
        "",
        _stage_table(stage_runs, brief_filter=holdout_set),
        "",
        "*Metric definitions:*",
        "- `competency`: unique `competency_id` set per brief.",
        "- `pair`: exact `(role_id, competency_id)` set per brief.",
        "- `pair_relaxed`: `(role_id, competency_id)` where role matches if `role_id in competency.roles`.",
        "- `role_accuracy`: share of matched competencies (`gold_comps & pred_comps`) assigned to the exact gold `role_id` (`correct_roles / matched_comps`).",
        "",
        "## Per-brief metrics (Run 1)",
        "",
        _per_brief_table(stage_runs),
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
        lines.append("None (0 errors; all 116 gold pairs satisfy `role_id in competency.roles`).")
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


def _load_effective_settings(api_key_arg: str | None, model_arg: str | None) -> LLMSettings:
    """Load LLMSettings checking CLI args, environment, ml/.env, and repo .env."""
    clear_settings_cache()
    settings = get_settings()
    if not settings.is_available:
        for candidate in (ML_DIR / ".env", REPO_ROOT / ".env"):
            if candidate.exists():
                s = LLMSettings(_env_file=str(candidate))
                if s.is_available:
                    settings = s
                    break
    if api_key_arg:
        settings = settings.model_copy(update={"api_key": api_key_arg.strip()})
    if model_arg:
        settings = settings.model_copy(update={"model": model_arg.strip()})
    return settings


async def main() -> None:
    parser = argparse.ArgumentParser(description="Run extraction benchmark")
    parser.add_argument(
        "--mode",
        choices=["auto", "rules", "live", "fixtures"],
        default="auto",
        help="Evaluation mode: auto (live if LLM_API_KEY configured, else rules), rules, live, or fixtures",
    )
    parser.add_argument(
        "--runs",
        type=int,
        default=None,
        help="Number of runs per brief for LLM stages (default: 3 for live, 1 for rules/fixtures)",
    )
    parser.add_argument(
        "--llm-dir",
        type=Path,
        default=None,
        help="Directory with recorded LLM responses (fixtures mode)",
    )
    parser.add_argument(
        "--api-key",
        type=str,
        default=None,
        help="Optional LLM API key override",
    )
    parser.add_argument(
        "--model",
        type=str,
        default=None,
        help="Optional LLM model override",
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
    settings = _load_effective_settings(args.api_key, args.model)

    mode = args.mode
    if mode == "auto":
        if args.llm_dir is not None:
            mode = "fixtures"
        elif settings.is_available:
            mode = "live"
        else:
            mode = "rules"

    if mode == "live" and not settings.is_available:
        print(
            "ERROR: --mode live requires LLM_API_KEY (in environment, ml/.env, .env, or --api-key)",
            file=sys.stderr,
            flush=True,
        )
        sys.exit(1)
    if mode == "fixtures" and (not args.llm_dir or not args.llm_dir.exists()):
        print("ERROR: --mode fixtures requires --llm-dir", file=sys.stderr, flush=True)
        sys.exit(1)

    num_runs = args.runs if args.runs is not None else (3 if mode == "live" else 1)

    print("=" * 72, flush=True)
    print("  Extraction Evaluation Runner (eval/run_eval.py)", flush=True)
    print("=" * 72, flush=True)
    print(f"  Mode:           {mode}", flush=True)
    print(f"  Model:          {settings.model if mode != 'rules' else 'n/a (rules-only)'}", flush=True)
    print(f"  Base URL:       {settings.base_url}", flush=True)
    print(f"  API Key:        {_mask_key(settings.api_key)}", flush=True)
    print(f"  Prompt version: {PROMPT_VERSION}", flush=True)
    print(f"  Briefs:         {len(briefs)} (hold-out: {len(HOLD_OUT_BRIEFS)})", flush=True)
    print(f"  Runs per brief: {num_runs}", flush=True)
    print("=" * 72, flush=True)

    stage_runs: dict[str, list[list[dict[str, Any]]]] = {s: [] for s in STAGE_ORDER}
    raw_dumps_by_run: list[dict[str, dict[str, Any]]] = []

    for run_idx in range(num_runs):
        if num_runs > 1:
            print(f"\n--- Run {run_idx + 1}/{num_runs} ---", flush=True)
        run_stage_results: dict[str, list[dict[str, Any]]] = defaultdict(list)
        raw_dumps: dict[str, dict[str, Any]] = {}

        for b_idx, brief in enumerate(briefs, 1):
            bid = brief["id"]
            text = brief["text"]
            gold = brief["gold"]

            print(
                f"  [Run {run_idx + 1}/{num_runs}] [{b_idx:02d}/{len(briefs):02d}] {bid:32s} ... ",
                end="",
                flush=True,
            )

            # Stage 1: rules (reference only)
            t0 = time.monotonic()
            rules_reqs, rules_dropped = run_rules_stage(text, registry)
            rules_duration_ms = int((time.monotonic() - t0) * 1000)

            # Stage 2 & 3: llm_raw and llm_validated
            llm_raw_reqs, llm_val_reqs, llm_stats, llm_empty_reason, drop_counts = (
                await run_single_llm_eval(
                    bid, text, registry, mode, settings, args.llm_dir
                )
            )

            # Stage 4: final_api
            if mode == "rules":
                final_reqs, final_drops = run_deterministic_validation(rules_reqs, text, registry)
            else:
                final_reqs = list(llm_val_reqs)
                final_drops = dict(drop_counts)

            # Compute metrics for each stage
            rules_metrics = compute_metrics(gold, rules_reqs, registry)
            rules_metrics.update(
                {
                    "brief_id": bid,
                    "duration_ms": rules_duration_ms,
                    "dropped_no_evidence": rules_dropped,
                    "post_processor_dropped": 0,
                    "dropped_duplicate": 0,
                    "llm_empty_reason": "",
                    "llm_tokens_in": None,
                    "llm_tokens_out": None,
                    "llm_duration_ms": None,
                }
            )
            run_stage_results["rules"].append(rules_metrics)

            llm_raw_metrics = compute_metrics(gold, llm_raw_reqs, registry)
            llm_raw_metrics.update(
                {
                    "brief_id": bid,
                    "duration_ms": llm_stats.duration_ms if llm_stats else 0,
                    "dropped_no_evidence": 0,
                    "post_processor_dropped": 0,
                    "dropped_duplicate": 0,
                    "llm_empty_reason": llm_empty_reason,
                    "llm_tokens_in": llm_stats.tokens_in if llm_stats else None,
                    "llm_tokens_out": llm_stats.tokens_out if llm_stats else None,
                    "llm_duration_ms": llm_stats.duration_ms if llm_stats else None,
                }
            )
            run_stage_results["llm_raw"].append(llm_raw_metrics)

            llm_val_metrics = compute_metrics(gold, llm_val_reqs, registry)
            llm_val_metrics.update(
                {
                    "brief_id": bid,
                    "duration_ms": llm_stats.duration_ms if llm_stats else 0,
                    "dropped_no_evidence": drop_counts["dropped_no_evidence"],
                    "post_processor_dropped": drop_counts["post_processor_dropped"],
                    "dropped_duplicate": drop_counts["dropped_duplicate"],
                    "llm_empty_reason": llm_empty_reason,
                    "llm_tokens_in": llm_stats.tokens_in if llm_stats else None,
                    "llm_tokens_out": llm_stats.tokens_out if llm_stats else None,
                    "llm_duration_ms": llm_stats.duration_ms if llm_stats else None,
                }
            )
            run_stage_results["llm_validated"].append(llm_val_metrics)

            final_metrics = compute_metrics(gold, final_reqs, registry)
            final_metrics.update(
                {
                    "brief_id": bid,
                    "duration_ms": llm_stats.duration_ms if llm_stats else rules_duration_ms,
                    "dropped_no_evidence": final_drops.get("dropped_no_evidence", 0),
                    "post_processor_dropped": final_drops.get("post_processor_dropped", 0),
                    "dropped_duplicate": final_drops.get("dropped_duplicate", 0),
                    "llm_empty_reason": llm_empty_reason if mode != "rules" else "",
                    "llm_tokens_in": llm_stats.tokens_in if llm_stats else None,
                    "llm_tokens_out": llm_stats.tokens_out if llm_stats else None,
                    "llm_duration_ms": llm_stats.duration_ms if llm_stats else None,
                }
            )
            run_stage_results["final_api"].append(final_metrics)

            if mode == "rules":
                print(
                    f"gold={final_metrics['gold_count']:2d} pred={final_metrics['predicted_count']:2d} | "
                    f"CompF1={final_metrics['competency']['f1']:.3f} "
                    f"PairF1={final_metrics['pair']['f1']:.3f} | "
                    f"{rules_duration_ms}ms",
                    flush=True,
                )
            elif llm_empty_reason and not final_reqs:
                print(f"FAIL ({llm_empty_reason})", flush=True)
            else:
                tok_str = (
                    f"{llm_stats.tokens_in}/{llm_stats.tokens_out}"
                    if llm_stats
                    else "0/0"
                )
                dur_ms = llm_stats.duration_ms if llm_stats else 0
                print(
                    f"gold={final_metrics['gold_count']:2d} pred={final_metrics['predicted_count']:2d} | "
                    f"CompF1={final_metrics['competency']['f1']:.3f} "
                    f"PairF1={final_metrics['pair']['f1']:.3f} "
                    f"RoleAcc={final_metrics['role_accuracy']['accuracy']:.3f} | "
                    f"tok={tok_str} | {dur_ms}ms",
                    flush=True,
                )

            raw_dumps[bid] = {
                "brief_id": bid,
                "run_index": run_idx,
                "brief_text": text,
                "gold": gold,
                "rules": _normalize_for_dump(rules_reqs),
                "llm_raw": _normalize_for_dump(llm_raw_reqs),
                "llm_validated": _normalize_for_dump(llm_val_reqs),
                "final_api": _normalize_for_dump(final_reqs),
                "drop_counts": drop_counts,
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

        for s in STAGE_ORDER:
            stage_runs[s].append(run_stage_results[s])
        raw_dumps_by_run.append(raw_dumps)

    duration_s = time.monotonic() - t_start
    audit = audit_gold(registry)

    md = render_baseline_markdown(stage_runs, audit, duration_s, mode, num_runs)
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(md, encoding="utf-8")

    print("\n" + "=" * 72, flush=True)
    print("  Aggregate Metrics — All Briefs (17 briefs, 116 gold reqs)", flush=True)
    print("=" * 72, flush=True)
    print(_stage_table(stage_runs, brief_filter=None), flush=True)
    print("\n" + "=" * 72, flush=True)
    print("  Aggregate Metrics — Hold-out Split (5 briefs, 30 gold reqs)", flush=True)
    print("=" * 72, flush=True)
    print(_stage_table(stage_runs, brief_filter=set(HOLD_OUT_BRIEFS)), flush=True)
    print(f"\nWrote evaluation report to {args.out} ({duration_s:.2f}s)", flush=True)

    if args.dump_raw or mode == "live":
        run_dir = RUNS_DIR / datetime.now(timezone.utc).strftime("%Y%m%d_%H%M%S")
        run_dir.mkdir(parents=True, exist_ok=True)
        for run_idx, raw_dumps in enumerate(raw_dumps_by_run):
            for bid, dump in raw_dumps.items():
                suffix = f"_run{run_idx + 1}" if num_runs > 1 else ""
                (run_dir / f"{bid}{suffix}_stages.json").write_text(
                    json.dumps(dump, ensure_ascii=False, indent=2), encoding="utf-8"
                )
        print(f"Wrote raw stage dumps to {run_dir}", flush=True)


if __name__ == "__main__":
    asyncio.run(main())
