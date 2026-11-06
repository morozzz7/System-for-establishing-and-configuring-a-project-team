#!/usr/bin/env python3
"""run_benchmark.py — Run the extraction benchmark on gold briefs.

Usage:
    python eval/scripts/run_benchmark.py \\
        --gold-dir eval/gold \\
        --extract-url http://localhost:8100/v1/extract \\
        --output eval/reports/2026-11-05 \\
        --seed 42

Pipeline:
    1. Load all gold briefs.
    2. For each brief, POST text to /v1/extract and collect predicted requirements.
    3. Match predictions to gold by (role, competency) using Hungarian matching.
    4. Compute per-brief and aggregate metrics (F1, precision, recall, normalization, etc.).
    5. Write report to --output.{json,md}.

TODO (phase 3):
    - Implement actual HTTP call to /v1/extract.
    - Implement Hungarian matching for prediction-gold alignment.
    - Compute normalization top-1 / top-3 from `normalization` field in predictions.
    - Diff against previous report.
"""

import argparse
import json
import os
import sys
import time
from datetime import datetime
from pathlib import Path
from typing import Any

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def load_gold_briefs(gold_dir: Path) -> list[dict]:
    """Load all gold briefs from directory."""
    briefs = []
    for d in sorted(gold_dir.iterdir()):
        if not d.is_dir() or not d.name.startswith("brief_"):
            continue
        brief_path = d / "brief.md"
        gold_path = d / "gold.json"
        if not brief_path.exists() or not gold_path.exists():
            print(f"WARNING: skipping {d.name} (missing files)", file=sys.stderr)
            continue
        brief_text = brief_path.read_text(encoding="utf-8")
        # Strip front-matter
        if brief_text.startswith("---"):
            end = brief_text.find("---", 3)
            if end != -1:
                brief_text = brief_text[end + 3:].lstrip("\n")
        with open(gold_path, "r", encoding="utf-8") as f:
            gold_reqs = json.load(f)
        briefs.append({
            "id": d.name,
            "text": brief_text,
            "gold": gold_reqs,
        })
    return briefs


def call_extract(extract_url: str, brief_text: str, timeout_ms: int = 20000) -> dict:
    """Call /v1/extract endpoint.

    TODO (phase 3): implement actual HTTP call.
    For now, returns a stub response for testing the pipeline structure.
    """
    # TODO: implement actual HTTP POST
    # import httpx
    # response = httpx.post(
    #     extract_url,
    #     json={"brief_text": brief_text, "taxonomy_ref": "v1"},
    #     timeout=timeout_ms / 1000,
    # )
    # response.raise_for_status()
    # return response.json()
    print("WARNING: /v1/extract not implemented yet, returning empty predictions", file=sys.stderr)
    return {"requirements": [], "model_version": "stub", "prompt_version": "stub"}


# ---------------------------------------------------------------------------
# Metrics
# ---------------------------------------------------------------------------

def compute_metrics(gold_reqs: list[dict], pred_reqs: list[dict]) -> dict:
    """Compute extraction metrics for a single brief.

    TODO (phase 3): implement Hungarian matching and full metric computation.
    """
    n_gold = len(gold_reqs)
    n_pred = len(pred_reqs)

    # Placeholder metrics
    metrics = {
        "n_gold": n_gold,
        "n_pred": n_pred,
        "precision": 0.0,
        "recall": 0.0,
        "f1": 0.0,
        "level_accuracy_exact": 0.0,
        "level_accuracy_within_1": 0.0,
        "criticality_accuracy": 0.0,
        "normalization_top1": 0.0,
        "normalization_top3": 0.0,
        "hallucination_count": n_pred,  # all predictions are hallucinations in stub
        "hallucination_rate": 1.0 if n_pred > 0 else 0.0,
        "effort_mape": 0.0,
        "effort_range_hit": 0.0,
        "needs_review_share": 0.0,
    }

    # TODO: implement actual matching and metric computation
    # 1. Match pred to gold by (role_id, competency_id) using Hungarian algorithm
    # 2. For matched pairs: compute level accuracy, criticality accuracy, effort MAPE
    # 3. For unmatched predictions: count as hallucinations
    # 4. For unmatched gold: count as misses
    # 5. Compute normalization metrics from 'normalization' field in predictions

    return metrics


def aggregate_metrics(all_metrics: list[dict]) -> dict:
    """Aggregate per-brief metrics into summary."""
    if not all_metrics:
        return {}

    n_briefs = len(all_metrics)
    total_gold = sum(m["n_gold"] for m in all_metrics)
    total_pred = sum(m["n_pred"] for m in all_metrics)

    agg = {
        "n_briefs": n_briefs,
        "total_gold_reqs": total_gold,
        "total_pred_reqs": total_pred,
        "macro_avg_f1": sum(m["f1"] for m in all_metrics) / n_briefs,
        "macro_avg_precision": sum(m["precision"] for m in all_metrics) / n_briefs,
        "macro_avg_recall": sum(m["recall"] for m in all_metrics) / n_briefs,
        "hallucination_rate_total": sum(m["hallucination_count"] for m in all_metrics) / max(total_pred, 1),
        "needs_review_share_avg": sum(m["needs_review_share"] for m in all_metrics) / n_briefs,
    }

    # TODO: compute weighted averages for effort MAPE, normalization, etc.

    return agg


# ---------------------------------------------------------------------------
# Report generation
# ---------------------------------------------------------------------------

def write_json_report(report: dict, output_path: Path):
    """Write machine-readable JSON report."""
    output_path.with_suffix(".json").write_text(
        json.dumps(report, ensure_ascii=False, indent=2),
        encoding="utf-8"
    )


def write_markdown_report(report: dict, output_path: Path):
    """Write human-readable Markdown report."""
    lines = []
    lines.append(f"# Benchmark Report {report['date']}")
    lines.append("")
    lines.append(f"**Commit:** {report.get('commit_hash', 'N/A')}")
    lines.append(f"**Model:** {report.get('model_version', 'N/A')}")
    lines.append(f"**Prompt:** {report.get('prompt_version', 'N/A')}")
    lines.append(f"**Seed:** {report.get('seed', 'N/A')}")
    lines.append("")

    # Summary table
    agg = report["aggregate"]
    lines.append("## Summary")
    lines.append("")
    lines.append("| Metric | Value | Target | Status |")
    lines.append("|---|---|---|---|")
    lines.append(f"| Briefs | {agg['n_briefs']} | ≥ 15 | {'✅' if agg['n_briefs'] >= 15 else '❌'} |")
    lines.append(f"| F1 | {agg['macro_avg_f1']:.3f} | ≥ 0.85 | {'✅' if agg['macro_avg_f1'] >= 0.85 else '❌'} |")
    lines.append(f"| Hallucination rate | {agg['hallucination_rate_total']:.1%} | = 0% | {'✅' if agg['hallucination_rate_total'] == 0 else '❌'} |")
    lines.append(f"| needs_review share | {agg['needs_review_share_avg']:.1%} | 10-30% | {'✅' if 0.10 <= agg['needs_review_share_avg'] <= 0.30 else '❌'} |")
    lines.append("")

    # Per-brief breakdown
    lines.append("## Per-brief breakdown")
    lines.append("")
    lines.append("| Brief | Gold | Pred | F1 | Hallucinations |")
    lines.append("|---|---|---|---|---|")
    for bm in report["per_brief"]:
        lines.append(f"| {bm['brief_id']} | {bm['n_gold']} | {bm['n_pred']} | {bm['f1']:.3f} | {bm['hallucination_count']} |")
    lines.append("")

    # TODO: add diff vs previous report section

    output_path.with_suffix(".md").write_text("\n".join(lines), encoding="utf-8")


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

def main():
    parser = argparse.ArgumentParser(description="Run extraction benchmark on gold briefs")
    parser.add_argument("--gold-dir", required=True, help="Path to eval/gold/")
    parser.add_argument("--extract-url", required=True, help="URL of /v1/extract endpoint")
    parser.add_argument("--output", required=True, help="Output path (without extension)")
    parser.add_argument("--seed", type=int, default=42, help="Random seed for reproducibility")
    parser.add_argument("--timeout-ms", type=int, default=20000, help="Request timeout in ms")
    args = parser.parse_args()

    gold_dir = Path(args.gold_dir)
    output_path = Path(args.output)
    output_path.parent.mkdir(parents=True, exist_ok=True)

    print(f"Loading gold briefs from {gold_dir}...")
    briefs = load_gold_briefs(gold_dir)
    print(f"Loaded {len(briefs)} briefs")

    all_metrics = []
    per_brief_reports = []

    for brief in briefs:
        print(f"Processing {brief['id']}...")
        t0 = time.time()

        # Call /v1/extract
        pred_response = call_extract(args.extract_url, brief["text"], args.timeout_ms)
        pred_reqs = pred_response.get("requirements", [])

        elapsed = time.time() - t0

        # Compute metrics
        metrics = compute_metrics(brief["gold"], pred_reqs)
        metrics["brief_id"] = brief["id"]
        metrics["elapsed_s"] = elapsed
        all_metrics.append(metrics)

        per_brief_reports.append({
            "brief_id": brief["id"],
            **metrics,
        })

        print(f"  {metrics['n_gold']} gold, {metrics['n_pred']} pred, F1={metrics['f1']:.3f}")

    # Aggregate
    agg = aggregate_metrics(all_metrics)

    # Build report
    report = {
        "date": datetime.now().strftime("%Y-%m-%d"),
        "seed": args.seed,
        "commit_hash": "TODO",  # git rev-parse HEAD
        "model_version": "TODO",
        "prompt_version": "TODO",
        "aggregate": agg,
        "per_brief": per_brief_reports,
    }

    # Write reports
    write_json_report(report, output_path)
    write_markdown_report(report, output_path)

    print(f"\nReports written to {output_path}.json and {output_path}.md")
    print(f"Aggregate F1: {agg['macro_avg_f1']:.3f}")


if __name__ == "__main__":
    main()
