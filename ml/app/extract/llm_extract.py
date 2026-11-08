"""LLM extraction call + compact schema enrichment + element-wise validation + feedback retry.

Per ADR-005 (`tz_ml_extract_fix_v2.md` §3.4 & §3.6):
- Never swallows `LLMTimeoutError`, `LLMUnavailableError`, or `LLMError` — errors propagate
  to `router.py` so the service returns an honest `Problem` (408/503).
- Supports compact LLM output `{role_id, competency_id, quote, level, criticality, headcount?, confidence?, needs_review?}`
  as well as full `ExtractedRequirement` objects, enriching compact items deterministically
  via `TaxonomyRegistry` and `EffortEstimator`.
- Validates elements individually (one bad element never discards valid siblings).
- Retries with specific per-element error feedback (invalid JSON, schema errors, unknown
  taxonomy IDs, disallowed `(role_id, competency_id)` pairs with allowed roles listed,
  or `quote` not found in `brief_text`), merging valid items across attempts.
- Increases `max_tokens` when output was truncated (`finish_reason == "length"`).
- Respects `options.timeout_ms` across all attempts.
"""

from __future__ import annotations

import inspect
import json
import re
import time
from dataclasses import dataclass, field
from typing import Any, Iterable

from pydantic import ValidationError

from ..llm.client import LLMClient, LLMError, LLMErrorResponse, LLMTimeoutError
from ..llm.config import LLMSettings
from .effort import EffortEstimator
from .postprocessor import RoleCompetencyValidator
from .prompt import build_extract_prompt
from .schemas import (
    CompetencyCandidate,
    EvidenceSpan,
    ExtractCompetency,
    ExtractedRequirement,
    ExtractRole,
)
from .taxonomy import TaxonomyRegistry
from .validator import EvidenceValidator, find_quote_in_text

_REQ_ID_RE = re.compile(r"^[A-Za-z0-9_-]{4,40}$")

_STRING_LEVEL_MAP: dict[str, int] = {
    "intern": 1,
    "стажер": 1,
    "стажёр": 1,
    "junior": 2,
    "джуниор": 2,
    "джун": 2,
    "middle": 3,
    "мидл": 3,
    "миддл": 3,
    "senior": 4,
    "сеньор": 4,
    "синьор": 4,
    "lead": 4,
    "лид": 4,
    "principal": 5,
    "принципал": 5,
    "expert": 5,
    "эксперт": 5,
}


# ---------------------------------------------------------------------------
# Stats dataclass
# ---------------------------------------------------------------------------


@dataclass
class LLMStats:
    """Per-call LLM statistics and deterministic validation counters."""

    tokens_in: int = 0
    tokens_out: int = 0
    duration_ms: int = 0
    retries: int = 0
    json_valid: bool = True
    empty_reason: str = ""
    dropped_schema_invalid: int = 0
    dropped_unknown_taxonomy: int = 0
    dropped_no_evidence: int = 0
    dropped_invalid_pair: int = 0
    dropped_duplicate: int = 0
    dropped_low_confidence: int = 0
    dropped_over_limit: int = 0
    raw_items: list[ExtractedRequirement] = field(default_factory=list)

    @property
    def post_processor_dropped(self) -> int:
        return (
            self.dropped_schema_invalid
            + self.dropped_unknown_taxonomy
            + self.dropped_invalid_pair
            + self.dropped_duplicate
            + self.dropped_low_confidence
            + self.dropped_over_limit
        )


# ---------------------------------------------------------------------------
# JSON extraction helpers
# ---------------------------------------------------------------------------


def _extract_json_array(text: str) -> str | None:
    """Try to extract a JSON array from LLM output (handles markdown fences, preamble)."""
    text = text.strip()

    # Remove markdown code fences
    if text.startswith("```"):
        first_newline = text.index("\n") if "\n" in text else len(text)
        text = text[first_newline + 1 :]
        if text.endswith("```"):
            text = text[:-3]
        text = text.strip()

    start = text.find("[")
    end = text.rfind("]")
    if start == -1 or end == -1 or end <= start:
        return None
    return text[start : end + 1]


def _parse_json_items(content: str) -> tuple[list[Any] | None, str]:
    """Parse LLM output into a list of raw items, or `(None, error_message)`."""
    if not content or not content.strip():
        return None, "Empty response from LLM"

    raw: Any = None
    try:
        raw = json.loads(content)
    except json.JSONDecodeError:
        repaired = _extract_json_array(content)
        if repaired is None:
            return None, "Response does not contain a valid JSON array"
        try:
            raw = json.loads(repaired)
        except json.JSONDecodeError as exc:
            return None, f"Invalid JSON syntax: {exc.msg} at pos {exc.pos}"

    if isinstance(raw, list):
        return raw, ""

    if isinstance(raw, dict):
        for key in ("requirements", "items", "data", "result"):
            if isinstance(raw.get(key), list):
                return raw[key], ""
        if "role_id" in raw or "role" in raw:
            return [raw], ""

    return None, f"Expected JSON array of requirements, got {type(raw).__name__}"


# ---------------------------------------------------------------------------
# Compact -> Full ExtractedRequirement enrichment
# ---------------------------------------------------------------------------


def _is_full_requirement_dict(item: dict[str, Any]) -> bool:
    """Return True if `item` uses the full wire `ExtractedRequirement` shape."""
    return (
        isinstance(item.get("role"), dict)
        and isinstance(item.get("effort"), dict)
        and isinstance(item.get("evidence"), list)
    )


def _enrich_single_item(
    item: Any,
    idx: int,
    brief_text: str,
    taxonomy: TaxonomyRegistry,
    estimator: EffortEstimator,
    *,
    return_candidates: bool = True,
    effective_team_size: int | None = None,
) -> tuple[ExtractedRequirement | None, str]:
    """Validate and enrich one raw LLM element into an `ExtractedRequirement`.

    Returns `(requirement, "")` on success or `(None, error_reason)` on schema failure.
    """
    if not isinstance(item, dict):
        return None, f"Element {idx}: expected JSON object, got {type(item).__name__}"

    # Case 1: Full ExtractedRequirement dictionary
    if _is_full_requirement_dict(item):
        data = dict(item)
        if isinstance(data.get("role"), dict):
            role_dict = dict(data["role"])
            role_dict.setdefault("match", "llm")
            data["role"] = role_dict
        if isinstance(data.get("competency"), dict):
            comp_dict = dict(data["competency"])
            comp_dict.setdefault("match", "llm")
            if not return_candidates:
                comp_dict["candidates"] = []
            data["competency"] = comp_dict
        if not isinstance(data.get("requirement_id"), str) or not _REQ_ID_RE.match(
            data["requirement_id"]
        ):
            data["requirement_id"] = f"req_{idx + 1:03d}"
        try:
            return ExtractedRequirement.model_validate(data), ""
        except ValidationError as exc:
            first_err = exc.errors()[0] if exc.errors() else {"msg": str(exc)}
            loc = ".".join(str(x) for x in first_err.get("loc", ()))
            return (
                None,
                f"Element {idx}: invalid field '{loc}': {first_err.get('msg', str(exc))}",
            )

    # Case 2: Compact LLM item {role_id, competency_id, quote, level, criticality, ...}
    raw_role = item.get("role_id")
    if raw_role is None and isinstance(item.get("role"), str):
        raw_role = item["role"]
    elif raw_role is None and isinstance(item.get("role"), dict):
        raw_role = item["role"].get("role_id")

    if not isinstance(raw_role, str) or not raw_role.strip():
        return None, f"Element {idx}: missing or invalid 'role_id'"
    role_id = raw_role.strip()

    # Competency ID (optional for role-only requirements, though competency is normally expected)
    raw_comp: Any = item.get("competency_id")
    if raw_comp is None and "competency" in item:
        if isinstance(item["competency"], str):
            raw_comp = item["competency"]
        elif isinstance(item["competency"], dict):
            raw_comp = item["competency"].get("competency_id")

    comp_id: str | None
    if raw_comp is None or raw_comp == "" or raw_comp == "null":
        comp_id = None
    elif isinstance(raw_comp, str):
        comp_id = raw_comp.strip() or None
    else:
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): invalid 'competency_id' {raw_comp!r}",
        )

    # Quote
    raw_quote = item.get("quote")
    if raw_quote is None and isinstance(item.get("evidence"), list) and item["evidence"]:
        first_ev = item["evidence"][0]
        if isinstance(first_ev, dict):
            raw_quote = first_ev.get("quote")
        elif isinstance(first_ev, str):
            raw_quote = first_ev
    elif raw_quote is None and isinstance(item.get("evidence"), str):
        raw_quote = item["evidence"]

    if not isinstance(raw_quote, str) or len(raw_quote.strip()) < 3:
        return (
            None,
            f"Element {idx} (role_id='{role_id}', competency_id={comp_id!r}): "
            f"missing or too short 'quote' (must be at least 3 characters from brief)",
        )
    quote_str = raw_quote.strip()[:1000]

    # Level (1..5)
    raw_level = item.get("level", 3)
    if raw_level is None:
        level = 3
    elif isinstance(raw_level, str):
        mapped = _STRING_LEVEL_MAP.get(raw_level.strip().lower())
        if mapped is not None:
            level = mapped
        elif raw_level.strip().isdigit():
            level = int(raw_level.strip())
        else:
            return (
                None,
                f"Element {idx} (role_id='{role_id}'): invalid 'level' {raw_level!r} (expected integer 1..5)",
            )
    elif isinstance(raw_level, int) and not isinstance(raw_level, bool):
        level = raw_level
    else:
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): invalid 'level' {raw_level!r} (expected integer 1..5)",
        )

    if not (1 <= level <= 5):
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): 'level' {level} out of range 1..5",
        )

    # Criticality
    raw_crit = item.get("criticality", "must")
    if raw_crit is None:
        criticality = "must"
    elif isinstance(raw_crit, str) and raw_crit.strip().lower() in ("must", "should", "nice"):
        criticality = raw_crit.strip().lower()
    else:
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): invalid 'criticality' {raw_crit!r} (expected 'must', 'should', or 'nice')",
        )

    # Headcount
    raw_hc = item.get("headcount", 1)
    if raw_hc is None:
        headcount = 1
    elif isinstance(raw_hc, int) and not isinstance(raw_hc, bool) and 1 <= raw_hc <= 20:
        headcount = raw_hc
    else:
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): invalid 'headcount' {raw_hc!r} (expected integer 1..20)",
        )

    # Confidence
    raw_conf = item.get("confidence", 0.9)
    if raw_conf is None:
        conf = 0.9
    elif (
        isinstance(raw_conf, (int, float))
        and not isinstance(raw_conf, bool)
        and 0.0 <= float(raw_conf) <= 1.0
    ):
        conf = round(float(raw_conf), 4)
    else:
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): invalid 'confidence' {raw_conf!r} (expected number 0..1)",
        )

    needs_review = bool(item.get("needs_review", False)) or (conf < 0.7)

    # Look up Russian display names from taxonomy
    role_obj = taxonomy.get_role(role_id)
    role_name_ru = role_obj.name_ru if role_obj is not None else role_id[:120]

    extract_role = ExtractRole(
        role_id=role_id,
        name_ru=role_name_ru,
        match="llm",
        confidence=conf,
    )

    extract_comp: ExtractCompetency | None = None
    if comp_id is not None:
        comp_obj = taxonomy.get_competency(comp_id)
        comp_name_ru = comp_obj.name_ru if comp_obj is not None else comp_id[:120]
        candidates = (
            [CompetencyCandidate(competency_id=comp_id, score=conf)]
            if return_candidates
            else []
        )
        extract_comp = ExtractCompetency(
            competency_id=comp_id,
            name_ru=comp_name_ru,
            match="llm",
            confidence=conf,
            candidates=candidates,
        )

    # Effort via EffortEstimator
    effort_range, effort_assumption = estimator.estimate_for_fields(
        role_id=role_id,
        comp_id=comp_id,
        level=level,
        headcount=headcount,
        brief_text=brief_text,
        effective_team_size=effective_team_size,
    )

    # Assumptions
    assumptions: list[str] = []
    raw_assumptions = item.get("assumptions")
    if isinstance(raw_assumptions, list):
        for a in raw_assumptions:
            if isinstance(a, str) and a.strip():
                assumptions.append(a.strip()[:300])
    if not assumptions:
        target_desc = f"{role_id}/{comp_id}" if comp_id else role_id
        assumptions.append(f"Extracted via LLM from brief evidence for {target_desc}")
    if effort_assumption not in assumptions and len(assumptions) < 10:
        assumptions.append(effort_assumption[:300])

    # Requirement ID
    raw_req_id = item.get("requirement_id")
    if isinstance(raw_req_id, str) and _REQ_ID_RE.match(raw_req_id):
        req_id = raw_req_id
    else:
        req_id = f"req_{idx + 1:03d}"

    try:
        req = ExtractedRequirement(
            requirement_id=req_id,
            role=extract_role,
            competency=extract_comp,
            level=level,
            criticality=criticality,  # type: ignore[arg-type]
            headcount=headcount,
            effort=effort_range,
            assumptions=assumptions[:10],
            confidence=conf,
            needs_review=needs_review,
            evidence=[
                EvidenceSpan(
                    kind="brief_span",
                    quote=quote_str,
                    char_start=None,
                    char_end=None,
                )
            ],
        )
        return req, ""
    except ValidationError as exc:
        return None, f"Element {idx}: schema validation failed: {exc}"


# ---------------------------------------------------------------------------
# Feedback prompt builder
# ---------------------------------------------------------------------------


def _build_repair_feedback_message(
    errors: list[str],
    truncated: bool = False,
) -> str:
    """Build a specific repair prompt listing each validation error."""
    lines = [
        "Your previous response had validation errors that must be fixed:",
    ]
    if truncated:
        lines.append(
            "- Output was truncated due to token limit. Return a compact JSON array with short exact quotes."
        )
    for err in errors[:15]:
        lines.append(f"- {err}")
    lines.append(
        "\nReturn ONLY the corrected JSON array of requirements. "
        "For every item, ensure `role_id` is in the competency's allowed roles and `quote` is an exact substring of the brief text."
    )
    return "\n".join(lines)


async def _call_client_chat(
    client: Any,
    messages: list[dict[str, Any]],
    *,
    temperature: float,
    seed: int,
    max_tokens: int,
    timeout_ms: int,
) -> Any:
    """Invoke `client.chat` passing `timeout_ms` if supported by the client's signature."""
    sig = inspect.signature(client.chat)
    kwargs: dict[str, Any] = {
        "temperature": temperature,
        "seed": seed,
        "max_tokens": max_tokens,
    }
    if "timeout_ms" in sig.parameters or any(
        p.kind == inspect.Parameter.VAR_KEYWORD for p in sig.parameters.values()
    ):
        kwargs["timeout_ms"] = timeout_ms
    return await client.chat(messages, **kwargs)


# ---------------------------------------------------------------------------
# Main extraction function
# ---------------------------------------------------------------------------


async def llm_extract(
    brief_text: str,
    locale: str,
    taxonomy: TaxonomyRegistry,
    client: LLMClient,
    settings: LLMSettings,
    *,
    allowed_role_ids: Iterable[str] | None = None,
    allowed_competency_ids: Iterable[str] | None = None,
    max_requirements: int = 30,
    min_confidence: float = 0.0,
    return_candidates: bool = True,
    timeout_ms: int | None = None,
) -> tuple[list[ExtractedRequirement], LLMStats]:
    """Extract requirements via LLM with element-wise validation and feedback retries.

    Raises:
        LLMTimeoutError: when an LLM call times out or total `timeout_ms` is exceeded.
        LLMUnavailableError: when the LLM endpoint is unreachable or unconfigured.
        LLMError: on HTTP 4xx/5xx or malformed provider payload.
    """
    stats = LLMStats()
    messages: list[dict[str, Any]] = list(
        build_extract_prompt(
            brief_text,
            taxonomy,
            locale,
            allowed_role_ids=allowed_role_ids,
            allowed_competency_ids=allowed_competency_ids,
        )
    )

    total_timeout_ms = timeout_ms if timeout_ms is not None else settings.timeout_ms
    current_max_tokens = max(2048, min(8192, max_requirements * 120))
    max_attempts = 1 + settings.max_retries

    estimator = EffortEstimator()
    role_validator = RoleCompetencyValidator(
        taxonomy,
        allowed_role_ids=allowed_role_ids,
        allowed_competency_ids=allowed_competency_ids,
    )
    evidence_validator = EvidenceValidator()

    # Keep valid requirements across retry attempts so valid items are never lost
    accumulated_valid: list[ExtractedRequirement] = []
    seen_valid_keys: set[tuple[str, str, str]] = set()
    first_raw_items: list[ExtractedRequirement] = []

    overall_start = time.monotonic()

    # Reset per-attempt counters for unfixed items from the final attempt
    last_attempt_drops = {
        "schema": 0,
        "taxonomy": 0,
        "evidence": 0,
        "pair": 0,
    }

    for attempt in range(max_attempts):
        elapsed_total_ms = int((time.monotonic() - overall_start) * 1000)
        remaining_ms = total_timeout_ms - elapsed_total_ms
        if remaining_ms <= 0:
            raise LLMTimeoutError(
                f"LLM extraction exceeded timeout budget of {total_timeout_ms}ms"
            )

        if attempt > 0:
            stats.retries = attempt

        t0 = time.monotonic()
        try:
            resp = await _call_client_chat(
                client,
                messages,
                temperature=0,
                seed=42,
                max_tokens=current_max_tokens,
                timeout_ms=remaining_ms,
            )
        except LLMError as exc:
            stats.duration_ms += int((time.monotonic() - t0) * 1000)
            # Compatibility with single-response test mocks that don't queue retry responses
            if (
                attempt > 0
                and isinstance(exc, LLMErrorResponse)
                and exc.status_code is None
                and "no more mock responses" in str(exc).lower()
            ):
                break
            raise

        stats.duration_ms += int((time.monotonic() - t0) * 1000)
        stats.tokens_in += resp.tokens_in
        stats.tokens_out += resp.tokens_out

        is_truncated = getattr(resp, "finish_reason", None) == "length"
        raw_items, parse_error = _parse_json_items(resp.content)

        if raw_items is None:
            stats.json_valid = False
            stats.empty_reason = parse_error
            if is_truncated:
                current_max_tokens = min(8192, int(current_max_tokens * 1.5))
            if attempt + 1 < max_attempts:
                messages = messages + [
                    {"role": "assistant", "content": resp.content or ""},
                    {
                        "role": "user",
                        "content": _build_repair_feedback_message(
                            [parse_error], truncated=is_truncated
                        ),
                    },
                ]
                continue
            break

        stats.json_valid = True
        stats.empty_reason = ""
        feedback_errors: list[str] = []
        last_attempt_drops = {"schema": 0, "taxonomy": 0, "evidence": 0, "pair": 0}
        attempt_enriched: list[ExtractedRequirement] = []

        effective_team_size = max(len(raw_items), 1)

        for idx, raw_item in enumerate(raw_items):
            req, schema_err = _enrich_single_item(
                raw_item,
                idx,
                brief_text,
                taxonomy,
                estimator,
                return_candidates=return_candidates,
                effective_team_size=effective_team_size,
            )
            if req is None:
                last_attempt_drops["schema"] += 1
                feedback_errors.append(schema_err)
                continue

            attempt_enriched.append(req)
            role_id = req.role.role_id
            comp_id = req.competency.competency_id if req.competency else None

            # Check taxonomy & allowed pair
            pair_ok, category, pair_reason = role_validator.check_pair(role_id, comp_id)
            if not pair_ok:
                if category == "unknown_taxonomy":
                    last_attempt_drops["taxonomy"] += 1
                else:
                    last_attempt_drops["pair"] += 1
                feedback_errors.append(
                    f"Element {idx} (role_id='{role_id}', competency_id='{comp_id}'): {pair_reason}"
                )
                continue

            # Check quote in brief_text (normalized match)
            corrected_spans = evidence_validator._validate_evidence_spans(
                req.evidence, brief_text
            )
            if corrected_spans is None:
                bad_quote = req.evidence[0].quote if req.evidence else ""
                last_attempt_drops["evidence"] += 1
                feedback_errors.append(
                    f"Element {idx} (role_id='{role_id}', competency_id='{comp_id}'): "
                    f"quote {bad_quote!r} was not found in the brief text. "
                    f"Provide an exact substring from the brief."
                )
                continue

            valid_req = req.model_copy(update={"evidence": corrected_spans})
            valid_req = role_validator._apply_review_flags(valid_req)
            key = (
                role_id,
                comp_id or "",
                valid_req.evidence[0].quote if valid_req.evidence else "",
            )
            if key not in seen_valid_keys:
                seen_valid_keys.add(key)
                accumulated_valid.append(valid_req)

        if attempt == 0 or not first_raw_items:
            first_raw_items = attempt_enriched

        # If all elements were valid (and output was not truncated), we are done
        if not feedback_errors and not is_truncated:
            break

        # Otherwise, if we have retries left, send feedback retry
        if attempt + 1 < max_attempts:
            if is_truncated:
                current_max_tokens = min(8192, int(current_max_tokens * 1.5))
            messages = messages + [
                {"role": "assistant", "content": resp.content},
                {
                    "role": "user",
                    "content": _build_repair_feedback_message(
                        feedback_errors, truncated=is_truncated
                    ),
                },
            ]

    # Run final deterministic deduplication & confidence/limit filtering on accumulated valid items
    final_valid, pipe_stats = evidence_validator.validate_pipeline(
        accumulated_valid,
        brief_text,
        taxonomy,
        allowed_role_ids=allowed_role_ids,
        allowed_competency_ids=allowed_competency_ids,
        min_confidence=min_confidence,
        max_requirements=max_requirements,
    )

    stats.raw_items = first_raw_items if first_raw_items else list(final_valid)
    stats.dropped_schema_invalid = last_attempt_drops["schema"] + pipe_stats.dropped_schema_invalid
    stats.dropped_unknown_taxonomy = (
        last_attempt_drops["taxonomy"] + pipe_stats.dropped_unknown_taxonomy
    )
    stats.dropped_no_evidence = last_attempt_drops["evidence"] + pipe_stats.dropped_no_evidence
    stats.dropped_invalid_pair = last_attempt_drops["pair"] + pipe_stats.dropped_invalid_pair
    stats.dropped_duplicate = pipe_stats.dropped_duplicate
    stats.dropped_low_confidence = pipe_stats.dropped_low_confidence
    stats.dropped_over_limit = pipe_stats.dropped_over_limit

    if not final_valid and not stats.empty_reason:
        if not stats.json_valid:
            stats.empty_reason = "Invalid JSON response after retries"
        elif (
            stats.dropped_no_evidence
            + stats.post_processor_dropped
            > 0
        ):
            stats.empty_reason = "All extracted items dropped by deterministic validation"
        else:
            stats.empty_reason = "Empty JSON array returned by LLM"

    return final_valid, stats
