"""DEPRECATED: Merger of rules-based and LLM-based extraction results.

Per ADR-005 v2 (`tz_ml_extract_fix_v2.md` §3.7), `/v1/extract` is LLM-first and no
longer merges `RulesEngine` outputs into API responses. Retained only for legacy
unit tests.
"""

from __future__ import annotations

from .schemas import ExtractedRequirement


def _key(req: ExtractedRequirement) -> tuple[str, str]:
    """Deduplication key: (role_id, competency_id or empty string)."""
    return (
        req.role.role_id,
        req.competency.competency_id if req.competency else "",
    )


def _merge_requirements(
    rules_req: ExtractedRequirement,
    llm_req: ExtractedRequirement,
) -> ExtractedRequirement:
    """Keep the rules requirement as the base and enrich it from LLM.

    Merges:
      - evidence: rules evidence first, then LLM evidence not already present
      - assumptions: rules assumptions first, then LLM assumptions
      - needs_review: OR of both; LLM ambiguity flag is preserved
      - candidates: prefer LLM candidates if rules has none
    """
    merged = rules_req.model_copy()

    # Merge evidence: keep rules evidence, append unique LLM evidence by quote
    existing_quotes = {ev.quote for ev in merged.evidence}
    for ev in llm_req.evidence:
        if ev.quote not in existing_quotes:
            merged.evidence.append(ev)
            existing_quotes.add(ev.quote)
    # Clamp to schema maxItems
    if len(merged.evidence) > 10:
        merged.evidence = merged.evidence[:10]

    # Merge assumptions
    merged.assumptions = list(merged.assumptions)
    for assumption in llm_req.assumptions:
        if assumption not in merged.assumptions:
            merged.assumptions.append(assumption)
    if len(merged.assumptions) > 10:
        merged.assumptions = merged.assumptions[:10]

    # Preserve review flag from either side
    merged.needs_review = merged.needs_review or llm_req.needs_review

    # Preserve LLM candidates if rules had none
    if merged.competency and llm_req.competency:
        if not merged.competency.candidates and llm_req.competency.candidates:
            merged.competency = merged.competency.model_copy(
                update={"candidates": llm_req.competency.candidates}
            )

    return merged


class Merger:
    """Merge rules + LLM extraction results, preferring rules on conflict."""

    def merge(
        self,
        rules_reqs: list[ExtractedRequirement],
        llm_reqs: list[ExtractedRequirement],
        max_requirements: int = 30,
    ) -> list[ExtractedRequirement]:
        """Merge two lists of requirements.

        Returns a deduplicated, sorted list with at most max_requirements entries.
        When both sides produce the same (role_id, competency_id):
          - keep the rules-based requirement (higher precision)
          - enrich it with LLM evidence/assumptions/candidates
        """
        # Index rules by key for O(1) lookup
        rules_by_key: dict[tuple[str, str], ExtractedRequirement] = {}
        for req in rules_reqs:
            k = _key(req)
            # On collision within rules, keep higher confidence
            if k not in rules_by_key or req.confidence > rules_by_key[k].confidence:
                rules_by_key[k] = req

        merged: dict[tuple[str, str], ExtractedRequirement] = {}

        # Start with rules requirements
        for k, req in rules_by_key.items():
            merged[k] = req

        # Add LLM-only keys, merging when key exists
        for req in llm_reqs:
            k = _key(req)
            if k in merged:
                merged[k] = _merge_requirements(merged[k], req)
            else:
                merged[k] = req

        # Collect and sort deterministically
        result = list(merged.values())
        result.sort(key=lambda r: (r.role.role_id, r.competency.competency_id if r.competency else ""))

        # Enforce max_requirements: trim lowest confidence
        if len(result) > max_requirements:
            result.sort(key=lambda r: r.confidence)
            result = result[-max_requirements:]
            # Re-sort by key for deterministic output
            result.sort(key=lambda r: (r.role.role_id, r.competency.competency_id if r.competency else ""))

        return result
