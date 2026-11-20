"""Diagnostics builder for ExtractDiagnostics schema."""

from __future__ import annotations

from .llm_extract import LLMStats
from .prompt import PROMPT_VERSION
from .schemas import ExtractDiagnostics, ExtractVerification


def _verification_from_stats(llm_stats: LLMStats | None) -> ExtractVerification | None:
    """Build the `diagnostics.verification` block from `LLMStats`, if available."""
    if llm_stats is None:
        return None
    return ExtractVerification(
        candidates_found=llm_stats.candidate_competencies_found,
        confirmed=llm_stats.candidate_competencies_confirmed,
        rejected=llm_stats.candidate_competencies_rejected,
        skipped=llm_stats.candidate_competencies_skipped,
        failed_batches=llm_stats.verification_failed_batches,
        attempts=llm_stats.verification_attempts,
    )


def build_diagnostics(
    *,
    llm_stats: LLMStats | None,
    engine: str,
    model: str,
    duration_ms: int,
    dropped_no_evidence: int,
    post_processor_dropped: int = 0,
    dropped_generic_mention: int = 0,
    json_valid: bool = True,
) -> ExtractDiagnostics:
    """Build an ExtractDiagnostics object from run statistics.

    Args:
        llm_stats: LLM call statistics (None if rules-only).
        engine: which engine(s) actually ran.
        model: model name used (or "n/a" for rules-only).
        duration_ms: total pipeline duration.
        dropped_no_evidence: count of requirements dropped by validator.
        post_processor_dropped: count of requirements dropped by post-processor.
        dropped_generic_mention: count dropped by the deterministic generic-mention gate.
        json_valid: whether the final JSON output is valid.
    """
    verification = _verification_from_stats(llm_stats)

    if llm_stats is not None:
        return ExtractDiagnostics(
            engine=engine,  # type: ignore[arg-type]
            model=model,
            prompt_version=PROMPT_VERSION,
            temperature=0,
            duration_ms=duration_ms,
            json_valid=json_valid and llm_stats.json_valid,
            retries=llm_stats.retries,
            dropped_no_evidence=dropped_no_evidence,
            post_processor_dropped=post_processor_dropped,
            dropped_generic_mention=dropped_generic_mention,
            verification=verification,
            llm_tokens_in=llm_stats.tokens_in,
            llm_tokens_out=llm_stats.tokens_out,
        )

    # Rules-only: no LLM stats
    return ExtractDiagnostics(
        engine=engine,  # type: ignore[arg-type]
        model=model,
        prompt_version=PROMPT_VERSION,
        temperature=0,
        duration_ms=duration_ms,
        json_valid=json_valid,
        retries=0,
        dropped_no_evidence=dropped_no_evidence,
        post_processor_dropped=post_processor_dropped,
        dropped_generic_mention=dropped_generic_mention,
        verification=None,
        llm_tokens_in=None,
        llm_tokens_out=None,
    )
