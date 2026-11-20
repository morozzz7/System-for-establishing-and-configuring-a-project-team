"""Tests for diagnostics builder."""

from __future__ import annotations

from app.extract.diagnostics import build_diagnostics
from app.extract.llm_extract import LLMStats


class TestBuildDiagnostics:
    def test_rules_only(self):
        d = build_diagnostics(
            llm_stats=None,
            engine="rules",
            model="n/a",
            duration_ms=100,
            dropped_no_evidence=2,
        )
        assert d.engine == "rules"
        assert d.llm_tokens_in is None
        assert d.llm_tokens_out is None
        assert d.retries == 0
        assert d.dropped_no_evidence == 2
        assert d.duration_ms == 100
        assert d.prompt_version == "extract-v4"
        assert d.temperature == 0

    def test_llm_only(self):
        stats = LLMStats(tokens_in=500, tokens_out=200, duration_ms=800, retries=0, json_valid=True)
        d = build_diagnostics(
            llm_stats=stats,
            engine="llm",
            model="qwen/qwen-2.5-72b-instruct",
            duration_ms=900,
            dropped_no_evidence=0,
        )
        assert d.engine == "llm"
        assert d.llm_tokens_in == 500
        assert d.llm_tokens_out == 200
        assert d.model == "qwen/qwen-2.5-72b-instruct"

    def test_hybrid(self):
        stats = LLMStats(tokens_in=300, tokens_out=150, duration_ms=500, retries=1, json_valid=True)
        d = build_diagnostics(
            llm_stats=stats,
            engine="hybrid",
            model="qwen/qwen-2.5-72b-instruct",
            duration_ms=600,
            dropped_no_evidence=1,
        )
        assert d.engine == "hybrid"
        assert d.retries == 1
        assert d.dropped_no_evidence == 1

    def test_dropped_count_propagated(self):
        d = build_diagnostics(
            llm_stats=None,
            engine="rules",
            model="n/a",
            duration_ms=50,
            dropped_no_evidence=5,
            post_processor_dropped=3,
        )
        assert d.dropped_no_evidence == 5
        assert d.post_processor_dropped == 3
        assert "post_processor_dropped" not in d.model_dump()

    def test_duration_aggregated(self):
        d = build_diagnostics(
            llm_stats=None,
            engine="rules",
            model="n/a",
            duration_ms=1234,
            dropped_no_evidence=0,
        )
        assert d.duration_ms == 1234
