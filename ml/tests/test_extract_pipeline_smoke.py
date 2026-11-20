"""End-to-end smoke test: LLM-first extraction + deterministic 6-stage validator on all 17 briefs.

Acceptance criteria (3.7 & 3.8):
- Hallucination rate = 0% on ALL 17 briefs (`ev.quote in brief_text` and exact offset slice).
- Every extracted pair satisfies `role_id in competency.roles`.
- On `brief_001`, competency F1 >= 0.85 and `devops_engineer / cloud_yandex` is preserved.
"""

from __future__ import annotations

import asyncio
import json
from pathlib import Path

import pytest

from app.extract.llm_extract import llm_extract
from app.extract.schemas import ExtractedRequirement
from app.extract.taxonomy import get_registry, reset_registry
from app.extract.validator import EvidenceValidator
from app.llm.client import LLMResponse
from app.llm.config import LLMSettings

GOLD_DIR = Path(__file__).resolve().parents[2] / "eval" / "gold"
FIXTURES_DIR = Path(__file__).resolve().parent / "fixtures" / "llm"


class _ReplayClient:
    def __init__(self, fixture: dict) -> None:
        self._fixture = fixture

    async def chat(self, messages, **kwargs) -> LLMResponse:
        return LLMResponse(
            content=self._fixture["content"],
            tokens_in=self._fixture["tokens_in"],
            tokens_out=self._fixture["tokens_out"],
            model=self._fixture["model"],
        )


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


def _run_llm_first_pipeline(
    brief_text: str, fixture_num: str
) -> tuple[list[ExtractedRequirement], int]:
    """Run LLM-first extraction + 6-stage deterministic validation on a brief."""
    registry = get_registry()
    settings = LLMSettings(api_key="test-key", max_retries=1)
    fixture_path = FIXTURES_DIR / f"brief_{fixture_num}_response.json"
    fixture = json.loads(fixture_path.read_text(encoding="utf-8"))
    client = _ReplayClient(fixture)

    reqs, stats = asyncio.run(
        llm_extract(brief_text, "ru", registry, client, settings)
    )
    validator = EvidenceValidator()
    valid, vstats = validator.validate_pipeline(reqs, brief_text, registry)
    total_dropped = (
        stats.dropped_no_evidence
        + stats.post_processor_dropped
        + vstats.dropped_no_evidence
        + vstats.post_processor_dropped
    )
    return valid, total_dropped


def test_all_gold_briefs_zero_hallucination():
    """Run LLM-first pipeline on all 17 briefs. Fail if any hallucinated quote or invalid pair."""
    brief_dirs = sorted(GOLD_DIR.glob("brief_*"))
    assert len(brief_dirs) == 17, f"Expected 17 gold briefs, found {len(brief_dirs)}"
    registry = get_registry()

    for brief_dir in brief_dirs:
        brief_id = brief_dir.name
        num = brief_id.split("_")[1]
        brief_text = (brief_dir / "brief.md").read_text(encoding="utf-8")

        valid, _dropped = _run_llm_first_pipeline(brief_text, num)
        assert len(valid) > 0, f"{brief_id}: produced 0 requirements"

        for req in valid:
            if req.competency is not None:
                comp = registry.get_competency(req.competency.competency_id)
                assert comp is not None
                assert req.role.role_id in comp.roles, (
                    f"{brief_id}: invalid pair ({req.role.role_id}, {req.competency.competency_id})"
                )
            for ev in req.evidence:
                assert ev.quote in brief_text, (
                    f"{brief_id}: hallucinated quote {ev.quote!r}"
                )
                assert brief_text[ev.char_start : ev.char_end] == ev.quote


def test_brief_001_competency_f1_threshold_and_cloud_yandex():
    """Verify competency F1 >= 0.85 and devops_engineer/cloud_yandex on brief_001 (3.3 & 3.8)."""
    brief_dir = GOLD_DIR / "brief_001_simple_web_mvp"
    brief_text = (brief_dir / "brief.md").read_text(encoding="utf-8")
    gold = json.loads((brief_dir / "gold.json").read_text(encoding="utf-8"))

    valid, _ = _run_llm_first_pipeline(brief_text, "001")

    gold_comps = {
        r["competency"]["competency_id"] for r in gold if r.get("competency")
    }
    pred_comps = {
        r.competency.competency_id for r in valid if r.competency is not None
    }
    tp = len(gold_comps & pred_comps)
    fp = len(pred_comps - gold_comps)
    fn = len(gold_comps - pred_comps)
    precision = tp / (tp + fp) if (tp + fp) else 0.0
    recall = tp / (tp + fn) if (tp + fn) else 0.0
    f1 = 2 * precision * recall / (precision + recall) if (precision + recall) else 0.0

    assert f1 >= 0.85, f"brief_001 competency F1={f1:.3f} < 0.85"
    pairs = {(r.role.role_id, r.competency.competency_id) for r in valid if r.competency}
    assert ("devops_engineer", "cloud_yandex") in pairs


def test_pipeline_deterministic():
    """Same input -> same output (no randomness)."""
    brief_path = GOLD_DIR / "brief_001_simple_web_mvp" / "brief.md"
    text = brief_path.read_text(encoding="utf-8")

    r1, d1 = _run_llm_first_pipeline(text, "001")
    r2, d2 = _run_llm_first_pipeline(text, "001")

    assert d1 == d2
    assert len(r1) == len(r2)
    for a, b in zip(r1, r2):
        assert a.requirement_id == b.requirement_id
        assert a.role.role_id == b.role.role_id
        assert a.level == b.level
        assert a.criticality == b.criticality
