"""Deterministic result validator, lexical competency check, and evidence hallucination gate.

Hard rule: every requirement's `evidence[].quote` MUST be a literal substring of
`brief.text`. Quotes are matched with character/whitespace normalization
(multiple spaces, non-breaking spaces, `ё/е`, dash variants, quotation marks),
and replaced with the exact original slice `brief.text[char_start:char_end]`.

Also implements:
  - Ordered 6-stage deterministic validation pipeline (3.6)
  - Non-destructive lexical competency anchor check: if neither the competency's
    names (`name_en`, `name_ru`) nor any of its synonyms (or their stems) appear
    in `quote` or its local surrounding context in `brief_text`, the requirement
    is NOT deleted, but its `confidence` is lowered and `needs_review=True` is set.
  - Code-side role headcount normalization (`normalize_headcounts_by_role`).
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any, Iterable

from pydantic import ValidationError

from .candidates import normalize_headcounts_by_role
from .mentions import has_generic_marker, is_generic_mention
from .postprocessor import RoleCompetencyValidator
from .schemas import EvidenceSpan, ExtractedRequirement
from .taxonomy import Competency, TaxonomyRegistry

# ---------------------------------------------------------------------------
# Character normalization tables for quote matching
# ---------------------------------------------------------------------------

_DASH_CHARS = frozenset(
    "\u2010\u2011\u2012\u2013\u2014\u2015\u2212\ufe58\ufe63\uff0d"
)
_QUOTE_CHARS = frozenset("«»“”„‟'’‘‚‹›")
_SPACE_CHARS = frozenset(" \t\n\r\x0b\x0c\u00a0\u202f\u2007\u2009\u200a")

# Words that are role nouns or generic words and should not by themselves count
# as a stem match for a specific competency when extracting stems from multi-word names.
_NON_DISTINGUISHING_STEM_WORDS: frozenset[str] = frozenset(
    {
        "работа",
        "работы",
        "работе",
        "тестирование",
        "тестировщик",
        "разработка",
        "разработчик",
        "инженер",
        "аналитик",
        "анализ",
        "система",
        "системы",
        "управление",
        "проектирование",
        "безопасность",
        "технологии",
        "данных",
        "данные",
        "сервис",
        "сервисы",
        "приложений",
        "приложения",
        "интерфейсов",
        "интерфейс",
        "дизайн",
        "моделирование",
        "база",
        "базы",
        "язык",
        "техническое",
        "задание",
    }
)

# Terms that describe a role or a broad activity but do not identify a
# particular competency on their own.  These must not make an LLM assignment
# look lexically grounded.  A generic role-only mention is useful for staffing,
# but it is not evidence for (for example) manual testing or ETL.
_GENERIC_COMPETENCY_TERMS: frozenset[str] = frozenset(
    {
        "тестирование",
        "тестировщик",
        "аналитика",
        "аналитик",
        "рекомендации",
        "дизайнер",
        "архитектор",
        "архитектура",
        "дата-инженер",
        "инженер данных",
        "мобильное приложение",
        "mobile application",
    }
)

# Closed list of implicit/contextual anchors allowed by GOLD_POLICY.md
_IMPLICIT_COMPETENCY_ANCHORS: dict[str, tuple[str, ...]] = {
    "api_rest": ("rest", "openapi", "swagger", "2 api", "внешние api", "через api"),
    "sec_appsec": ("pci dss", "152-фз", "безопасность приложен", "appsec"),
    "sec_iam": ("oauth", "oidc", "keycloak", "iam", "rbac", "ролевую модель доступа", "единый вход", "sso"),
    "dom_fintech": ("платёж", "платеж", "банк", "эквайринг", "карт", "pci dss", "финтех"),
    "dom_retail": ("ритейл", "розничн", "магазин", "fmcg", "маркетплейс", "товар", "покупател", "корзин"),
    "dom_telecom": ("телеком", "оператор связи", "абонент", "биллинг"),
    "dom_logistics": ("логист", "склад", "постав", "перевоз", "маршрут", "wms", "tms", "supply chain"),
    "etl_orchestration": ("airflow", "dagster", "prefect", "etl", "elt", "пайплайн", "конвейер", "загрузк", "миграция данных"),
    "dwh_modeling": ("dwh", "data lake", "хранилищ", "витрин", "iceberg", "снежинк", "звезд"),
    "arch_integration_patterns": ("esb", "шин", "pub/sub", "request/reply", "outbox", "saga", "идемпотентн", "retry"),
    "arch_event_driven": ("событи", "event", "eda", "асинхронн"),
    "arch_microservices": ("микросервис", "microservice"),
    "an_integration": ("интеграц", "обмен", "erp", "crm", "биллинг", "1с", "1c", "апи", "api"),
    "web_accessibility": ("wcag", "доступност", "a11y", "aria"),
}


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
    """Normalize text and return `(norm_text, orig_starts, orig_ends)`."""
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
    """Locate `quote` inside `brief_text` using exact or normalized matching."""
    if not quote or not brief_text:
        return None

    idx = brief_text.find(quote)
    if idx != -1:
        if len(quote) < 3:
            return None
        return quote, idx, idx + len(quote)

    trimmed = quote.strip()
    if not trimmed:
        return None
    idx = brief_text.find(trimmed)
    if idx != -1:
        if len(trimmed) < 3:
            return None
        return trimmed, idx, idx + len(trimmed)

    norm_quote, _, _ = _build_normalized_index(trimmed)
    norm_quote = norm_quote.strip()
    if len(norm_quote) < 3:
        return None

    norm_text, orig_starts, orig_ends = _build_normalized_index(brief_text)
    if not norm_text:
        return None

    n_idx = norm_text.find(norm_quote)
    if n_idx == -1:
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


def has_competency_lexical_anchor(
    comp: Competency,
    quote: str,
    brief_text: str,
    char_start: int | None = None,
    char_end: int | None = None,
) -> bool:
    """Return True if `comp`'s name, synonym, or distinguishing stem appears in `quote` or nearby context.

    Checks `quote` plus a `±120` character window around `quote` in `brief_text`.
    Never deletes a requirement; used to flag unanchored competency assignments
    with `needs_review=True` and lowered confidence.
    """
    if not brief_text and not quote:
        return False

    if char_start is not None and char_end is not None and brief_text:
        para_start = brief_text.rfind("\n\n", 0, char_start)
        para_start = 0 if para_start == -1 else para_start + 2
        para_end = brief_text.find("\n\n", char_end)
        para_end = len(brief_text) if para_end == -1 else para_end
        win_start = max(para_start, char_start - 120)
        win_end = min(para_end, char_end + 120)
        window = brief_text[win_start:win_end] + " " + quote
    elif quote and brief_text:
        pos = brief_text.find(quote)
        if pos != -1:
            para_start = brief_text.rfind("\n\n", 0, pos)
            para_start = 0 if para_start == -1 else para_start + 2
            para_end = brief_text.find("\n\n", pos + len(quote))
            para_end = len(brief_text) if para_end == -1 else para_end
            win_start = max(para_start, pos - 120)
            win_end = min(para_end, pos + len(quote) + 120)
            window = brief_text[win_start:win_end]
        else:
            window = quote
    else:
        window = quote or brief_text

    norm_win = _safe_lower(window.replace("ё", "е").replace("Ё", "Е"))

    # 1. Check closed implicit/contextual anchors
    for anchor in _IMPLICIT_COMPETENCY_ANCHORS.get(comp.competency_id, ()):
        if anchor in norm_win:
            return True

    # 2. Check direct substring / word-boundary presence of name_en, name_ru, and synonyms
    all_terms = [comp.name_en, comp.name_ru, *comp.synonyms]
    for raw_term in all_terms:
        term = _safe_lower(raw_term.strip().replace("ё", "е").replace("Ё", "Е"))
        if not term or len(term) < 2:
            continue
        if term in _GENERIC_COMPETENCY_TERMS:
            continue
        if len(term) <= 3:
            # Require word boundary for short 2-3 char tokens ("go", "c4", "bi", "ml", "qa", "тз")
            pat = re.compile(rf"(?<![а-яёa-z0-9_]){re.escape(term)}(?![а-яёa-z0-9_])")
            if pat.search(norm_win):
                return True
        else:
            if term in norm_win:
                return True

    # 3. Check distinguishing Cyrillic/Latin word stems (>= 4 chars) from multi-word terms
    for raw_term in all_terms:
        term = _safe_lower(raw_term.strip().replace("ё", "е").replace("Ё", "Е"))
        words = [
            w
            for w in re.split(r"[^а-яa-z0-9]+", term)
            if len(w) >= 5 and w not in _NON_DISTINGUISHING_STEM_WORDS
        ]
        for w in words:
            stem = w[:-2] if len(w) >= 6 else w[:-1]
            if len(stem) >= 4 and stem in norm_win:
                return True

    return False


# ---------------------------------------------------------------------------
# Diagnostics counters for the 6 validation stages (3.6)
# ---------------------------------------------------------------------------


@dataclass
class ValidationStats:
    """Per-stage drop and review counters for the deterministic validation checks."""

    dropped_schema_invalid: int = 0
    dropped_unknown_taxonomy: int = 0
    dropped_no_evidence: int = 0
    dropped_invalid_pair: int = 0
    dropped_duplicate: int = 0
    dropped_low_confidence: int = 0
    dropped_over_limit: int = 0
    flagged_no_lexical_anchor: int = 0
    dropped_generic_mention: int = 0
    # Internal audit trail of dropped generic mentions: (requirement_id, quote).
    dropped_generic_ids: list[tuple[str, str]] = field(default_factory=list)

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
        """Validate evidence spans and deduplicate by `requirement_id`."""
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
        """Validate and normalize all evidence quotes for a requirement."""
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
        """Execute all 6 deterministic validation checks + lexical competency check + headcount normalization:

        1. Schema check (element-wise Pydantic validation) -> `dropped_schema_invalid`
        2. Taxonomy existence & request subset check -> `dropped_unknown_taxonomy`
        3. Quote found in `brief_text` (normalized match + exact slice/offsets) -> `dropped_no_evidence`
        4. Role allowed for competency (`role_id in competency.roles`) -> `dropped_invalid_pair`
        4b. Non-destructive lexical anchor check: if neither competency name nor synonyms
            appear near `quote`, lower confidence and set `needs_review=True` (never drop!) -> `flagged_no_lexical_anchor`
        4c. Deterministic generic/passing-mention gate (ADR-006): a marker with no task cue
            is dropped -> `dropped_generic_mention`; a marker with a task cue is kept with
            `needs_review=True`.
        5. Deduplication -> `dropped_duplicate`
        6. `min_confidence` filter & `max_requirements` cap -> `dropped_over_limit`
        7. Role-level headcount normalization in code (`normalize_headcounts_by_role`).
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

        # Stage 2, 3, 4, 4b: Taxonomy check, Evidence quote check, Allowed pair check, Lexical anchor check
        stage4_valid: list[ExtractedRequirement] = []
        for req in schema_valid:
            role_id = req.role.role_id
            comp_id = req.competency.competency_id if req.competency else None

            ok, category, _reason = role_validator.check_pair(role_id, comp_id)
            if not ok and category == "unknown_taxonomy":
                stats.dropped_unknown_taxonomy += 1
                continue

            corrected_evidence = self._validate_evidence_spans(req.evidence, brief_text)
            if corrected_evidence is None:
                stats.dropped_no_evidence += 1
                continue

            if not ok and category == "invalid_pair":
                stats.dropped_invalid_pair += 1
                continue

            updated_req = req.model_copy(update={"evidence": corrected_evidence})
            if updated_req.competency is None:
                updated_req = updated_req.model_copy(update={"needs_review": True})
            else:
                comp_obj = taxonomy.get_competency(comp_id) if comp_id else None
                first_ev = corrected_evidence[0]
                if comp_obj is not None and not has_competency_lexical_anchor(
                    comp_obj,
                    first_ev.quote,
                    brief_text,
                    first_ev.char_start,
                    first_ev.char_end,
                ):
                    stats.flagged_no_lexical_anchor += 1
                    # Lower confidence without dropping below min_confidence (never delete!)
                    floor_conf = max(min_confidence, 0.56)
                    lowered_conf = max(
                        floor_conf, round(min(updated_req.confidence * 0.70, 0.58), 2)
                    )
                    assumptions = list(updated_req.assumptions)
                    lex_note = (
                        f"Lexical check: competency '{comp_obj.name_ru}' ({comp_obj.competency_id}) "
                        f"and its synonyms were not found near quote; confidence lowered and marked needs_review"
                    )
                    if lex_note not in assumptions and len(assumptions) < 10:
                        assumptions.append(lex_note[:300])
                    updated_req = updated_req.model_copy(
                        update={
                            "confidence": lowered_conf,
                            "needs_review": True,
                            "assumptions": assumptions[:10],
                            "role": updated_req.role.model_copy(
                                update={"confidence": lowered_conf}
                            ),
                            "competency": updated_req.competency.model_copy(
                                update={"confidence": lowered_conf}
                            ),
                        }
                    )

            # Stage 4c: deterministic generic/passing-mention gate (ADR-006 §3).
            # A marker with no concrete task cue -> drop; a marker with a task cue
            # -> keep but force needs_review=True and record the weak mention.
            first_quote = corrected_evidence[0].quote
            if has_generic_marker(first_quote):
                if is_generic_mention(first_quote):
                    stats.dropped_generic_mention += 1
                    stats.dropped_generic_ids.append(
                        (updated_req.requirement_id, first_quote)
                    )
                    continue
                generic_note = (
                    "Generic/passing technology mention (marker present) with a task cue: "
                    "kept but marked needs_review as weak evidence"
                )
                assumptions = list(updated_req.assumptions)
                if generic_note not in assumptions and len(assumptions) < 10:
                    assumptions.append(generic_note[:300])
                updated_req = updated_req.model_copy(
                    update={"needs_review": True, "assumptions": assumptions[:10]}
                )

            updated_req = role_validator._apply_review_flags(updated_req)
            stage4_valid.append(updated_req)

        # Stage 5: Deduplication
        # - Same (role_id, competency_id) is never repeated.
        # - Same competency_id with different roles is allowed ONLY if each role has its own distinct quote.
        # - If a role already has a specific competency (role_id, comp_id != ""), drop redundant (role_id, "") role-only items for the same quote.
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
            indexed = list(enumerate(conf_filtered))
            indexed.sort(key=lambda pair: (-pair[1].confidence, pair[0]))
            kept_indices = sorted(i for i, _ in indexed[:max_requirements])
            conf_filtered = [conf_filtered[i] for i in kept_indices]

        # Stage 7: Normalize headcounts by role in code (max among role mentions in text)
        conf_filtered = normalize_headcounts_by_role(conf_filtered, brief_text)

        return conf_filtered, stats

    def validate_partial_quotes(
        self,
        quotes: list[str],
        brief_text: str,
    ) -> list[bool]:
        """Check each quote independently (with normalization). Returns list of booleans."""
        return [find_quote_in_text(q, brief_text) is not None for q in quotes]
