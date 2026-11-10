"""LLM extraction call + parallel ensemble union + candidate verification + role safety net.

Per ADR-005 & reliability enhancements:
1. Parallel Ensemble Union (`ensemble_runs=3` with seeds 42, 43, 44):
   - Merges extracted requirements across runs by `(role_id, competency_id)` via UNION
     (never drops a requirement found in only 1 of 3 runs; instead lowers confidence
     and sets `needs_review=True`).
   - Supports `competency_id: null` (`competency=None, needs_review=True`) for roles
     mentioned in the brief without a specific technology.
2. Code-Side Role Headcount Normalization (`normalize_headcounts_by_role`):
   - Normalizes `headcount` per `role_id` in code as the maximum across all explicit
     mentions of that role in `brief_text`, rather than trusting the LLM.
3. Non-Destructive Lexical Competency Anchor Check:
   - In `EvidenceValidator.validate_pipeline`, if neither the competency name nor any
     of its synonyms appear near `quote`, the item is NOT deleted; its confidence is
     lowered and `needs_review=True` is set (`flagged_no_lexical_anchor`).
4. Word-Boundary Candidate Competency Scanner + Verification Call with Full Audit Log:
   - Tracks every candidate competency and missing role as `confirmed`, `rejected`, or
     `skipped` with an explicit human-readable `reason` (`verification_log`), guaranteeing
     `found == confirmed + rejected + skipped`.
"""

from __future__ import annotations

import asyncio
import inspect
import json
import re
import time
from dataclasses import dataclass, field
from typing import Any, Iterable

from pydantic import ValidationError

from ..llm.client import (
    LLMClient,
    LLMError,
    LLMErrorResponse,
    LLMTimeoutError,
    LLMUnavailableError,
)
from ..llm.config import LLMSettings
from .candidates import (
    build_verification_prompt,
    detect_headcount_for_role_near_quote,
    normalize_headcounts_by_role,
    scan_missing_roles,
    scan_unmatched_competencies,
)
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
    """Per-call LLM statistics, validation counters, and verification audit log."""

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
    flagged_no_lexical_anchor: int = 0
    dropped_generic_mention: int = 0
    # (requirement_id, quote) pairs dropped by the deterministic generic-mention
    # gate, propagated to the router so the live response can surface them in
    # `warnings` for explainability.
    dropped_generic_ids: list[tuple[str, str]] = field(default_factory=list)
    ensemble_runs_completed: int = 1
    candidate_competencies_found: int = 0
    candidate_competencies_confirmed: int = 0
    candidate_competencies_rejected: int = 0
    candidate_competencies_skipped: int = 0
    missing_roles_found: int = 0
    missing_roles_recovered: int = 0
    missing_roles_rejected: int = 0
    missing_roles_skipped: int = 0
    verification_attempts: int = 0
    verification_failed_batches: int = 0
    unmatched_candidate_ids: list[str] = field(default_factory=list)
    verification_log: list[dict[str, str]] = field(default_factory=list)
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
    """Validate and enrich one raw LLM element into an `ExtractedRequirement`."""
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

    raw_comp: Any = item.get("competency_id")
    if raw_comp is None and "competency" in item:
        if isinstance(item["competency"], str):
            raw_comp = item["competency"]
        elif isinstance(item["competency"], dict):
            raw_comp = item["competency"].get("competency_id")

    comp_id: str | None
    if raw_comp is None or raw_comp == "" or raw_comp == "null" or raw_comp == "none":
        comp_id = None
    elif isinstance(raw_comp, str):
        cleaned_c = raw_comp.strip()
        comp_id = None if cleaned_c.lower() in ("", "null", "none") else cleaned_c
    else:
        return (
            None,
            f"Element {idx} (role_id='{role_id}'): invalid 'competency_id' {raw_comp!r}",
        )

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

    needs_review = bool(item.get("needs_review", False)) or (conf < 0.7) or (comp_id is None)

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

    effort_range, effort_assumption = estimator.estimate_for_fields(
        role_id=role_id,
        comp_id=comp_id,
        level=level,
        headcount=headcount,
        brief_text=brief_text,
        effective_team_size=effective_team_size,
    )

    assumptions: list[str] = []
    raw_assumptions = item.get("assumptions")
    if isinstance(raw_assumptions, list):
        for a in raw_assumptions:
            if isinstance(a, str) and a.strip():
                assumptions.append(a.strip()[:300])
    if not assumptions:
        target_desc = f"{role_id}/{comp_id}" if comp_id else f"{role_id} (role-only)"
        assumptions.append(f"Extracted via LLM from brief evidence for {target_desc}")
    if effort_assumption not in assumptions and len(assumptions) < 10:
        assumptions.append(effort_assumption[:300])

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
        "For every item, ensure `role_id` is in the competency's allowed roles (or `competency_id: null` for role-only) and `quote` is an exact substring of the brief text."
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
# Candidate verification batching helpers (ADR-006 §1)
# ---------------------------------------------------------------------------

_VERIFICATION_BATCH_SIZE = 8
_VERIFICATION_MIN_TOKENS = 1024
_VERIFICATION_MAX_TOKENS_CAP = 8192


def _verification_budget(n_items: int) -> int:
    """Per-batch token budget: `min(8192, max(1024, 256 + 192 * n_items))`."""
    return min(
        _VERIFICATION_MAX_TOKENS_CAP,
        max(_VERIFICATION_MIN_TOKENS, 256 + 192 * n_items),
    )


def _verification_nudge_message(parse_error: str, n_items: int) -> str:
    """Retry nudge demanding ONLY the JSON array with one explicit decision per candidate."""
    return (
        f"Your previous response could not be parsed as JSON ({parse_error}). "
        f"Return ONLY a JSON array with exactly one object per candidate ({n_items} items): "
        "no prose, no markdown fences, no explanations outside the array. "
        'Every object MUST contain an explicit "decision" field ("confirm" or "reject").'
    )


def _is_transient_verification_error(exc: LLMError) -> bool:
    """Return True for verification errors worth retrying within the batch budget.

    Transient: HTTP 5xx, a response with no status code (malformed body), and timeouts.
    Not transient: `LLMUnavailableError` (no API key / connection refused) - it cannot
    heal inside one request and retrying only burns the remaining time budget.
    """
    if isinstance(exc, LLMUnavailableError):
        return False
    if isinstance(exc, LLMTimeoutError):
        return True
    if isinstance(exc, LLMErrorResponse):
        return exc.status_code is None or exc.status_code >= 500
    return False


def _batch_candidates(
    competency_ids: list[str],
    role_ids: list[str],
) -> list[list[tuple[str, str]]]:
    """Split competency + missing-role candidates into batches of at most 8 items.

    Returns a list of batches; each batch is a list of `(kind, id)` tuples where
    `kind` is `"competency"` or `"role"`.
    """
    items: list[tuple[str, str]] = [("competency", cid) for cid in competency_ids]
    items.extend(("role", rid) for rid in role_ids)
    return [
        items[i : i + _VERIFICATION_BATCH_SIZE]
        for i in range(0, len(items), _VERIFICATION_BATCH_SIZE)
    ]


# ---------------------------------------------------------------------------
# Single pass (with repair retries)
# ---------------------------------------------------------------------------


async def _run_single_extraction_pass(
    brief_text: str,
    base_messages: list[dict[str, Any]],
    taxonomy: TaxonomyRegistry,
    client: Any,
    settings: LLMSettings,
    *,
    seed: int,
    per_attempt_timeout_ms: int,
    initial_max_tokens: int,
    allowed_role_ids: Iterable[str] | None,
    allowed_competency_ids: Iterable[str] | None,
    return_candidates: bool,
) -> tuple[list[ExtractedRequirement], list[ExtractedRequirement], LLMStats]:
    """Run one LLM extraction pass (with feedback retries on invalid items)."""
    stats = LLMStats()
    messages = list(base_messages)
    current_max_tokens = initial_max_tokens
    max_attempts = 1 + settings.max_retries

    estimator = EffortEstimator()
    role_validator = RoleCompetencyValidator(
        taxonomy,
        allowed_role_ids=allowed_role_ids,
        allowed_competency_ids=allowed_competency_ids,
    )
    evidence_validator = EvidenceValidator()

    accumulated_valid: list[ExtractedRequirement] = []
    seen_valid_keys: set[tuple[str, str, str]] = set()
    first_raw_items: list[ExtractedRequirement] = []

    last_attempt_drops = {
        "schema": 0,
        "taxonomy": 0,
        "evidence": 0,
        "pair": 0,
    }

    for attempt in range(max_attempts):
        if attempt > 0:
            stats.retries = attempt

        t0 = time.monotonic()
        try:
            resp = await _call_client_chat(
                client,
                messages,
                temperature=0,
                seed=seed,
                max_tokens=current_max_tokens,
                timeout_ms=per_attempt_timeout_ms,
            )
        except LLMError as exc:
            stats.duration_ms += int((time.monotonic() - t0) * 1000)
            if (
                attempt > 0
                and isinstance(exc, LLMErrorResponse)
                and exc.status_code is None
                and "no more mock responses" in str(exc).lower()
            ):
                break
            if (
                isinstance(exc, LLMErrorResponse)
                and (exc.status_code is None or exc.status_code >= 500)
                and attempt + 1 < max_attempts
            ):
                stats.retries += 1
                messages = messages + [
                    {
                        "role": "user",
                        "content": "The previous call failed with a transient upstream error. Please retry and return ONLY the JSON array.",
                    }
                ]
                continue
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

        if not feedback_errors and not is_truncated:
            break

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

    stats.dropped_schema_invalid = last_attempt_drops["schema"]
    stats.dropped_unknown_taxonomy = last_attempt_drops["taxonomy"]
    stats.dropped_no_evidence = last_attempt_drops["evidence"]
    stats.dropped_invalid_pair = last_attempt_drops["pair"]
    return accumulated_valid, first_raw_items, stats


# ---------------------------------------------------------------------------
# Ensemble Union + Headcount Reconciliation (Mechanisms 1 & 4)
# ---------------------------------------------------------------------------


def _reconcile_ensemble_union(
    runs_valid: list[list[ExtractedRequirement]],
    brief_text: str,
) -> list[ExtractedRequirement]:
    """Merge valid requirements across `K` parallel runs by `(role_id, competency_id)` via UNION.

    - Never drops a requirement found in only 1 of K runs; lowers confidence to <=0.58
      (>=0.56 so default min_confidence=0.55 preserves it) and sets `needs_review=True`.
    - Normalizes `headcount` by role in code (maximum among role mentions in `brief_text`).
    """
    num_runs = len(runs_valid)
    if num_runs == 0:
        return []

    grouped: dict[tuple[str, str], list[ExtractedRequirement]] = {}
    order_keys: list[tuple[str, str]] = []

    for run_reqs in runs_valid:
        seen_in_run: set[tuple[str, str]] = set()
        for r in run_reqs:
            key = (r.role.role_id, r.competency.competency_id if r.competency else "")
            if key in seen_in_run:
                continue
            seen_in_run.add(key)
            if key not in grouped:
                grouped[key] = []
                order_keys.append(key)
            grouped[key].append(r)

    merged: list[ExtractedRequirement] = []
    for key in order_keys:
        group = grouped[key]
        votes = len(group)
        best = max(
            group,
            key=lambda r: (
                r.confidence,
                len(r.evidence[0].quote) if r.evidence else 0,
            ),
        )
        max_conf = max(r.confidence for r in group)
        needs_review = any(r.needs_review for r in group) or (best.competency is None)
        assumptions = list(best.assumptions)

        if num_runs >= 2:
            if votes == 1:
                conf = max(0.56, round(min(max_conf * 0.75, 0.58), 2))
                needs_review = True
                note = f"Ensemble union: found in {votes}/{num_runs} parallel runs; marked needs_review"
                if note not in assumptions and len(assumptions) < 10:
                    assumptions.append(note)
            elif votes < num_runs:
                conf = round(max_conf * 0.95, 2)
            else:
                conf = round(max_conf, 2)
        else:
            conf = best.confidence

        run_hcs = [r.headcount for r in group]
        max_run_hc = max(run_hcs)
        min_run_hc = min(run_hcs)
        quote_str = best.evidence[0].quote if best.evidence else ""
        text_hc = detect_headcount_for_role_near_quote(
            brief_text, quote_str, best.role.role_id
        )
        final_hc = max(max_run_hc, text_hc or 1)

        if min_run_hc != max_run_hc or (text_hc is not None and text_hc != max_run_hc):
            needs_review = True
            hc_note = (
                f"Headcount reconciled to max={final_hc} "
                f"(ensemble runs: {min_run_hc}..{max_run_hc}, text mention: {text_hc})"
            )
            if hc_note not in assumptions and len(assumptions) < 10:
                assumptions.append(hc_note[:300])

        updated_role = best.role.model_copy(update={"confidence": conf})
        updated_comp = (
            best.competency.model_copy(update={"confidence": conf})
            if best.competency is not None
            else None
        )
        merged.append(
            best.model_copy(
                update={
                    "role": updated_role,
                    "competency": updated_comp,
                    "headcount": final_hc,
                    "confidence": conf,
                    "needs_review": needs_review,
                    "assumptions": assumptions[:10],
                }
            )
        )

    # Normalize headcounts by role in code (maximum among role mentions in text)
    return normalize_headcounts_by_role(merged, brief_text)


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
    ensemble_runs: int | None = None,
    verify_candidates: bool | None = None,
    role_safety_net: bool | None = None,
) -> tuple[list[ExtractedRequirement], LLMStats]:
    """Extract requirements via LLM with parallel ensemble union, candidate verification, and role safety net."""
    if not brief_text or not brief_text.strip():
        empty_stats = LLMStats(empty_reason="Empty brief text")
        return [], empty_stats

    per_attempt_timeout_ms = (
        timeout_ms if timeout_ms is not None else settings.timeout_ms
    )
    if per_attempt_timeout_ms <= 0:
        raise LLMTimeoutError(
            f"LLM extraction exceeded timeout budget of {per_attempt_timeout_ms}ms"
        )

    is_real_client = isinstance(client, LLMClient)
    n_runs = (
        ensemble_runs
        if ensemble_runs is not None
        else (settings.ensemble_runs if is_real_client else 1)
    )
    do_verify = (
        verify_candidates
        if verify_candidates is not None
        else (settings.verify_candidates if is_real_client else False)
    )
    do_role_net = (
        role_safety_net
        if role_safety_net is not None
        else (settings.role_safety_net if is_real_client else False)
    )

    initial_max_tokens = max(1200, min(2400, max_requirements * 55))
    base_messages: list[dict[str, Any]] = list(
        build_extract_prompt(
            brief_text,
            taxonomy,
            locale,
            allowed_role_ids=allowed_role_ids,
            allowed_competency_ids=allowed_competency_ids,
        )
    )

    t_overall = time.monotonic()

    # Step 1: Run N parallel extraction passes (seeds 42, 43, 44...)
    if n_runs <= 1:
        valid_0, raw_0, stats = await _run_single_extraction_pass(
            brief_text,
            base_messages,
            taxonomy,
            client,
            settings,
            seed=42,
            per_attempt_timeout_ms=per_attempt_timeout_ms,
            initial_max_tokens=initial_max_tokens,
            allowed_role_ids=allowed_role_ids,
            allowed_competency_ids=allowed_competency_ids,
            return_candidates=return_candidates,
        )
        runs_valid = [valid_0]
        first_raw_items = raw_0
    else:
        tasks = [
            _run_single_extraction_pass(
                brief_text,
                base_messages,
                taxonomy,
                client,
                settings,
                seed=42 + i,
                per_attempt_timeout_ms=per_attempt_timeout_ms,
                initial_max_tokens=initial_max_tokens,
                allowed_role_ids=allowed_role_ids,
                allowed_competency_ids=allowed_competency_ids,
                return_candidates=return_candidates,
            )
            for i in range(n_runs)
        ]
        results = await asyncio.gather(*tasks, return_exceptions=True)

        runs_valid = []
        first_raw_items = []
        pass_stats_list: list[LLMStats] = []
        errors: list[Exception] = []

        for res in results:
            if isinstance(res, Exception):
                errors.append(res)
            else:
                v_list, r_list, p_stats = res
                runs_valid.append(v_list)
                if not first_raw_items and r_list:
                    first_raw_items = r_list
                pass_stats_list.append(p_stats)

        if not pass_stats_list:
            raise errors[0]

        stats = LLMStats(
            tokens_in=sum(s.tokens_in for s in pass_stats_list),
            tokens_out=sum(s.tokens_out for s in pass_stats_list),
            duration_ms=int((time.monotonic() - t_overall) * 1000),
            retries=min(2, max(s.retries for s in pass_stats_list)),
            json_valid=any(s.json_valid for s in pass_stats_list),
            dropped_schema_invalid=pass_stats_list[0].dropped_schema_invalid,
            dropped_unknown_taxonomy=pass_stats_list[0].dropped_unknown_taxonomy,
            dropped_no_evidence=pass_stats_list[0].dropped_no_evidence,
            dropped_invalid_pair=pass_stats_list[0].dropped_invalid_pair,
            ensemble_runs_completed=len(pass_stats_list),
        )

    # Step 2: Reconcile union across parallel runs + code-side role headcount normalization
    union_reqs = _reconcile_ensemble_union(runs_valid, brief_text)

    # Step 3: Word-boundary candidate competency scanner + missing role scanner (Mechanisms 2 & 3)
    estimator = EffortEstimator()
    role_validator = RoleCompetencyValidator(
        taxonomy,
        allowed_role_ids=allowed_role_ids,
        allowed_competency_ids=allowed_competency_ids,
    )
    evidence_validator = EvidenceValidator()

    already_comp_ids = {
        r.competency.competency_id for r in union_reqs if r.competency is not None
    }
    already_role_ids = {r.role.role_id for r in union_reqs}

    unmatched_comps = (
        scan_unmatched_competencies(
            brief_text,
            taxonomy,
            already_comp_ids,
            allowed_role_ids=allowed_role_ids,
            allowed_competency_ids=allowed_competency_ids,
        )
        if do_verify
        else []
    )
    missing_roles = (
        scan_missing_roles(
            brief_text,
            taxonomy,
            already_role_ids,
            allowed_role_ids=allowed_role_ids,
            allowed_competency_ids=allowed_competency_ids,
        )
        if do_role_net
        else []
    )

    stats.candidate_competencies_found = len(unmatched_comps)
    stats.unmatched_candidate_ids = [c.competency_id for c in unmatched_comps]
    stats.missing_roles_found = len(missing_roles)

    cand_comp_map = {c.competency_id: c for c in unmatched_comps}
    missing_role_map = {r.role_id: r for r in missing_roles}

    comp_decisions: dict[str, dict[str, str]] = {}
    role_decisions: dict[str, dict[str, str]] = {}
    rejected_role_ids: set[str] = set()

    def _record_comp(cid: str, status: str, reason: str) -> None:
        """Record the first decision for a competency candidate (never overwrite)."""
        if cid in comp_decisions:
            return
        c = cand_comp_map.get(cid)
        comp_decisions[cid] = {
            "kind": "competency",
            "id": cid,
            "matched_text": c.matched_text if c is not None else "",
            "status": status,
            "reason": reason,
        }

    def _record_role(rid: str, status: str, reason: str) -> None:
        """Record the first decision for a missing-role candidate (never overwrite)."""
        if rid in role_decisions:
            return
        rm = missing_role_map.get(rid)
        role_decisions[rid] = {
            "kind": "role",
            "id": rid,
            "matched_text": rm.matched_text if rm is not None else "",
            "status": status,
            "reason": reason,
        }

    if unmatched_comps or missing_roles:
        union_pair_keys = {
            (
                r.role.role_id,
                r.competency.competency_id if r.competency else "",
            )
            for r in union_reqs
        }
        union_req_ids = {r.requirement_id for r in union_reqs}
        # Monotonic index for requirements created by verification: shared across batches
        # and independent of the (growing) `union_reqs` length, so default ids cannot
        # collide between batches.
        verification_req_index = len(union_reqs)
        batches = _batch_candidates(
            [c.competency_id for c in unmatched_comps],
            [r.role_id for r in missing_roles],
        )

        for batch in batches:
            batch_comps = [
                cand_comp_map[cid] for kind, cid in batch if kind == "competency"
            ]
            batch_roles = [
                missing_role_map[rid] for kind, rid in batch if kind == "role"
            ]
            batch_messages = build_verification_prompt(
                brief_text, batch_comps, batch_roles, locale
            )
            budget = _verification_budget(len(batch))
            max_attempts = 1 + settings.max_retries
            ver_items: list[Any] | None = None
            last_error = "no verification attempt made"

            for attempt in range(max_attempts):
                t_ver = time.monotonic()
                try:
                    ver_resp = await _call_client_chat(
                        client,
                        batch_messages,
                        temperature=0,
                        seed=42,
                        max_tokens=budget,
                        timeout_ms=per_attempt_timeout_ms,
                    )
                except LLMError as exc:
                    stats.duration_ms += int((time.monotonic() - t_ver) * 1000)
                    stats.verification_attempts += 1
                    last_error = (
                        f"verification LLM call failed: {type(exc).__name__}: {exc}"
                    )
                    if (
                        _is_transient_verification_error(exc)
                        and attempt + 1 < max_attempts
                    ):
                        # Transient (5xx / no status / timeout): retry the same batch.
                        continue
                    break

                stats.duration_ms += int((time.monotonic() - t_ver) * 1000)
                stats.verification_attempts += 1
                stats.tokens_in += ver_resp.tokens_in
                stats.tokens_out += ver_resp.tokens_out

                finish_reason = getattr(ver_resp, "finish_reason", None)
                ver_items, parse_err = _parse_json_items(ver_resp.content)
                if ver_items is not None:
                    break

                last_error = (
                    f"{parse_err} (finish_reason={finish_reason}, max_tokens={budget})"
                )
                if attempt + 1 < max_attempts:
                    budget = min(_VERIFICATION_MAX_TOKENS_CAP, int(budget * 1.5))
                    nudge = _verification_nudge_message(parse_err, len(batch))
                    prev_content = (ver_resp.content or "").strip()
                    if prev_content:
                        # Keep the rejected answer so the model can correct it.
                        batch_messages = batch_messages + [
                            {"role": "assistant", "content": prev_content},
                            {"role": "user", "content": nudge},
                        ]
                    else:
                        # Never append an empty assistant turn (some APIs reject it).
                        batch_messages = batch_messages + [
                            {"role": "user", "content": nudge},
                        ]

            if ver_items is None:
                # Whole batch failed after retries: mark its candidates skipped and
                # continue with the next batch (never abort the extraction).
                stats.verification_failed_batches += 1
                for kind, item_id in batch:
                    if kind == "competency":
                        _record_comp(item_id, "skipped", last_error)
                    else:
                        _record_role(item_id, "skipped", last_error)
                continue

            for v_item in ver_items:
                if not isinstance(v_item, dict):
                    continue
                decision = str(v_item.get("decision") or "").strip().lower()
                raw_cid = v_item.get("competency_id")
                if raw_cid is None:
                    nested_comp = v_item.get("competency")
                    if isinstance(nested_comp, dict):
                        raw_cid = nested_comp.get("competency_id")
                    elif isinstance(nested_comp, str):
                        raw_cid = nested_comp
                c_id_str = (
                    raw_cid.strip()
                    if isinstance(raw_cid, str)
                    and raw_cid.strip().lower() not in ("", "null", "none")
                    else None
                )
                raw_rid = v_item.get("role_id")
                if raw_rid is None:
                    nested_role = v_item.get("role")
                    if isinstance(nested_role, dict):
                        raw_rid = nested_role.get("role_id")
                    elif isinstance(nested_role, str):
                        raw_rid = nested_role
                r_id_str = raw_rid.strip() if isinstance(raw_rid, str) else ""
                llm_reason = str(v_item.get("reason") or "").strip()

                if decision not in ("confirm", "reject"):
                    # Never default-confirm: an explicit decision is required.
                    reason = (
                        "missing decision field"
                        if not decision
                        else f"unknown decision {decision!r}"
                    )
                    if c_id_str is not None and c_id_str in cand_comp_map:
                        _record_comp(c_id_str, "skipped", reason)
                    elif c_id_str is None and r_id_str in missing_role_map:
                        _record_role(r_id_str, "skipped", reason)
                    continue

                if decision == "reject":
                    # Attribution: a reject may only drop the candidate it actually names.
                    if c_id_str is not None and c_id_str in cand_comp_map:
                        c = cand_comp_map[c_id_str]
                        _record_comp(
                            c_id_str,
                            "rejected",
                            llm_reason
                            or f"rejected by LLM in context: {c.quote_sentence[:80]!r}",
                        )
                    elif c_id_str is None and r_id_str in missing_role_map:
                        rm = missing_role_map[r_id_str]
                        rejected_role_ids.add(r_id_str)
                        _record_role(
                            r_id_str,
                            "rejected",
                            llm_reason
                            or f"rejected by LLM in context: {rm.quote_sentence[:80]!r}",
                        )
                    # Unrelated competency_id / unknown role_id: count nothing.
                    continue

                # Decision == "confirm": attribute it to the candidate actually sent
                # for verification. A confirm for anything else is never appended.
                if c_id_str is not None:
                    cand = cand_comp_map.get(c_id_str)
                    if cand is None:
                        # Not a candidate of this run: no enrichment, no append.
                        continue
                    if not r_id_str:
                        _record_comp(
                            c_id_str,
                            "skipped",
                            f"confirmed {c_id_str} without a role_id",
                        )
                        continue
                    if r_id_str not in cand.allowed_roles:
                        _record_comp(
                            c_id_str,
                            "skipped",
                            f"confirmed role {r_id_str} not in allowed_roles for {c_id_str}",
                        )
                        continue
                elif not r_id_str or r_id_str not in missing_role_map:
                    # Role-only confirm must target a missing-role candidate.
                    continue

                # Decision == "confirm": require a schema-valid pair with an exact quote.
                req_index = verification_req_index
                verification_req_index += 1
                req, err = _enrich_single_item(
                    v_item,
                    req_index,
                    brief_text,
                    taxonomy,
                    estimator,
                    return_candidates=return_candidates,
                    effective_team_size=max(len(union_reqs) + 1, 1),
                )
                if req is None:
                    if c_id_str is not None:
                        _record_comp(
                            c_id_str,
                            "skipped",
                            f"LLM confirmed, but schema invalid: {err}",
                        )
                    continue

                r_id = req.role.role_id
                c_id = req.competency.competency_id if req.competency else None
                pair_ok, _, pair_reason = role_validator.check_pair(r_id, c_id)
                if not pair_ok:
                    if c_id_str is not None:
                        _record_comp(
                            c_id_str,
                            "skipped",
                            f"LLM confirmed, but pair invalid: {pair_reason}",
                        )
                    continue

                corrected_spans = evidence_validator._validate_evidence_spans(
                    req.evidence, brief_text
                )
                if corrected_spans is None:
                    bad_q = req.evidence[0].quote if req.evidence else ""
                    if c_id_str is not None:
                        _record_comp(
                            c_id_str,
                            "skipped",
                            f"LLM confirmed, but quote {bad_q!r} not found in brief",
                        )
                    continue

                pair_key = (r_id, c_id or "")
                if pair_key in union_pair_keys:
                    # Guard against a confirmed pair duplicating one already extracted.
                    if c_id_str is not None:
                        _record_comp(c_id_str, "confirmed", "already present")
                    if r_id in missing_role_map and r_id not in already_role_ids:
                        already_role_ids.add(r_id)
                        _record_role(r_id, "confirmed", "already present")
                    continue

                if req.requirement_id in union_req_ids:
                    # A default id collided with an existing requirement: renumber.
                    base_id = req.requirement_id[:34]
                    suffix = 1
                    while f"{base_id}_v{suffix}" in union_req_ids:
                        suffix += 1
                    req = req.model_copy(
                        update={"requirement_id": f"{base_id}_v{suffix}"}
                    )

                valid_req = req.model_copy(update={"evidence": corrected_spans})
                valid_req = role_validator._apply_review_flags(valid_req)
                union_reqs.append(valid_req)
                union_req_ids.add(valid_req.requirement_id)
                union_pair_keys.add(pair_key)

                if c_id_str is not None:
                    _record_comp(
                        c_id_str,
                        "confirmed",
                        llm_reason
                        or f"confirmed as {r_id}/{c_id_str} with quote {corrected_spans[0].quote[:60]!r}",
                    )

                if r_id in missing_role_map and r_id not in already_role_ids:
                    already_role_ids.add(r_id)
                    _record_role(
                        r_id,
                        "confirmed",
                        llm_reason
                        or f"recovered role {r_id} (competency={c_id}) via verification call",
                    )

        # Account for any candidate competency omitted by the LLM in its response
        for cid, c in cand_comp_map.items():
            if cid not in comp_decisions:
                _record_comp(
                    cid,
                    "skipped",
                    f"omitted by LLM in verification response (sentence: {c.quote_sentence[:70]!r})",
                )

    # Role Safety Net fallback: if a role mentioned in the brief is still missing and wasn't rejected
    if do_role_net and missing_roles:
        current_roles = {r.role.role_id for r in union_reqs}
        for rm in missing_roles:
            if rm.role_id in current_roles or rm.role_id in rejected_role_ids:
                continue
            found_span = find_quote_in_text(
                rm.quote_sentence, brief_text
            ) or find_quote_in_text(rm.matched_text, brief_text)
            if found_span is None:
                role_decisions[rm.role_id] = {
                    "kind": "role",
                    "id": rm.role_id,
                    "matched_text": rm.matched_text,
                    "status": "skipped",
                    "reason": "role quote could not be matched in brief text",
                }
                continue
            orig_quote, cs, ce = found_span
            effort_range, effort_assumption = estimator.estimate_for_fields(
                role_id=rm.role_id,
                comp_id=None,
                level=3,
                headcount=1,
                brief_text=brief_text,
                effective_team_size=max(len(union_reqs) + 1, 1),
            )
            role_only_req = ExtractedRequirement(
                requirement_id=f"req_role_{rm.role_id[:20]}",
                role=ExtractRole(
                    role_id=rm.role_id,
                    name_ru=rm.name_ru,
                    match="hybrid",
                    confidence=0.6,
                ),
                competency=None,
                level=3,
                criticality="must",
                headcount=1,
                effort=effort_range,
                assumptions=[
                    f"Role safety net: role '{rm.role_id}' found in brief without extracted competency; marked needs_review",
                    effort_assumption[:300],
                ],
                confidence=0.6,
                needs_review=True,
                evidence=[
                    EvidenceSpan(
                        kind="brief_span",
                        quote=orig_quote,
                        char_start=cs,
                        char_end=ce,
                    )
                ],
            )
            union_reqs.append(role_only_req)
            current_roles.add(rm.role_id)
            role_decisions[rm.role_id] = {
                "kind": "role",
                "id": rm.role_id,
                "matched_text": rm.matched_text,
                "status": "confirmed",
                "reason": f"recovered as role-only (competency=None, needs_review=True) by Role Safety Net from {orig_quote[:60]!r}",
            }

    for rid, rm in missing_role_map.items():
        if rid not in role_decisions:
            role_decisions[rid] = {
                "kind": "role",
                "id": rid,
                "matched_text": rm.matched_text,
                "status": "skipped",
                "reason": "not recovered",
            }

    # Populate exact verification summary counters & full log
    if cand_comp_map:
        stats.candidate_competencies_confirmed = sum(
            1 for d in comp_decisions.values() if d["status"] == "confirmed"
        )
        stats.candidate_competencies_rejected = sum(
            1 for d in comp_decisions.values() if d["status"] == "rejected"
        )
        stats.candidate_competencies_skipped = sum(
            1 for d in comp_decisions.values() if d["status"] == "skipped"
        )
    if missing_role_map:
        stats.missing_roles_recovered = sum(
            1 for d in role_decisions.values() if d["status"] == "confirmed"
        )
        stats.missing_roles_rejected = sum(
            1 for d in role_decisions.values() if d["status"] == "rejected"
        )
        stats.missing_roles_skipped = sum(
            1 for d in role_decisions.values() if d["status"] == "skipped"
        )

    stats.verification_log = list(comp_decisions.values()) + list(
        role_decisions.values()
    )

    # Step 4: Final deterministic 6-stage validation gate + lexical check + role headcount normalization
    final_valid, pipe_stats = evidence_validator.validate_pipeline(
        union_reqs,
        brief_text,
        taxonomy,
        allowed_role_ids=allowed_role_ids,
        allowed_competency_ids=allowed_competency_ids,
        min_confidence=min_confidence,
        max_requirements=max_requirements,
    )

    stats.raw_items = first_raw_items if first_raw_items else list(final_valid)
    stats.dropped_schema_invalid += pipe_stats.dropped_schema_invalid
    stats.dropped_unknown_taxonomy += pipe_stats.dropped_unknown_taxonomy
    stats.dropped_no_evidence += pipe_stats.dropped_no_evidence
    stats.dropped_invalid_pair += pipe_stats.dropped_invalid_pair
    stats.dropped_duplicate = pipe_stats.dropped_duplicate
    stats.dropped_low_confidence = pipe_stats.dropped_low_confidence
    stats.dropped_over_limit = pipe_stats.dropped_over_limit
    stats.flagged_no_lexical_anchor = pipe_stats.flagged_no_lexical_anchor
    stats.dropped_generic_mention += pipe_stats.dropped_generic_mention
    stats.dropped_generic_ids += pipe_stats.dropped_generic_ids

    if not final_valid and not stats.empty_reason:
        if not stats.json_valid:
            stats.empty_reason = "Invalid JSON response after retries"
        elif stats.dropped_no_evidence + stats.post_processor_dropped > 0:
            stats.empty_reason = "All extracted items dropped by deterministic validation"
        else:
            stats.empty_reason = "Empty JSON array returned by LLM"

    return final_valid, stats
