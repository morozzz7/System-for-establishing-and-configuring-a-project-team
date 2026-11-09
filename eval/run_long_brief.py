#!/usr/bin/env python3
"""eval/run_long_brief.py — Runner for evaluating extraction on a large Russian TZ (mega-brief).

Supports:
  - Parallel ensemble union (`--ensemble-runs 3`, default 3)
  - Word-boundary candidate competency scanner + short LLM verification call
  - Role safety net (target Role Recall = 1.0)
  - Headcount reconciliation (`headcount_accuracy` & `headcount_multi`)
  - Multi-run stability & blinking-pair analysis (`--runs 10`)
  - Planted trap trigger rate (`traps`)
  - ADR-006 review routing (`requirements_for_review` vs `requirements`) and
    diagnostics (`verification`, `dropped_generic_mention`)

Usage:
    # Single evaluation (with 3x parallel ensemble + candidate verification + role safety net)
    python3 eval/run_long_brief.py

    # 10-run stability evaluation (computes per-pair hit_rate across 10 runs)
    python3 eval/run_long_brief.py --runs 10

    # Specify either eval/long_brief1/brief.md or eval/long_brief/brief.md
    python3 eval/run_long_brief.py --brief eval/long_brief1/brief.md

    # Offline rules check (no LLM call)
    python3 eval/run_long_brief.py --mode rules
"""

from __future__ import annotations

import argparse
import asyncio
import json
import sys
import time
from pathlib import Path
from typing import Any

REPO_ROOT = Path(__file__).resolve().parent.parent
ML_DIR = REPO_ROOT / "ml"
GOLD_17_DIR = REPO_ROOT / "eval" / "gold"

# Prefer eval/long_brief1/brief.md if present (user's 30-req scattered brief with traps),
# otherwise eval/long_brief/brief.md.
_LONG_BRIEF_1 = REPO_ROOT / "eval" / "long_brief2" / "brief.md"
_LONG_BRIEF_0 = REPO_ROOT / "eval" / "long_brief" / "brief.md"
DEFAULT_BRIEF_PATH = _LONG_BRIEF_1 if _LONG_BRIEF_1.exists() else _LONG_BRIEF_0
DEFAULT_OUT_PATH = REPO_ROOT / "eval" / "runs" / "long_brief_latest.json"

# Known planted traps by brief directory (legacy, rejected prototype, customer scripts, rejected brokers/clouds)
KNOWN_TRAPS_BY_BRIEF: dict[str, set[str]] = {
    "long_brief1": {
        "lang_java",
        "db_mongodb",
        "fw_angular",
        "test_selenium",
        "lang_python",
        "lang_javascript",
        "mq_rabbitmq",
        "cloud_aws",
        "cloud_gcp",
    },
    "long_brief2": {
        "fw_angular",
        "db_mysql",
        "test_selenium",
        "lang_python",
        "mq_rabbitmq",
        "sec_siem",
        "cloud_aws",
        "api_graphql",
        "lang_javascript",
        "proc_agile_scrum",
    },
}

sys.path.insert(0, str(ML_DIR))

from app.extract.llm_extract import LLMStats, llm_extract  # noqa: E402
from app.extract.prompt import PROMPT_VERSION  # noqa: E402
from app.extract.review import REVIEW_CONFIDENCE_MAX, split_for_review  # noqa: E402
from app.extract.schemas import ExtractedRequirement  # noqa: E402
from app.extract.taxonomy import get_registry  # noqa: E402
from app.extract.validator import EvidenceValidator  # noqa: E402
from app.llm.client import LLMClient  # noqa: E402

from run_eval import (  # noqa: E402
    _load_effective_settings,
    _mask_key,
    _sum_pair_blocks,
    aggregate_multi_run_metrics,
    compute_metrics,
    compute_pair_hit_rates,
    diagnostics_from_stats,
    run_rules_stage,
)


def _gold_17_total_chars() -> int:
    total = 0
    if GOLD_17_DIR.exists():
        for d in sorted(GOLD_17_DIR.glob("brief_*")):
            p = d / "brief.md"
            if p.exists():
                total += len(p.read_text(encoding="utf-8"))
    return total or 7004


async def main() -> None:
    parser = argparse.ArgumentParser(
        description="Run extraction on the mega-brief (eval/long_brief1 or eval/long_brief)"
    )
    parser.add_argument(
        "--brief",
        type=Path,
        default=DEFAULT_BRIEF_PATH,
        help=f"Path to brief markdown/text file (default: {DEFAULT_BRIEF_PATH})",
    )
    parser.add_argument(
        "--gold",
        type=Path,
        default=None,
        help="Path to gold.json file (default: <brief_dir>/gold.json)",
    )
    parser.add_argument(
        "--mode",
        choices=["auto", "live", "rules"],
        default="auto",
        help="Execution mode: auto (live if LLM_API_KEY configured, else rules), live, or rules",
    )
    parser.add_argument(
        "--runs",
        type=int,
        default=1,
        help="Number of evaluation runs to measure stability and per-pair hit_rate (e.g. --runs 10)",
    )
    parser.add_argument(
        "--ensemble-runs",
        type=int,
        default=None,
        help="Number of parallel LLM calls per run in the union ensemble (default: 3 from settings)",
    )
    parser.add_argument(
        "--no-verify-candidates",
        action="store_true",
        help="Disable word-boundary candidate scanner + LLM verification call",
    )
    parser.add_argument(
        "--no-role-safety-net",
        action="store_true",
        help="Disable missing-role safety net",
    )
    parser.add_argument(
        "--timeout-ms",
        type=int,
        default=300000,
        help="LLM request timeout in ms per HTTP call (default: 300000 = 300s)",
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
        default=DEFAULT_OUT_PATH,
        help=f"Output JSON path (default: {DEFAULT_OUT_PATH})",
    )
    parser.add_argument(
        "--strict",
        action="store_true",
        help="Exit non-zero if the ADR-006 diagnostics asserts fail "
        "(verification accounting mismatch or failed_batches > 0)",
    )
    args = parser.parse_args()

    if not args.brief.exists():
        print(f"ERROR: brief file not found: {args.brief}", file=sys.stderr)
        sys.exit(1)

    gold_path = args.gold if args.gold is not None else (args.brief.parent / "gold.json")
    brief_text = args.brief.read_text(encoding="utf-8")
    gold_reqs: list[dict[str, Any]] = []
    if gold_path.exists():
        gold_reqs = json.loads(gold_path.read_text(encoding="utf-8"))

    explicit_traps = KNOWN_TRAPS_BY_BRIEF.get(args.brief.parent.name)

    gold_17_chars = _gold_17_total_chars()
    ratio = len(brief_text) / gold_17_chars if gold_17_chars else 1.0

    registry = get_registry()
    settings = _load_effective_settings(args.api_key, args.model, args.timeout_ms)
    if args.ensemble_runs is not None:
        settings = settings.model_copy(update={"ensemble_runs": args.ensemble_runs})
    if args.no_verify_candidates:
        settings = settings.model_copy(update={"verify_candidates": False})
    if args.no_role_safety_net:
        settings = settings.model_copy(update={"role_safety_net": False})

    mode = args.mode
    if mode == "auto":
        mode = "live" if settings.is_available else "rules"

    if mode == "live" and not settings.is_available:
        print(
            "ERROR: --mode live requires LLM_API_KEY in ml/.env, .env, environment, or --api-key",
            file=sys.stderr,
        )
        sys.exit(1)

    num_runs = max(1, args.runs)

    print("=" * 84, flush=True)
    print("  Mega-Brief Extraction Runner (eval/run_long_brief.py)", flush=True)
    print("=" * 84, flush=True)
    print(f"  Brief file:         {args.brief}", flush=True)
    print(f"  Gold file:          {gold_path if gold_path.exists() else 'none'} ({len(gold_reqs)} reqs)", flush=True)
    print(
        f"  Brief size:         {len(brief_text):,} chars | {len(brief_text.split()):,} words | "
        f"{len(brief_text.splitlines())} lines ({ratio:.2f}x of 17 gold briefs combined)",
        flush=True,
    )
    print(f"  Mode:               {mode}", flush=True)
    print(f"  Model:              {settings.model if mode == 'live' else 'n/a (rules)'}", flush=True)
    print(f"  API Key:            {_mask_key(settings.api_key)}", flush=True)
    print(f"  Timeout per call:   {settings.timeout_ms} ms ({settings.timeout_ms / 1000:.0f}s)", flush=True)
    print(f"  Ensemble runs (K):  {settings.ensemble_runs if mode == 'live' else 1}", flush=True)
    print(f"  Verify candidates:  {settings.verify_candidates if mode == 'live' else False}", flush=True)
    print(f"  Role safety net:    {settings.role_safety_net if mode == 'live' else False}", flush=True)
    print(f"  Eval runs (N):      {num_runs}", flush=True)
    print(f"  Prompt version:     {PROMPT_VERSION}", flush=True)
    print("=" * 84, flush=True)

    all_runs_reqs: list[list[ExtractedRequirement]] = []
    all_runs_main: list[list[ExtractedRequirement]] = []
    all_runs_review: list[list[ExtractedRequirement]] = []
    all_runs_metrics: list[dict[str, Any]] = []
    all_runs_stats: list[LLMStats | None] = []
    t_overall = time.monotonic()

    for run_idx in range(num_runs):
        t0 = time.monotonic()
        stats: LLMStats | None = None
        reqs: list[ExtractedRequirement] = []

        if mode == "live":
            print(
                f"\n[Run {run_idx + 1}/{num_runs}] Calling LLM pipeline "
                f"(ensemble={settings.ensemble_runs}, verify={settings.verify_candidates}, "
                f"role_net={settings.role_safety_net}, timeout={settings.timeout_ms // 1000}s) ... ",
                end="",
                flush=True,
            )
            client = LLMClient(settings)
            try:
                raw_reqs, stats = await llm_extract(
                    brief_text,
                    "ru",
                    registry,
                    client,
                    settings,
                    max_requirements=40,
                    timeout_ms=settings.timeout_ms,
                    ensemble_runs=settings.ensemble_runs,
                    verify_candidates=settings.verify_candidates,
                    role_safety_net=settings.role_safety_net,
                )
                validator = EvidenceValidator()
                reqs, _ = validator.validate_pipeline(
                    raw_reqs, brief_text, registry, max_requirements=40
                )
                elapsed_ms = int((time.monotonic() - t0) * 1000)
                print(
                    f"DONE in {elapsed_ms} ms | pred={len(reqs)} | "
                    f"tok={stats.tokens_in}/{stats.tokens_out} | "
                    f"cands: found={stats.candidate_competencies_found} "
                    f"(confirmed={stats.candidate_competencies_confirmed}, "
                    f"rejected={stats.candidate_competencies_rejected}, "
                    f"skipped={stats.candidate_competencies_skipped}) | "
                    f"roles: found={stats.missing_roles_found} "
                    f"(recovered={stats.missing_roles_recovered}, "
                    f"rejected={stats.missing_roles_rejected}, "
                    f"skipped={stats.missing_roles_skipped}) | "
                    f"lexical_flagged={stats.flagged_no_lexical_anchor}",
                    flush=True,
                )
            except Exception as exc:
                elapsed_ms = int((time.monotonic() - t0) * 1000)
                print(
                    f"FAILED after {elapsed_ms} ms: {type(exc).__name__}: {exc}",
                    flush=True,
                )
                if num_runs == 1:
                    sys.exit(1)
                continue
        else:
            print(
                f"\n[Run {run_idx + 1}/{num_runs}] Running deterministic rules stage ... ",
                end="",
                flush=True,
            )
            reqs, _ = run_rules_stage(brief_text, registry)
            elapsed_ms = int((time.monotonic() - t0) * 1000)
            print(f"DONE in {elapsed_ms} ms | pred={len(reqs)}", flush=True)

        all_runs_reqs.append(reqs)
        all_runs_stats.append(stats)
        main_reqs, review_reqs = split_for_review(reqs)
        all_runs_main.append(main_reqs)
        all_runs_review.append(review_reqs)
        print(
            f"    -> ADR-006 routing: main={len(main_reqs)} "
            f"(to /v1/optimize), review={len(review_reqs)} "
            f"(confidence <= {REVIEW_CONFIDENCE_MAX})",
            flush=True,
        )
        if gold_reqs:
            m = compute_metrics(
                gold_reqs, reqs, registry, brief_text, explicit_traps=explicit_traps
            )
            # ADR-006 additive blocks: `pair` above stays on the full validated list.
            m["pair_main"] = compute_metrics(
                gold_reqs,
                main_reqs,
                registry,
                brief_text,
                explicit_traps=explicit_traps,
            )["pair"]
            m["pair_review"] = compute_metrics(
                gold_reqs,
                review_reqs,
                registry,
                brief_text,
                explicit_traps=explicit_traps,
            )["pair"]
            m["predicted_main_count"] = len(main_reqs)
            m["review_count"] = len(review_reqs)
            all_runs_metrics.append(m)
            if num_runs > 1:
                print(
                    f"    -> RoleRec={m['role']['recall']:.4f} | "
                    f"CompF1={m['competency']['f1']:.4f} | "
                    f"PairF1={m['pair']['f1']:.4f} (TP={m['pair']['tp']} FP={m['pair']['fp']} FN={m['pair']['fn']}) | "
                    f"PairMainF1={m['pair_main']['f1']:.4f} | "
                    f"HCAcc={m['headcount_accuracy']['accuracy']:.4f} | "
                    f"Traps={m['traps']['triggered_count']}/{m['traps']['total_traps']}",
                    flush=True,
                )

    if not all_runs_reqs:
        print("ERROR: No runs completed.", file=sys.stderr)
        sys.exit(1)

    last_reqs = all_runs_reqs[-1]
    last_main = all_runs_main[-1]
    last_review = all_runs_review[-1]
    last_stats = all_runs_stats[-1]
    total_elapsed_ms = int((time.monotonic() - t_overall) * 1000)

    # Display extracted requirements from the latest run, split per ADR-006 §2.
    print("\n" + "-" * 84, flush=True)
    print(
        f"  requirements — main, sent to /v1/optimize ({len(last_main)} of "
        f"{len(last_reqs)} validated items)",
        flush=True,
    )
    print("-" * 84, flush=True)
    print(
        f"  {'#':>2s} | {'role_id':20s} | {'competency_id':24s} | {'lvl':>3s} | {'crit':6s} | {'hc':>2s} | {'conf':>4s} | {'rev':>3s} | {'quote'}",
        flush=True,
    )
    print("  " + "-" * 80, flush=True)
    hallucinations = 0
    for i, r in enumerate(last_main, 1):
        cid = r.competency.competency_id if r.competency else "(role-only)"
        quote = r.evidence[0].quote if r.evidence else ""
        if quote not in brief_text:
            hallucinations += 1
        short_q = (quote[:42] + "...") if len(quote) > 45 else quote
        rev_flag = "yes" if r.needs_review else "no"
        print(
            f"  {i:2d} | {r.role.role_id:20s} | {cid:24s} | {r.level:3d} | {r.criticality:6s} | "
            f"{r.headcount:2d} | {r.confidence:4.2f} | {rev_flag:>3s} | {short_q!r}",
            flush=True,
        )

    print("\n" + "-" * 84, flush=True)
    print(
        f"  requirements_for_review — not sent to /v1/optimize until a human confirms "
        f"({len(last_review)} items; needs_review=true and confidence <= {REVIEW_CONFIDENCE_MAX})",
        flush=True,
    )
    print("-" * 84, flush=True)
    print(
        f"  {'#':>2s} | {'role_id':20s} | {'competency_id':24s} | {'conf':>4s} | {'quote'}",
        flush=True,
    )
    print("  " + "-" * 80, flush=True)
    for i, r in enumerate(last_review, 1):
        cid = r.competency.competency_id if r.competency else "(role-only)"
        quote = r.evidence[0].quote if r.evidence else ""
        if quote not in brief_text:
            hallucinations += 1
        short_q = (quote[:42] + "...") if len(quote) > 45 else quote
        print(
            f"  {i:2d} | {r.role.role_id:20s} | {cid:24s} | {r.confidence:4.2f} | {short_q!r}",
            flush=True,
        )

    print(f"\n  Hallucinated quotes (quote not in brief_text): {hallucinations}", flush=True)
    if last_stats and last_stats.flagged_no_lexical_anchor > 0:
        print(
            f"  Flagged without lexical competency anchor (confidence lowered, needs_review=True): "
            f"{last_stats.flagged_no_lexical_anchor}",
            flush=True,
        )

    if last_stats and last_stats.verification_log:
        print("\n" + "-" * 84, flush=True)
        print(
            f"  Candidate & Missing-Role Verification Log ("
            f"cands: {last_stats.candidate_competencies_found} = "
            f"{last_stats.candidate_competencies_confirmed} confirmed + "
            f"{last_stats.candidate_competencies_rejected} rejected + "
            f"{last_stats.candidate_competencies_skipped} skipped | "
            f"roles: {last_stats.missing_roles_found} = "
            f"{last_stats.missing_roles_recovered} recovered + "
            f"{last_stats.missing_roles_rejected} rejected + "
            f"{last_stats.missing_roles_skipped} skipped)",
            flush=True,
        )
        print("-" * 84, flush=True)
        for entry in last_stats.verification_log:
            print(
                f"  [{entry['status'].upper():9s}] {entry['kind']:10s} | {entry['id']:22s} "
                f"(matched={entry['matched_text']!r}) -> {entry['reason']}",
                flush=True,
            )

    # ADR-006 diagnostics: review routing + verification liveness (assert-style).
    diagnostics = diagnostics_from_stats(last_stats)
    assert_failures: list[str] = []
    print("\n" + "-" * 84, flush=True)
    print("  ADR-006 diagnostics", flush=True)
    print("-" * 84, flush=True)
    print(
        f"  predicted={len(last_reqs)} | main (to /v1/optimize)={len(last_main)} | "
        f"requirements_for_review={len(last_review)} | "
        f"dropped_generic_mention={diagnostics['dropped_generic_mention']}",
        flush=True,
    )
    verification = diagnostics["verification"]
    if verification is None:
        print(
            "  diagnostics.verification: n/a (rules mode — no candidate verification call runs)",
            flush=True,
        )
    else:
        found = verification["candidates_found"]
        resolved = (
            verification["confirmed"]
            + verification["rejected"]
            + verification["skipped"]
        )
        print(
            f"  verification: candidates_found={found} = "
            f"confirmed({verification['confirmed']}) + rejected({verification['rejected']}) "
            f"+ skipped({verification['skipped']}) | "
            f"failed_batches={verification['failed_batches']} | "
            f"attempts={verification['attempts']}",
            flush=True,
        )
        if found == resolved:
            print(
                f"  ASSERT verification accounting: candidates_found == "
                f"confirmed+rejected+skipped ({found} == {resolved}) -> PASS",
                flush=True,
            )
        else:
            print(
                f"  ASSERT verification accounting: candidates_found == "
                f"confirmed+rejected+skipped ({found} != {resolved}) -> FAIL",
                flush=True,
            )
            assert_failures.append("verification accounting mismatch")
        if verification["failed_batches"] == 0:
            print(
                "  ASSERT verification liveness: failed_batches == 0 -> PASS",
                flush=True,
            )
        else:
            print(
                f"  ASSERT verification liveness: failed_batches == 0 -> "
                f"FAIL (failed_batches={verification['failed_batches']})",
                flush=True,
            )
            assert_failures.append("verification failed_batches > 0")
    if assert_failures and args.strict:
        print(
            f"  STRICT: {len(assert_failures)} ADR-006 assert(s) failed -> non-zero exit",
            flush=True,
        )

    # Evaluation against gold
    summary_metrics: dict[str, Any] = {}
    hit_rates: list[dict[str, Any]] = []
    if gold_reqs and all_runs_metrics:
        summary_metrics = aggregate_multi_run_metrics([[m] for m in all_runs_metrics])
        last_m = all_runs_metrics[-1]
        summary_metrics["traps"] = last_m["traps"]
        summary_metrics["role_only_count"] = last_m["role_only_count"]
        # ADR-006 additive blocks (pair keeps its full-list semantics above).
        summary_metrics["pair_main"] = _sum_pair_blocks(
            [m["pair_main"] for m in all_runs_metrics]
        )
        summary_metrics["pair_review"] = _sum_pair_blocks(
            [m["pair_review"] for m in all_runs_metrics]
        )
        summary_metrics["predicted_main_count"] = last_m["predicted_main_count"]
        summary_metrics["review_count"] = last_m["review_count"]

        print("\n" + "=" * 84, flush=True)
        print(f"  Evaluation vs {gold_path.name} (Runs={len(all_runs_metrics)})", flush=True)
        print("=" * 84, flush=True)
        print(
            "  | Metric             |   TP  |   FP  |   FN  | Precision | Recall | F1 / Acc | Spread (min..max) |\n"
            "  |--------------------|-------|-------|-------|-----------|--------|----------|-------------------|",
            flush=True,
        )
        for k in (
            "role",
            "competency",
            "pair",
            "pair_main",
            "pair_review",
            "pair_relaxed",
            "role_accuracy",
            "headcount_accuracy",
            "headcount_multi",
        ):
            m = summary_metrics[k]
            spread = (
                f"{m['f1_min']:.4f}..{m['f1_max']:.4f}"
                if "f1_min" in m
                else "—"
            )
            print(
                f"  | {k:18s} | {m['tp']:5.1f} | {m['fp']:5.1f} | {m['fn']:5.1f} | "
                f"  {m['precision']:.4f}  | {m['recall']:.4f} |  {m['f1']:.4f}  | {spread:17s} |",
                flush=True,
            )

        traps_info = last_m["traps"]
        print(
            f"\n  Trap Trigger Rate: {traps_info['triggered_count']}/{traps_info['total_traps']} "
            f"({traps_info['trap_rate']:.2%}) "
            f"{'-> TRIGGERED: ' + str(traps_info['triggered_ids']) if traps_info['triggered_ids'] else '(0 traps triggered — PASS)'}",
            flush=True,
        )

        gold_pairs = {
            (
                g["role"]["role_id"],
                g["competency"]["competency_id"] if g.get("competency") else "",
            )
            for g in gold_reqs
            if g.get("competency")
        }
        # FP/FN on the main list (what reaches /v1/optimize), then review-only pairs.
        for label, subset in (("main", last_main), ("review", last_review)):
            subset_pairs = {
                (r.role.role_id, r.competency.competency_id)
                for r in subset
                if r.competency is not None
            }
            fp_pairs = sorted(subset_pairs - gold_pairs)
            fn_pairs = sorted(gold_pairs - subset_pairs)
            print(
                f"\n  [{label}] pairs vs full gold: predicted={len(subset_pairs)} "
                f"TP={len(subset_pairs & gold_pairs)} FP={len(fp_pairs)} FN={len(fn_pairs)}",
                flush=True,
            )
            if fp_pairs:
                print(
                    f"  [{label}] Extra predicted pairs (FP={len(fp_pairs)}): {fp_pairs}",
                    flush=True,
                )
            if fn_pairs and label == "main":
                print(
                    f"  [main] Missed gold pairs     (FN={len(fn_pairs)}): {fn_pairs}",
                    flush=True,
                )
            if label == "review":
                print(
                    "  [review] FN counts gold pairs that are (correctly) not in the review queue; "
                    "only the review FP list above is actionable.",
                    flush=True,
                )

        # Per-pair hit rate table across runs
        hit_rates = compute_pair_hit_rates(gold_reqs, all_runs_reqs)
        if len(all_runs_reqs) > 1:
            flaky = [r for r in hit_rates if 0.0 < r["hit_rate"] < 1.0]
            missed = [r for r in hit_rates if r["hit_rate"] == 0.0 and r["in_gold"]]
            stable = [r for r in hit_rates if r["hit_rate"] == 1.0 and r["in_gold"]]
            print("\n" + "=" * 84, flush=True)
            print(
                f"  Per-Pair Stability Across {len(all_runs_reqs)} Runs "
                f"(stable={len(stable)}, flaky={len(flaky)}, missed={len(missed)})",
                flush=True,
            )
            print("=" * 84, flush=True)
            if flaky or missed:
                print(
                    "  | role_id              | competency_id            | in_gold | hits/runs | hit_rate |\n"
                    "  |----------------------|--------------------------|---------|-----------|----------|",
                    flush=True,
                )
                for row in flaky + missed:
                    print(
                        f"  | {row['role_id']:20s} | {row['competency_id']:24s} | "
                        f"{str(row['in_gold']):7s} | {row['hits']:4d}/{row['num_runs']:<4d} |  {row['hit_rate']:.4f}  |",
                        flush=True,
                    )
            else:
                print("  All gold pairs achieved 100% (1.0000) hit rate across all runs!", flush=True)

    # Save report
    args.out.parent.mkdir(parents=True, exist_ok=True)
    payload = {
        "brief_path": str(args.brief),
        "brief_chars": len(brief_text),
        "brief_words": len(brief_text.split()),
        "ratio_vs_17_gold_briefs": round(ratio, 2),
        "mode": mode,
        "runs": len(all_runs_reqs),
        "model": settings.model if mode == "live" else "rules",
        "prompt_version": PROMPT_VERSION,
        "duration_ms": total_elapsed_ms,
        "llm_stats": (
            {
                "tokens_in": last_stats.tokens_in,
                "tokens_out": last_stats.tokens_out,
                "retries": last_stats.retries,
                "dropped_no_evidence": last_stats.dropped_no_evidence,
                "post_processor_dropped": last_stats.post_processor_dropped,
                "ensemble_runs_completed": last_stats.ensemble_runs_completed,
                "candidate_competencies_found": last_stats.candidate_competencies_found,
                "candidate_competencies_confirmed": last_stats.candidate_competencies_confirmed,
                "candidate_competencies_rejected": last_stats.candidate_competencies_rejected,
                "candidate_competencies_skipped": last_stats.candidate_competencies_skipped,
                "missing_roles_found": last_stats.missing_roles_found,
                "missing_roles_recovered": last_stats.missing_roles_recovered,
                "missing_roles_rejected": last_stats.missing_roles_rejected,
                "missing_roles_skipped": last_stats.missing_roles_skipped,
                "flagged_no_lexical_anchor": last_stats.flagged_no_lexical_anchor,
                "verification_log": last_stats.verification_log,
            }
            if last_stats
            else None
        ),
        "hallucination_count": hallucinations,
        "metrics": summary_metrics,
        "pair_hit_rates": hit_rates,
        "requirements": [r.model_dump(mode="json") for r in last_reqs],
        "requirements_main": [r.model_dump(mode="json") for r in last_main],
        "requirements_for_review": [r.model_dump(mode="json") for r in last_review],
        "review_count": len(last_review),
        "predicted_main_count": len(last_main),
        "diagnostics": diagnostics,
        "assert_failures": assert_failures,
    }
    args.out.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"\nSaved full JSON result to {args.out}", flush=True)

    if assert_failures and args.strict:
        sys.exit(1)


if __name__ == "__main__":
    asyncio.run(main())
