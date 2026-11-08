"""DEPRECATED: Rules-based extraction engine (retained only for eval reference stage).

Per ADR-005 v2 (`tz_ml_extract_fix_v2.md` §3.7), `/v1/extract` is LLM-first and no
longer calls `RulesEngine` in the main request path. This module is retained solely
for `eval/run_eval.py` reference comparisons and unit tests.
"""

from __future__ import annotations

import hashlib
import re

from .defaults import get_primary_role
from .effort import PartialRequirement
from .schemas import EvidenceSpan, ExtractCompetency, ExtractRole
from .taxonomy import TaxonomyRegistry, get_registry


# ---------------------------------------------------------------------------
# Keyword patterns
# ---------------------------------------------------------------------------

# Level keywords (case-insensitive)
_LEVEL_MAP: list[tuple[re.Pattern[str], int]] = [
    (re.compile(r"\b(?:junior|джуниор|джуна?)\b", re.IGNORECASE), 2),
    (re.compile(r"\b(?:middle|мидл|миддл)\b", re.IGNORECASE), 3),
    (re.compile(r"\b(?:senior|сеньор|синьор)\b", re.IGNORECASE), 4),
    (re.compile(r"\b(?:lead|лид|лив)\b", re.IGNORECASE), 4),
    (re.compile(r"\b(?:principal|принципал)\b", re.IGNORECASE), 5),
]

# Criticality keywords
_CRITICALITY_MUST = re.compile(
    r"\b(?:обязательно|must|критично|required|необходим|нужен|нужна|нужно)\b",
    re.IGNORECASE,
)
_CRITICALITY_SHOULD = re.compile(
    r"\b(?:желательно|should|хотелось бы|желателен)\b",
    re.IGNORECASE,
)
_CRITICALITY_NICE = re.compile(
    r"\b(?:опционально|nice[- ]to[- ]have|можно без|nice)\b",
    re.IGNORECASE,
)

# Headcount: explicit numbers near role mentions.
_HEADCOUNT_PATTERNS: list[re.Pattern[str]] = [
    # "<digit> <qualified-role-word>"
    re.compile(
        r"(\d+)\s*(?:бэкенд[- ]?разработчик[а-я]*|фронтенд[- ]?разработчик[а-я]*|"
        r"бэкенд|фронтенд|дата[- ]?инженер[а-я]*|ml[- ]?инженер[а-я]*|"
        r"бизнес[- ]?аналитик[а-я]*|системный\s+аналитик[а-я]*|"
        r"тестировщик[а-я]*|qa[- ]?инженер[а-я]*|qa|"
        r"devops[- ]?инженер[а-я]*|devops|sre|"
        r"архитектор[а-я]*|дизайнер[а-я]*|менеджер[а-я]*|"
        r"руководитель\s+проект[а-я]*|project\s+manager|scrum\s+master|"
        r"инженер[а-я]*\s+по\s+безопасности|security\s+engineer)",
        re.IGNORECASE,
    ),
    # English role-specific patterns
    re.compile(
        r"(\d+)\s*(?:backend\s+developer|frontend\s+developer|data\s+engineer|"
        r"ml\s+engineer|business\s+analyst|system\s+analyst|qa\s+engineer|"
        r"devops\s+engineer|solution\s+architect|security\s+engineer|"
        r"ux/ui\s+designer|project\s+manager)",
        re.IGNORECASE,
    ),
]

# Russian number words -> int
_RU_NUMBERS: dict[str, int] = {
    "один": 1, "одна": 1, "одно": 1, "два": 2, "две": 2, "три": 3,
    "четыре": 4, "пять": 5, "шесть": 6, "семь": 7, "восемь": 8, "девять": 9,
    "десять": 10,
}

# Synonyms excluded from rules matching (too ambiguous or reserved for LLM)
_EXCLUDED_RULES_SYNONYMS: set[str] = {"го"}

# Short ambiguous synonyms matched case-sensitively to avoid false positives.
_CASE_SENSITIVE_SHORT_SYNONYMS: set[str] = {
    "ba", "sa", "pm", "ds", "ch", "sh", "ts", "cv", "ui", "ux", "soc", "git",
}

_CYRILLIC_WORD_RE = re.compile(r"^[а-яё]+$")
_SENTENCE_SPLIT_RE = re.compile(r"(?<=[.!?])\s+|\n+")


# ---------------------------------------------------------------------------
# Detection helpers
# ---------------------------------------------------------------------------


def _is_cyrillic_word(text: str) -> bool:
    """True if text consists solely of Cyrillic letters."""
    return bool(_CYRILLIC_WORD_RE.match(text.lower()))


def _strip_quote_range(
    text: str, start: int, end: int
) -> tuple[str, int, int]:
    """Return the substring and adjusted offsets with whitespace stripped."""
    raw = text[start:end]
    lstripped = raw.lstrip()
    new_start = start + (len(raw) - len(lstripped))
    rstripped = lstripped.rstrip()
    new_end = new_start + len(rstripped)
    return rstripped, new_start, new_end


def _expand_short_quote(
    text: str, start: int, end: int
) -> tuple[str, int, int]:
    """Expand a quote shorter than 3 chars to include a neighboring token.

    Keeps the evidence quote a clean substring of the brief text.
    """
    # Try to include the next alphanumeric token to the right
    expanded_end = end
    while expanded_end < len(text) and not text[expanded_end].isalnum():
        expanded_end += 1
    word_end = expanded_end
    while word_end < len(text) and text[word_end].isalnum():
        word_end += 1
    if word_end - start >= 3:
        return _strip_quote_range(text, start, word_end)

    # Try to include the previous alphanumeric token to the left
    expanded_start = start
    while expanded_start > 0 and not text[expanded_start - 1].isalnum():
        expanded_start -= 1
    word_start = expanded_start
    while word_start > 0 and text[word_start - 1].isalnum():
        word_start -= 1
    if end - word_start >= 3:
        return _strip_quote_range(text, word_start, end)

    # Fallback: include both neighboring tokens
    if word_end - word_start >= 3:
        return _strip_quote_range(text, word_start, word_end)

    return _strip_quote_range(text, start, end)


def _build_synonym_pattern(syn: str) -> re.Pattern[str] | None:
    """Build a token-boundary regex for a synonym.

    - Excluded synonyms return None.
    - Pure Cyrillic synonyms >= 4 chars allow flex endings.
    - Short ambiguous synonyms are matched case-sensitively.
    - Everything else is case-insensitive.
    """
    if syn in _EXCLUDED_RULES_SYNONYMS:
        return None

    escaped = re.escape(syn)
    flags = re.UNICODE
    case_sensitive = syn in _CASE_SENSITIVE_SHORT_SYNONYMS and len(syn) <= 3

    if not case_sensitive:
        flags |= re.IGNORECASE

    if _is_cyrillic_word(syn) and len(syn) >= 4:
        body = rf"{escaped}[а-яё]{{0,4}}"
    else:
        body = escaped

    return re.compile(rf"(?<!\w){body}(?!\w)", flags)


def _find_all_synonym_matches(
    text: str,
    registry: TaxonomyRegistry,
    kind: str,
) -> list[tuple[str, str, int, int, float]]:
    """Find all token-bound synonym matches in text for roles or competencies.

    Returns list of (match_text, id, char_start, char_end, confidence).
    Longer synonyms are matched first to avoid partial overlaps.
    """
    syn_to_id: dict[str, str] = {}
    for syn_lower, (k, cid) in registry.synonym_index.items():
        if k == kind:
            syn_to_id[syn_lower] = cid

    sorted_syns = sorted(syn_to_id.keys(), key=len, reverse=True)
    results: list[tuple[str, str, int, int, float]] = []
    used_ranges: list[tuple[int, int]] = []

    for syn in sorted_syns:
        if len(syn) < 2:
            continue
        pattern = _build_synonym_pattern(syn)
        if pattern is None:
            continue

        for m in pattern.finditer(text):
            start, end = m.start(), m.end()
            overlaps = any(start < ue and end > us for us, ue in used_ranges)
            if overlaps:
                continue

            cid = syn_to_id[syn]
            original = text[start:end]

            # Confidence: exact case match for the matched token -> 1.0
            if syn in _CASE_SENSITIVE_SHORT_SYNONYMS and len(syn) <= 3:
                confidence = 1.0 if original == syn else 0.95
            else:
                confidence = 1.0 if original.lower() == syn else 0.95

            quote, quote_start, quote_end = _strip_quote_range(text, start, end)
            if len(quote) < 3:
                quote, quote_start, quote_end = _expand_short_quote(text, start, end)

            results.append((quote, cid, quote_start, quote_end, confidence))
            used_ranges.append((start, end))

    return results


def _sentence_boundaries(text: str) -> list[tuple[int, int]]:
    """Split text into sentence/line fragments with (start, end) offsets."""
    boundaries: list[tuple[int, int]] = []
    start = 0
    for m in _SENTENCE_SPLIT_RE.finditer(text):
        end = m.start()
        if end > start:
            boundaries.append((start, end))
        start = m.end()
    if start < len(text):
        boundaries.append((start, len(text)))
    return boundaries


def _fragment_for_position(
    boundaries: list[tuple[int, int]], pos: int
) -> tuple[int, int]:
    """Return the fragment containing the given position."""
    for start, end in boundaries:
        if start <= pos < end:
            return start, end
    if boundaries:
        return boundaries[-1]
    return 0, 0


def _detect_level(text: str) -> int:
    """Detect level from keywords. Default: 3 (middle)."""
    for pattern, level in _LEVEL_MAP:
        if pattern.search(text):
            return level
    return 3


def _detect_criticality(text: str, headcount: int) -> str:
    """Detect criticality from keywords."""
    if _CRITICALITY_NICE.search(text):
        return "nice"
    if _CRITICALITY_SHOULD.search(text):
        return "should"
    if _CRITICALITY_MUST.search(text):
        return "must"
    return "must" if headcount > 0 else "should"


def _detect_headcount(text: str, role_name_hint: str | None = None) -> int:
    """Detect headcount from numeric patterns near role mentions."""
    for pat in _HEADCOUNT_PATTERNS:
        m = pat.search(text)
        if m:
            val = int(m.group(1))
            if 1 <= val <= 20:
                return val

    if role_name_hint:
        for ru_word, num in _RU_NUMBERS.items():
            pat = re.compile(
                rf"\b{re.escape(ru_word)}\s+{re.escape(role_name_hint)}\b",
                re.IGNORECASE,
            )
            if pat.search(text):
                return num

    return 1


def _make_requirement_id(role_id: str, competency_id: str | None, headcount: int, criticality: str) -> str:
    """Deterministic short hash: req_<4hex>."""
    key = f"{role_id}|{competency_id or 'none'}|{headcount}|{criticality}"
    h = hashlib.sha256(key.encode()).hexdigest()[:4]
    return f"req_{h}"


def _distance_between_spans(
    c_start: int, c_end: int, rm_start: int, rm_end: int
) -> int:
    """Minimal char distance between two spans."""
    if rm_start <= c_start <= rm_end or rm_start <= c_end <= rm_end:
        return 0
    return min(abs(c_start - rm_end), abs(rm_start - c_end))


def _select_role_for_competency(
    comp_id: str,
    c_start: int,
    c_end: int,
    role_matches: list[tuple[str, str, int, int, float]],
    text: str,
    registry: TaxonomyRegistry,
) -> str:
    """Choose the best role for a competency mention.

    Priority:
    1. Explicit role in the same sentence/line fragment that is valid for the competency.
    2. For testing competencies, default to qa_engineer when no explicit QA role.
    3. If exactly one valid role is mentioned anywhere in the brief, use it.
    4. Default role from the taxonomy/primary-role table.
    """
    comp = registry.get_competency(comp_id)
    if comp is None:
        return ""

    possible_roles = set(comp.roles)
    boundaries = _sentence_boundaries(text)
    frag_start, frag_end = _fragment_for_position(boundaries, c_start)

    # 1. Same fragment explicit role
    same_fragment_roles = [
        (rm_id, rm_start, rm_end)
        for _, rm_id, rm_start, rm_end, _ in role_matches
        if rm_id in possible_roles
        and not (rm_end <= frag_start or rm_start >= frag_end)
    ]
    if same_fragment_roles:
        return min(
            same_fragment_roles,
            key=lambda x: _distance_between_spans(c_start, c_end, x[1], x[2]),
        )[0]

    # 2. Testing competencies -> QA engineer
    if comp.category == "testing" and "qa_engineer" in possible_roles:
        return "qa_engineer"

    # 3. Exactly one valid role mentioned anywhere
    found_allowed = [
        (rm_id, rm_start, rm_end)
        for _, rm_id, rm_start, rm_end, _ in role_matches
        if rm_id in possible_roles
    ]
    if len(found_allowed) == 1:
        return found_allowed[0][0]

    # 4. Default role
    default_role = get_primary_role(comp_id, registry)
    if default_role and default_role in possible_roles:
        return default_role

    return sorted(possible_roles)[0] if possible_roles else ""


# ---------------------------------------------------------------------------
# Main engine
# ---------------------------------------------------------------------------


class RulesEngine:
    """Extract partial requirements from brief text using synonym matching + regex."""

    def __init__(self, registry: TaxonomyRegistry | None = None):
        self.registry = registry or get_registry()

    def extract(self, brief_text: str, locale: str = "ru") -> list[PartialRequirement]:
        """Extract role+competency requirements from brief text."""
        text = brief_text

        role_matches = _find_all_synonym_matches(text, self.registry, "role")
        comp_matches = _find_all_synonym_matches(text, self.registry, "competency")

        # Group evidence by (role_id, competency_id)
        pair_evidence: dict[tuple[str, str], list[EvidenceSpan]] = {}
        used_roles: set[str] = set()

        for match_text, comp_id, c_start, c_end, conf in comp_matches:
            role_id = _select_role_for_competency(
                comp_id, c_start, c_end, role_matches, text, self.registry
            )
            if not role_id:
                continue
            used_roles.add(role_id)

            ev = EvidenceSpan(
                kind="brief_span",
                quote=match_text,
                char_start=c_start,
                char_end=c_end,
            )
            pair_evidence.setdefault((role_id, comp_id), []).append(ev)

        results: list[PartialRequirement] = []
        seen_ids: set[str] = set()

        for (role_id, comp_id), evidence_list in sorted(pair_evidence.items()):
            role = self.registry.get_role(role_id)
            if role is None:
                continue

            extract_role = ExtractRole(
                role_id=role_id,
                name_ru=role.name_ru,
                match="exact_synonym",
                confidence=1.0,
            )

            extract_comp: ExtractCompetency | None = None
            comp = self.registry.get_competency(comp_id)
            if comp:
                extract_comp = ExtractCompetency(
                    competency_id=comp_id,
                    name_ru=comp.name_ru,
                    match="exact_synonym",
                    confidence=1.0,
                    candidates=[],
                )

            context_parts: list[str] = []
            for ev in evidence_list:
                if ev.char_start is not None and ev.char_end is not None:
                    ctx_start = max(0, ev.char_start - 150)
                    ctx_end = min(len(text), ev.char_end + 150)
                    context_parts.append(text[ctx_start:ctx_end])
                else:
                    context_parts.append(ev.quote)
            context = " ".join(context_parts)

            level = _detect_level(context)
            headcount = _detect_headcount(context)
            criticality = _detect_criticality(context, headcount)

            req_id = _make_requirement_id(role_id, comp_id, headcount, criticality)
            base_id = req_id
            counter = 1
            while req_id in seen_ids:
                req_id = f"{base_id}_{counter}"
                counter += 1
            seen_ids.add(req_id)

            partial = PartialRequirement(
                requirement_id=req_id,
                role=extract_role,
                competency=extract_comp,
                level=level,
                criticality=criticality,
                headcount=headcount,
                evidence=list(evidence_list),
                confidence=0.95,
                assumptions=[],
            )
            results.append(partial)

        # Role-only requirements: explicit role mentions with no attached competency.
        # These are kept but flagged for review by the postprocessor.
        for match_text, role_id, rm_start, rm_end, _conf in role_matches:
            if role_id in used_roles:
                continue
            if role_id in seen_ids:  # one role-only req per role
                continue
            role = self.registry.get_role(role_id)
            if role is None:
                continue

            context_start = max(0, rm_start - 150)
            context_end = min(len(text), rm_end + 150)
            context = text[context_start:context_end]

            level = _detect_level(context)
            headcount = _detect_headcount(context)
            criticality = _detect_criticality(context, headcount)

            req_id = _make_requirement_id(role_id, None, headcount, criticality)
            base_id = req_id
            counter = 1
            while req_id in seen_ids:
                req_id = f"{base_id}_{counter}"
                counter += 1
            seen_ids.add(req_id)

            ev = EvidenceSpan(
                kind="brief_span",
                quote=match_text,
                char_start=rm_start,
                char_end=rm_end,
            )
            results.append(
                PartialRequirement(
                    requirement_id=req_id,
                    role=ExtractRole(
                        role_id=role_id,
                        name_ru=role.name_ru,
                        match="exact_synonym",
                        confidence=1.0,
                    ),
                    competency=None,
                    level=level,
                    criticality=criticality,
                    headcount=headcount,
                    evidence=[ev],
                    confidence=0.95,
                    assumptions=[],
                )
            )

        results.sort(key=lambda r: r.requirement_id)
        return results
