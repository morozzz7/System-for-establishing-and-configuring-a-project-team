"""Deterministic result validator and evidence hallucination gate (3.4 & 3.6).

Hard rule: every requirement's `evidence[].quote` MUST be a literal substring of
`brief.text`. Quotes are matched with character/whitespace normalization
(multiple spaces, non-breaking spaces, `ё/е`, dash variants, quotation marks),
and replaced with the exact original slice `brief.text[char_start:char_end]`.

Also implements the ordered 6-stage deterministic validation pipeline (3.6):
  1. Element-wise schema check -> `dropped_schema_invalid`
  2. Taxonomy existence & subset check -> `dropped_unknown_taxonomy`
  3. Quote in `brief_text` (normalized match + offset recovery) -> `dropped_no_evidence`
  4. Role allowed for competency (`role_id in competency.roles`) -> `dropped_invalid_pair`
  5. Deduplication (`(role_id, competency_id)` unique; same `competency_id` across
     different roles requires distinct `quote`) -> `dropped_duplicate`
  6. Confidence threshold (`min_confidence`) & `max_requirements` cap -> `dropped_over_limit`
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any, Iterable

from pydantic import ValidationError

from .postprocessor import RoleCompetencyValidator
from .schemas import EvidenceSpan, ExtractedRequirement
from .taxonomy import TaxonomyRegistry

# ---------------------------------------------------------------------------
# Character normalization tables for quote matching
# ---------------------------------------------------------------------------

_DASH_CHARS = frozenset(
    "\u2010\u2011\u2012\u2013\u2014\u2015\u2212\ufe58\ufe63\uff0d"
)
_QUOTE_CHARS = frozenset("«»“”„‟'’‘‚‹›")
_SPACE_CHARS = frozenset(" \t\n\r\x0b\x0c\u00a0\u202f\u2007\u2009\u200a")


def _norm_char(ch: str) -> str:
    """Normalize a single non-whitespace character."""
    if ch == "ё":
        return "е"
    if ch == "Ё":
        return "Е"
    if ch in _DASH_CHARS:
        return "-"
    if ch in _QUOTE_CHARS:
        return '"'
    return ch


def _build_normalized_index(text: str) -> tuple[str, list[int], list[int]]:
    """Normalize text and return `(norm_text, orig_starts, orig_ends)`.

    Each character `k` in `norm_text` corresponds to `text[orig_starts[k]:orig_ends[k]]`.
    Runs of whitespace (including `\\u00a0`, `\\u202f`, newlines, tabs) collapse to
    a single `' '`.
    """
    norm_chars: list[str] = []
    orig_starts: list[int] = []
    orig_ends: list[int] = []

    for i, ch in enumerate(text):
        if ch in _SPACE_CHARS or ch.isspace():
            if norm_chars and norm_chars[-1] == " ":
                orig_ends[-1] = i + 1
            else:
                norm_chars.append(" ")
                orig_starts.append(i)
                orig_ends.append(i + 1)
        else:
            norm_chars.append(_norm_char(ch))
            orig_starts.append(i)
            orig_ends.append(i + 1)

    return "".join(norm_chars), orig_starts, orig_ends


def _safe_lower(s: str) -> str:
    """Case-fold string while strictly preserving 1-to-1 character length."""
    return "".join(ch.lower() if len(ch.lower()) == 1 else ch for ch in s)


def find_quote_in_text(quote: str, brief_text: str) -> tuple[str, int, int] | None:
    """Locate `quote` inside `brief_text` using exact or normalized matching.

    Returns `(exact_original_substring, char_start, char_end)` where
    `brief_text[char_start:char_end] == exact_original_substring`, or `None`
    if not found (or if the matched span is shorter than 3 characters).
    """
    if not quote or not brief_text:
        return None

    # 1. Fast path: literal substring match
    idx = brief_text.find(quote)
    if idx != -1:
        if len(quote) < 3:
            return None
        return quote, idx, idx + len(quote)

    # Trim surrounding whitespace on candidate quote
    trimmed = quote.strip()
    if not trimmed:
        return None
    idx = brief_text.find(trimmed)
    if idx != -1:
        if len(trimmed) < 3:
            return None
        return trimmed, idx, idx + len(trimmed)

    # 2. Normalized match with index mapping back to brief_text
    norm_quote, _, _ = _build_normalized_index(trimmed)
    norm_quote = norm_quote.strip()
    if len(norm_quote) < 3:
        return None

    norm_text, orig_starts, orig_ends = _build_normalized_index(brief_text)
    if not norm_text:
        return None

    n_idx = norm_text.find(norm_quote)
    if n_idx == -1:
        # Case-insensitive fallback on normalized strings (1-to-1 length preserved)
        n_idx = _safe_lower(norm_text).find(_safe_lower(norm_quote))
    if n_idx == -1:
        return None

    end_norm_idx = n_idx + len(norm_quote) - 1
    if end_norm_idx >= len(orig_ends):
        return None

    orig_start = orig_starts[n_idx]
    orig_end = orig_ends[end_norm_idx]
    orig_slice = brief_text[orig_start:orig_end]
    if len(orig_slice) < 3 or len(orig_slice) > 1000:
        return None
    return orig_slice, orig_start, orig_end


# ---------------------------------------------------------------------------
# Diagnostics counters for the 6 validation stages (3.6)
# ---------------------------------------------------------------------------


@dataclass
class ValidationStats:
    """Per-stage drop counters for the 6 deterministic validation checks."""

    dropped_schema_invalid: int = 0
    dropped_unknown_taxonomy: int = 0
    dropped_no_evidence: int = 0
    dropped_invalid_pair: int = 0
    dropped_duplicate: int = 0
    dropped_low_confidence: int = 0
    dropped_over_limit: int = 0

    @property
    def post_processor_dropped(self) -> int:
        """Total dropped by taxonomy/pair/dedup/limit checks (outside evidence)."""
        return (
            self.dropped_schema_invalid
            + self.dropped_unknown_taxonomy
            + self.dropped_invalid_pair
            + self.dropped_duplicate
            + self.dropped_low_confidence
            + self.dropped_over_limit
        )


# ---------------------------------------------------------------------------
# Validator
# ---------------------------------------------------------------------------


class EvidenceValidator:
    """Validate evidence quotes and run the 6-stage deterministic validation gate."""

    def validate(
        self,
        requirements: list[ExtractedRequirement],
        brief_text: str,
    ) -> tuple[list[ExtractedRequirement], int]:
        """Validate evidence spans and deduplicate by `requirement_id` and `(role_id, competency_id)`.

        Returns:
            `(valid_requirements, dropped_count)` where `dropped_count` is the
            number of requirements removed due to invalid evidence or duplicate IDs.
        """
        valid: list[ExtractedRequirement] = []
        dropped = 0
        seen_ids: set[str] = set()

        for req in requirements:
            if req.requirement_id in seen_ids:
                dropped += 1
                continue

            corrected_evidence = self._validate_evidence_spans(req.evidence, brief_text)
            if corrected_evidence is None:
                dropped += 1
                continue

            validated_req = req.model_copy(update={"evidence": corrected_evidence})
            valid.append(validated_req)
            seen_ids.add(req.requirement_id)

        return valid, dropped

    def _validate_evidence_spans(
        self,
        spans: list[EvidenceSpan],
        brief_text: str,
    ) -> list[EvidenceSpan] | None:
        """Validate and normalize all evidence spans for a requirement.

        Returns corrected `EvidenceSpan` list with literal substrings of
        `brief_text` and exact `char_start`/`char_end` offsets, or `None` if any
        span fails to match `brief_text`.
        """
        if not spans:
            return None

        corrected: list[EvidenceSpan] = []
        for ev in spans:
            found = find_quote_in_text(ev.quote, brief_text)
            if found is None:
                return None
            orig_quote, actual_start, actual_end = found
            corrected.append(
                EvidenceSpan(
                    kind=ev.kind,
                    char_start=actual_start,
                    char_end=actual_end,
                    quote=orig_quote,
                )
            )
        return corrected

    def validate_pipeline(
        self,
        items: list[ExtractedRequirement | dict[str, Any]],
        brief_text: str,
        taxonomy: TaxonomyRegistry,
        *,
        allowed_role_ids: Iterable[str] | None = None,
        allowed_competency_ids: Iterable[str] | None = None,
        min_confidence: float = 0.0,
        max_requirements: int = 30,
    ) -> tuple[list[ExtractedRequirement], ValidationStats]:
        """Execute all 6 deterministic validation checks in strict order (3.6):

        1. Schema check (element-wise Pydantic validation) -> `dropped_schema_invalid`
        2. Taxonomy existence & request subset check -> `dropped_unknown_taxonomy`
        3. Quote found in `brief_text` (normalized match + exact slice/offsets) -> `dropped_no_evidence`
        4. Role allowed for competency (`role_id in competency.roles`) -> `dropped_invalid_pair`
        5. Deduplication:
           - `(role_id, competency_id)` never repeats;
           - same `competency_id` with different roles is allowed ONLY if each
             role has a distinct `quote` -> `dropped_duplicate`
        6. `min_confidence` filter & `max_requirements` cap (sorted by confidence
           descending, deterministic) -> `dropped_over_limit`
        """
        stats = ValidationStats()
        role_validator = RoleCompetencyValidator(
            taxonomy,
            allowed_role_ids=allowed_role_ids,
            allowed_competency_ids=allowed_competency_ids,
        )

        # Stage 1: Schema check (element-by-element)
        schema_valid: list[ExtractedRequirement] = []
        for item in items:
            if isinstance(item, ExtractedRequirement):
                schema_valid.append(item)
            elif isinstance(item, dict):
                try:
                    schema_valid.append(ExtractedRequirement.model_validate(item))
                except ValidationError:
                    stats.dropped_schema_invalid += 1
            else:
                stats.dropped_schema_invalid += 1

        # Stage 2, 3, 4: Taxonomy check, Evidence quote check, Allowed role-competency pair check
        stage4_valid: list[ExtractedRequirement] = []
        for req in schema_valid:
            role_id = req.role.role_id
            comp_id = req.competency.competency_id if req.competency else None

            # Check 2 (taxonomy existence & subset) and Check 4 (role in comp.roles)
            ok, category, _reason = role_validator.check_pair(role_id, comp_id)
            if not ok and category == "unknown_taxonomy":
                stats.dropped_unknown_taxonomy += 1
                continue

            # Check 3: Quote found in brief_text (normalized match)
            corrected_evidence = self._validate_evidence_spans(req.evidence, brief_text)
            if corrected_evidence is None:
                stats.dropped_no_evidence += 1
                continue

            # Check 4: Role allowed for competency (role_id in competency.roles)
            if not ok and category == "invalid_pair":
                stats.dropped_invalid_pair += 1
                continue

            updated_req = req.model_copy(update={"evidence": corrected_evidence})
            if updated_req.competency is None:
                updated_req = updated_req.model_copy(update={"needs_review": True})
            updated_req = role_validator._apply_review_flags(updated_req)
            stage4_valid.append(updated_req)

        # Stage 5: Deduplication
        # - Same (role_id, competency_id) is never repeated.
        # - Same competency_id with different roles is allowed ONLY if each role has its own distinct quote.
        deduped: list[ExtractedRequirement] = []
        seen_pairs: set[tuple[str, str]] = set()
        seen_comp_quotes: dict[str, set[str]] = {}
        seen_req_ids: set[str] = set()

        for idx, req in enumerate(stage4_valid):
            role_id = req.role.role_id
            comp_id = req.competency.competency_id if req.competency else ""
            pair_key = (role_id, comp_id)

            if pair_key in seen_pairs:
                stats.dropped_duplicate += 1
                continue

            primary_quote = req.evidence[0].quote.strip() if req.evidence else ""
            if comp_id:
                used_quotes_for_comp = seen_comp_quotes.setdefault(comp_id, set())
                if primary_quote in used_quotes_for_comp:
                    stats.dropped_duplicate += 1
                    continue
                used_quotes_for_comp.add(primary_quote)

            seen_pairs.add(pair_key)

            # Ensure requirement_id is unique across distinct kept items
            req_id = req.requirement_id
            if req_id in seen_req_ids:
                req_id = f"req_{idx + 1:03d}"
                while req_id in seen_req_ids:
                    idx += 1
                    req_id = f"req_{idx + 1:03d}"
                req = req.model_copy(update={"requirement_id": req_id})
            seen_req_ids.add(req.requirement_id)
            deduped.append(req)

        # Stage 6: Confidence filter & max_requirements cap
        conf_filtered: list[ExtractedRequirement] = []
        for req in deduped:
            if req.confidence < min_confidence:
                stats.dropped_low_confidence += 1
            else:
                conf_filtered.append(req)

        if len(conf_filtered) > max_requirements:
            stats.dropped_over_limit += len(conf_filtered) - max_requirements
            # Keep top max_requirements by confidence descending (tie-break by original order)
            indexed = list(enumerate(conf_filtered))
            indexed.sort(key=lambda pair: (-pair[1].confidence, pair[0]))
            kept_indices = sorted(i for i, _ in indexed[:max_requirements])
            conf_filtered = [conf_filtered[i] for i in kept_indices]

        return conf_filtered, stats

    def validate_partial_quotes(
        self,
        quotes: list[str],
        brief_text: str,
    ) -> list[bool]:
        """Check each quote independently (with normalization). Returns list of booleans."""
        return [find_quote_in_text(q, brief_text) is not None for q in quotes]
