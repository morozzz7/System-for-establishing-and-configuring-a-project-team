"""Word-boundary candidate competency scanner, role safety-net scanner, and role headcount normalizer.

Implements:
- Word-boundary candidate competency scanning (`scan_unmatched_competencies`)
- Word-boundary missing-role scanning (`scan_missing_roles`)
- Code-side role headcount normalization (`extract_role_headcounts_from_text`,
  `normalize_headcounts_by_role`): computes the maximum headcount across all
  mentions of each role in the brief text rather than trusting the LLM's numbers.
- Verification prompt builder (`build_verification_prompt`) with explicit per-candidate
  decision and reason logging.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from typing import Any, Iterable

from .schemas import ExtractedRequirement
from .taxonomy import TaxonomyRegistry

# ---------------------------------------------------------------------------
# Constants & stop-lists for word-boundary scanning
# ---------------------------------------------------------------------------

_EXCLUDED_LOWER_SYNONYMS: frozenset[str] = frozenset(
    {
        "го",
        "ba",
        "sa",
        "pm",
        "ds",
        "ch",
        "sh",
        "ts",
        "cv",
        "ui",
        "ux",
        "soc",
        "git",
    }
)

_CASE_SENSITIVE_ACRONYMS: frozenset[str] = frozenset(
    {
        "Go",
        "BA",
        "SA",
        "PM",
        "DS",
        "TS",
        "UI",
        "UX",
        "QA",
        "BI",
        "ML",
        "CV",
        "NLP",
        "SQL",
        "ETL",
        "DWH",
        "IAM",
        "PKI",
        "DDD",
        "C4",
        "AWS",
        "GCP",
        "ESB",
        "API",
        "REST",
        "gRPC",
        "S3",
        "1C",
        "1С",
        "WMS",
        "TMS",
    }
)

_GENERIC_PROSE_COMP_SYNONYMS: frozenset[str] = frozenset(
    {
        "требования",
        "требований",
        "техническое задание",
        "тз",
        "оценка",
        "оценка трудозатрат",
        "риски",
        "коммуникации",
        "коммуникация",
        "переговоры",
        "менторство",
        "наставничество",
        "код-ревью",
        "ревью кода",
        "метрики",
        "мониторинг",
        "логирование",
        "логи",
        "трассировка",
        "дашборды",
        "дашборд",
        "витрины",
        "отчёты",
        "отчеты",
        "тестирование",
        "автотесты",
        "контейнеры",
        "контейнеризация",
        "облако",
        "интеграции",
        "интеграция",
        "архитектура",
        "масштабирование",
        "высоконагруженные системы",
        "отказоустойчивость",
        "аутентификация",
        "авторизация",
        "безопасность",
        "шифрование",
        "сертификаты",
        "вероятность",
        "статистика",
        "каталог",
        "корзина",
        "магазин",
        "доставка",
        "склад",
        "платежи",
        "эквайринг",
        "тарифы",
        "биллинг",
        "прототипы",
        "прототипирование",
        "макеты",
        "вёрстка",
        "верстка",
        "стили",
        "скрипты",
        "пайплайны",
        "конвейеры",
        "оркестрация",
        "очереди",
        "брокер сообщений",
        "телеком",
        "оператор связи",
        "критерии приёмки",
        "нагрузка",
        "нагрузку",
        "нагрузки",
    }
)

_CYRILLIC_WORD_RE = re.compile(r"^[а-яё]+$", re.IGNORECASE)
_SENTENCE_SPLIT_RE = re.compile(r"(?<=[.!?])\s+|\n+")

_ROLE_PATTERNS: dict[str, list[re.Pattern[str]]] = {
    "backend_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:бэкенд[- ]?разработчик[а-яё]*|backend[- ]?разработчик[а-яё]*|"
            r"бэкенд[- ]?команд[а-яё]*|бэкендер[а-яё]*|backend\s+developer[s]?|"
            r"go[- ]?разработчик[а-яё]*|java[- ]?разработчик[а-яё]*|python[- ]?разработчик[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "frontend_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:фронтенд[- ]?разработчик[а-яё]*|frontend[- ]?разработчик[а-яё]*|"
            r"фронтендер[а-яё]*|frontend\s+developer[s]?|мобильн[а-яё]+\s+разработчик[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "qa_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:qa[- ]?инженер[а-яё]*|инженер[а-яё]*\s+по\s+тестированию|"
            r"тестировщик[а-яё]*|автотестировщик[а-яё]*|qa\s+engineer[s]?)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
        re.compile(r"(?<![а-яёa-z0-9_])QA(?![а-яёa-z0-9_])"),
    ],
    "devops_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:devops[- ]?инженер[а-яё]*|devops\s+engineer[s]?|"
            r"sre[- ]?инженер[а-яё]*|инженер[а-яё]*\s+инфраструктур[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
        re.compile(r"(?<![а-яёa-z0-9_])(?:DevOps|SRE)(?![а-яёa-z0-9_])"),
    ],
    "data_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:дата[- ]?инженер[а-яё]*|инженер[а-яё]*\s+данных|"
            r"data\s+engineer[s]?|разработчик[а-яё]*\s+dwh|etl[- ]?разработчик[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "ml_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:ml[- ]?инженер[а-яё]*|ml\s+engineer[s]?|"
            r"data\s+scientist[s]?|специалист[а-яё]*\s+по\s+(?:машинному\s+обучению|nlp|ml))"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "business_analyst": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:бизнес[- ]?аналитик[а-яё]*|business\s+analyst[s]?|"
            r"продуктов[а-яё]+\s+аналитик[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "system_analyst": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:системн[а-яё]+\s+аналитик[а-яё]*|system\s+analyst[s]?)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "project_manager": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:руководител[а-яё]+\s+проект[а-яё]*|менеджер[а-яё]*\s+проект[а-яё]*|"
            r"project\s+manager[s]?|scrum[- ]?мастер[а-яё]*|scrum\s+master[s]?|"
            r"деливери[- ]?менеджер[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "ux_ui_designer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:ux/ui[- ]?дизайнер[а-яё]*|ui/ux[- ]?дизайнер[а-яё]*|"
            r"продуктов[а-яё]+\s+дизайнер[а-яё]*|дизайнер[а-яё]*\s+интерфейс[а-яё]*|"
            r"веб[- ]?дизайнер[а-яё]*|ux[- ]?дизайнер[а-яё]*|ui[- ]?дизайнер[а-яё]*|дизайнер[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "solution_architect": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:архитектор[а-яё]*\s+решени[а-яё]*|solution\s+architect[s]?|"
            r"ведущ[а-яё]+\s+архитектор[а-яё]*|системн[а-яё]+\s+архитектор[а-яё]*|"
            r"ит[- ]?архитектор[а-яё]*|архитектор[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
    "security_engineer": [
        re.compile(
            r"(?<![а-яёa-z0-9_])(?:инженер[а-яё]*\s+по\s+(?:информационной\s+)?безопасности|"
            r"специалист[а-яё]*\s+по\s+(?:информационной\s+безопасности|иб)|"
            r"security\s+engineer[s]?|appsec[- ]?инженер[а-яё]*|безопасник[а-яё]*)"
            r"(?![а-яёa-z0-9_])",
            re.IGNORECASE,
        ),
    ],
}


# ---------------------------------------------------------------------------
# Data structures
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class CompetencyCandidateMention:
    """A taxonomy competency found in brief_text by the word-boundary scanner."""

    competency_id: str
    name_ru: str
    matched_text: str
    quote_sentence: str
    allowed_roles: list[str]


@dataclass(frozen=True)
class RoleMention:
    """A taxonomy role found in brief_text by the word-boundary scanner."""

    role_id: str
    name_ru: str
    matched_text: str
    quote_sentence: str
    allowed_competencies: list[str]


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _sentences_with_offsets(text: str) -> list[tuple[str, int, int]]:
    """Split text into sentence/line spans `(sentence_str, start, end)`."""
    spans: list[tuple[str, int, int]] = []
    start = 0
    for m in _SENTENCE_SPLIT_RE.finditer(text):
        end = m.start()
        if end > start:
            s = text[start:end].strip()
            if len(s) >= 3:
                s_start = text.find(s, start, end)
                spans.append((s, s_start, s_start + len(s)))
        start = m.end()
    if start < len(text):
        s = text[start:].strip()
        if len(s) >= 3:
            s_start = text.find(s, start)
            spans.append((s, s_start, s_start + len(s)))
    return spans


def _sentence_for_offset(
    sentences: list[tuple[str, int, int]], pos: int, fallback: str
) -> str:
    for s, start, end in sentences:
        if start <= pos <= end:
            return s[:300]
    return fallback[:300]


def _build_word_boundary_pattern(
    term: str, *, case_sensitive: bool = False
) -> re.Pattern[str] | None:
    """Compile a strict word-boundary regex for `term`.

    The lookbehind also excludes `-` so hyphenated proper names such as
    «Север-Телеком» do not yield a `телеком` competency match (ADR-006 §4).
    Only `scan_unmatched_competencies` uses this builder; role patterns are
    defined separately in `_ROLE_PATTERNS` and are intentionally unchanged.
    """
    cleaned = term.strip()
    if len(cleaned) < 2:
        return None
    if cleaned.lower() in _EXCLUDED_LOWER_SYNONYMS and not case_sensitive:
        return None

    escaped = re.escape(cleaned)
    words = cleaned.split()
    if len(words) == 1 and _CYRILLIC_WORD_RE.match(cleaned) and len(cleaned) >= 5:
        if cleaned[-1].lower() in "аяоеиыуюьй":
            stem = re.escape(cleaned[:-1])
            body = rf"{stem}[а-яё]{{1,4}}"
        else:
            body = rf"{escaped}[а-яё]{{0,3}}"
    else:
        body = escaped

    flags = re.UNICODE if case_sensitive else (re.UNICODE | re.IGNORECASE)
    return re.compile(rf"(?<![а-яёa-z0-9_-]){body}(?![а-яёa-z0-9_])", flags)


# ---------------------------------------------------------------------------
# 1. Word-boundary candidate competency scanner
# ---------------------------------------------------------------------------


def scan_unmatched_competencies(
    brief_text: str,
    taxonomy: TaxonomyRegistry,
    already_extracted_comp_ids: set[str],
    *,
    allowed_role_ids: Iterable[str] | None = None,
    allowed_competency_ids: Iterable[str] | None = None,
) -> list[CompetencyCandidateMention]:
    """Scan `brief_text` on strict word boundaries for taxonomy competencies not in `already_extracted_comp_ids`."""
    role_filter = set(allowed_role_ids) if allowed_role_ids is not None else None
    comp_filter = (
        set(allowed_competency_ids) if allowed_competency_ids is not None else None
    )
    sentences = _sentences_with_offsets(brief_text)
    found: list[CompetencyCandidateMention] = []

    for cid in sorted(taxonomy.competencies.keys()):
        if cid in already_extracted_comp_ids:
            continue
        if comp_filter is not None and cid not in comp_filter:
            continue

        comp = taxonomy.competencies[cid]
        allowed_roles = [
            r for r in comp.roles if (role_filter is None or r in role_filter)
        ]
        if not allowed_roles:
            continue

        terms: list[tuple[str, bool]] = []
        for raw_term in [comp.name_en, comp.name_ru, *comp.synonyms]:
            t = raw_term.strip()
            if not t:
                continue
            t_low = t.lower()
            if t_low in _GENERIC_PROSE_COMP_SYNONYMS:
                continue
            if t in _CASE_SENSITIVE_ACRONYMS:
                terms.append((t, True))
            elif t_low in _EXCLUDED_LOWER_SYNONYMS:
                if (
                    t.upper() in _CASE_SENSITIVE_ACRONYMS
                    or t.title() in _CASE_SENSITIVE_ACRONYMS
                ):
                    terms.append((t.upper() if len(t) <= 3 else t, True))
            else:
                terms.append((t, False))

        terms.sort(key=lambda x: len(x[0]), reverse=True)

        matched_span: tuple[str, str] | None = None
        for term, is_cs in terms:
            pat = _build_word_boundary_pattern(term, case_sensitive=is_cs)
            if pat is None:
                continue
            m = pat.search(brief_text)
            if m:
                matched_str = brief_text[m.start() : m.end()]
                sent = _sentence_for_offset(sentences, m.start(), matched_str)
                matched_span = (matched_str, sent)
                break

        if matched_span is not None:
            found.append(
                CompetencyCandidateMention(
                    competency_id=cid,
                    name_ru=comp.name_ru,
                    matched_text=matched_span[0],
                    quote_sentence=matched_span[1],
                    allowed_roles=allowed_roles,
                )
            )

    return found


# ---------------------------------------------------------------------------
# 2. Word-boundary missing role scanner (Role Safety Net)
# ---------------------------------------------------------------------------


def scan_missing_roles(
    brief_text: str,
    taxonomy: TaxonomyRegistry,
    already_extracted_role_ids: set[str],
    *,
    allowed_role_ids: Iterable[str] | None = None,
    allowed_competency_ids: Iterable[str] | None = None,
) -> list[RoleMention]:
    """Scan `brief_text` on strict word boundaries for taxonomy roles not yet in `already_extracted_role_ids`."""
    role_filter = set(allowed_role_ids) if allowed_role_ids is not None else None
    comp_filter = (
        set(allowed_competency_ids) if allowed_competency_ids is not None else None
    )
    sentences = _sentences_with_offsets(brief_text)
    missing: list[RoleMention] = []

    comps_by_role: dict[str, list[str]] = {rid: [] for rid in taxonomy.roles}
    for cid, comp in taxonomy.competencies.items():
        if comp_filter is not None and cid not in comp_filter:
            continue
        for rid in comp.roles:
            if rid in comps_by_role:
                comps_by_role[rid].append(cid)

    for rid in sorted(taxonomy.roles.keys()):
        if rid in already_extracted_role_ids:
            continue
        if role_filter is not None and rid not in role_filter:
            continue

        role = taxonomy.roles[rid]
        patterns = _ROLE_PATTERNS.get(rid, [])
        matched_hit: tuple[str, str] | None = None

        for pat in patterns:
            for m in pat.finditer(brief_text):
                matched_str = brief_text[m.start() : m.end()]
                sent = _sentence_for_offset(sentences, m.start(), matched_str)
                sent_low = sent.lower()
                if "руководство системного администратора" in sent_low:
                    continue
                if rid == "solution_architect" and not re.search(
                    r"(?<![а-яёa-z0-9_])(?:архитектор[а-яё]*|solution\s+architect[s]?)(?![а-яёa-z0-9_])",
                    matched_str,
                    re.IGNORECASE,
                ):
                    continue
                matched_hit = (matched_str, sent)
                break
            if matched_hit is not None:
                break

        if matched_hit is not None:
            missing.append(
                RoleMention(
                    role_id=rid,
                    name_ru=role.name_ru,
                    matched_text=matched_hit[0],
                    quote_sentence=matched_hit[1],
                    allowed_competencies=sorted(comps_by_role.get(rid, [])),
                )
            )

    return missing


# ---------------------------------------------------------------------------
# 3. Role-level Headcount Extraction & Normalization in Code
# ---------------------------------------------------------------------------

_RU_NUM_WORDS: dict[str, int] = {
    "один": 1,
    "одна": 1,
    "одного": 1,
    "одному": 1,
    "пара": 2,
    "пару": 2,
    "пары": 2,
    "два": 2,
    "две": 2,
    "двух": 2,
    "двое": 2,
    "двоих": 2,
    "три": 3,
    "трех": 3,
    "трёх": 3,
    "трое": 3,
    "троих": 3,
    "четыре": 4,
    "четырех": 4,
    "четырёх": 4,
    "четверо": 4,
    "четверых": 4,
    "пять": 5,
    "пятеро": 5,
    "пятерых": 5,
    "шесть": 6,
    "шестеро": 6,
    "семь": 7,
    "восемь": 8,
    "девять": 9,
    "десять": 10,
}

_ROLE_HEADCOUNT_NOUNS: dict[str, str] = {
    "backend_engineer": r"(?:бэкенд[- ]?разработчик[а-яё]*|backend[- ]?разработчик[а-яё]*|go[- ]?разработчик[а-яё]*|java[- ]?разработчик[а-яё]*|python[- ]?разработчик[а-яё]*|бэкендер[а-яё]*|бэкенд(?=\s*\())",
    "frontend_engineer": r"(?:фронтенд[- ]?разработчик[а-яё]*|frontend[- ]?разработчик[а-яё]*|мобильн[а-яё]+\s+разработчик[а-яё]*|фронтендер[а-яё]*|фронтенд(?=\s*(?:\(|middle|senior|junior)))",
    "qa_engineer": r"(?:тестировщик[а-яё]*|qa[- ]?инженер[а-яё]*|инженер[а-яё]*\s+по\s+тестированию|qa(?=\s*\())",
    "devops_engineer": r"(?:devops[- ]?инженер[а-яё]*|devops(?=\s*(?:\(|для|middle|senior))|sre)",
    "data_engineer": r"(?:дата[- ]?инженер[а-яё]*|инженер[а-яё]*\s+данных|data\s+engineer[s]?)",
    "ml_engineer": r"(?:ml[- ]?инженер[а-яё]*|ml\s+engineer[s]?|data\s+scientist[s]?)",
    "business_analyst": r"(?:бизнес[- ]?аналитик[а-яё]*|business\s+analyst[s]?)",
    "system_analyst": r"(?:системн[а-яё]+\s+аналитик[а-яё]*|system\s+analyst[s]?)",
    "project_manager": r"(?:руководител[а-яё]+\s+проект[а-яё]*|менеджер[а-яё]*\s+проект[а-яё]*|project\s+manager[s]?)",
    "ux_ui_designer": r"(?:ux/ui[- ]?дизайнер[а-яё]*|ui/ux[- ]?дизайнер[а-яё]*|дизайнер[а-яё]*)",
    "solution_architect": r"(?:архитектор[а-яё]*\s+решени[а-яё]*|ведущ[а-яё]+\s+архитектор[а-яё]*|архитектор[а-яё]*)",
    "security_engineer": r"(?:инженер[а-яё]*\s+по\s+(?:информационной\s+)?безопасности|специалист[а-яё]*\s+по\s+иб|security\s+engineer[s]?)",
}


def detect_headcount_for_role_near_quote(
    brief_text: str,
    quote: str,
    role_id: str,
) -> int | None:
    """Check the sentence containing `quote` for an explicit headcount tied to `role_id`."""
    role_noun_pat = _ROLE_HEADCOUNT_NOUNS.get(role_id)
    if not role_noun_pat or not quote:
        return None

    sentences = _sentences_with_offsets(brief_text)
    pos = brief_text.find(quote)
    if pos == -1:
        context = quote
    else:
        context = _sentence_for_offset(sentences, pos, quote)

    return _scan_max_headcount_for_role_in_span(context, role_noun_pat)


def _scan_max_headcount_for_role_in_span(text: str, role_noun_pat: str) -> int | None:
    """Return the maximum explicit headcount attached to `role_noun_pat` inside `text`, or `None`."""
    found_counts: list[int] = []

    # 1. Digit before role: e.g. "2 бэкенд-разработчика", "3 Go-разработчика"
    digit_re = re.compile(
        rf"(?<![а-яёa-z0-9_])(\d{{1,2}})\s+(?:[а-яёa-z-]+\s+){{0,2}}{role_noun_pat}(?![а-яёa-z0-9_])",
        re.IGNORECASE,
    )
    for m in digit_re.finditer(text):
        val = int(m.group(1))
        if 1 <= val <= 20:
            found_counts.append(val)

    # 2. Russian number word before role: e.g. "два бэкенд-разработчика", "пара бэкенд-разработчиков", "трое фронтенд-разработчиков"
    num_words = "|".join(_RU_NUM_WORDS.keys())
    word_re = re.compile(
        rf"(?<![а-яёa-z0-9_])({num_words})\s+(?:[а-яёa-z-]+\s+){{0,2}}{role_noun_pat}(?![а-яёa-z0-9_])",
        re.IGNORECASE,
    )
    for m2 in word_re.finditer(text):
        word = m2.group(1).lower()
        val2 = _RU_NUM_WORDS.get(word)
        if val2 is not None:
            found_counts.append(val2)

    return max(found_counts) if found_counts else None


def extract_role_headcounts_from_text(brief_text: str) -> dict[str, int]:
    """Scan the entire `brief_text` and return `{role_id: max_headcount}` for roles with explicit counts."""
    if not brief_text:
        return {}
    result: dict[str, int] = {}
    for role_id, role_noun_pat in _ROLE_HEADCOUNT_NOUNS.items():
        max_hc = _scan_max_headcount_for_role_in_span(brief_text, role_noun_pat)
        if max_hc is not None:
            result[role_id] = max_hc
    return result


def normalize_headcounts_by_role(
    requirements: list[ExtractedRequirement],
    brief_text: str,
) -> list[ExtractedRequirement]:
    """Normalize `headcount` across all requirements of the same `role_id` in code.

    Rule:
    - For each `role_id`, determine the maximum headcount across all explicit mentions
      of that role in `brief_text` (`extract_role_headcounts_from_text`).
    - If `brief_text` is non-trivial (>= 40 chars), the code-determined role headcount
      is `text_role_hc.get(role_id, 1)` (maximum among text mentions of the role,
      defaulting to 1 when no plural count is stated in the text — never trusting
      unverified model headcounts).
    - For tiny synthetic test strings (< 40 chars) without role nouns, fall back to
      `max(r.headcount for r in role_reqs)`.
    - Every requirement of `role_id` receives this normalized `role_hc`.
    """
    if not requirements:
        return []

    text_role_hc = extract_role_headcounts_from_text(brief_text)
    has_substantial_text = len(brief_text.strip()) >= 40

    max_model_hc_by_role: dict[str, int] = {}
    for r in requirements:
        rid = r.role.role_id
        max_model_hc_by_role[rid] = max(max_model_hc_by_role.get(rid, 1), r.headcount)

    normalized: list[ExtractedRequirement] = []
    for r in requirements:
        rid = r.role.role_id
        if rid in text_role_hc:
            target_hc = text_role_hc[rid]
        elif has_substantial_text:
            target_hc = 1
        else:
            target_hc = max_model_hc_by_role.get(rid, 1)

        if r.headcount != target_hc:
            assumptions = list(r.assumptions)
            note = (
                f"Headcount normalized by role '{rid}' in code to {target_hc} "
                f"(model had {r.headcount})"
            )
            if note not in assumptions and len(assumptions) < 10:
                assumptions.append(note[:300])
            r = r.model_copy(
                update={
                    "headcount": target_hc,
                    "assumptions": assumptions[:10],
                }
            )
        normalized.append(r)

    return normalized


# ---------------------------------------------------------------------------
# 4. Second LLM call prompt builder for candidate & role verification
# ---------------------------------------------------------------------------


def build_verification_prompt(
    brief_text: str,
    candidate_competencies: list[CompetencyCandidateMention],
    missing_roles: list[RoleMention],
    locale: str = "ru",
) -> list[dict[str, str]]:
    """Build a compact prompt for the second LLM call to verify candidates and missing roles."""
    cand_payload = [
        {
            "competency_id": c.competency_id,
            "name_ru": c.name_ru,
            "matched_in_text": c.matched_text,
            "sentence": c.quote_sentence,
            "allowed_roles": c.allowed_roles,
        }
        for c in candidate_competencies
    ]
    role_payload = [
        {
            "role_id": r.role_id,
            "name_ru": r.name_ru,
            "matched_in_text": r.matched_text,
            "sentence": r.quote_sentence,
            "allowed_competencies": r.allowed_competencies,
        }
        for r in missing_roles
    ]

    system_text = (
        "You are a precision verification assistant for project team formation.\n"
        "A word-boundary scanner found additional technology/competency mentions and/or role mentions in the project brief that were not included in the initial extraction.\n"
        "Your task is to inspect EVERY candidate in the context of the brief and return an explicit decision (`\"confirm\"` or `\"reject\"`) with a short `\"reason\"` for EVERY candidate.\n\n"
        "## Strict Verification Rules\n"
        "1. **REJECT (`decision: \"reject\"`) any technology or competency that is:**\n"
        "   - Legacy system kept unchanged, decommissioned, or not modified in this project (e.g., 'остаётся без изменений', 'не дорабатывается', 'будет выведен из эксплуатации', 'после миграции отключается').\n"
        "   - Rejected or discarded alternative/prototype (e.g., 'отказались от', 'отброшен', 'были отключены', 'не переносим', 'не подходит', 'не рассматривается').\n"
        "   - Explicitly forbidden or excluded (e.g., 'запрещено', 'без смешивания с ...', 'не предусмотрен', 'не требуется').\n"
        "   - Done entirely by the customer, vendor, or an external security/ops team (e.g., 'пишет сам заказчик', 'пишут аналитики заказчика', 'остаётся у вендора', 'остаётся в зоне ответственности службы ИБ заказчика').\n"
        "   - Only listed in a generic 'Стек: ...' summary line or passing remark ('разумеется, Docker, как у всех') without a concrete task.\n"
        "2. **CONFIRM (`decision: \"confirm\"`) a candidate competency ONLY if:**\n"
        "   - A member of the project team being formed actually needs this competency to perform project work.\n"
        "   - You MUST choose `role_id` from the candidate's `allowed_roles` list and provide an exact `quote` substring from the brief.\n"
        "3. **For each candidate in `missing_roles`:**\n"
        "   - If the role IS required on the project team, output `decision: \"confirm\"`, `role_id`, `competency_id` (chosen from `allowed_competencies` ONLY if that specific competency's name/synonym appears in the brief for that role; otherwise `null`), and an exact `quote` from the brief.\n"
        "   - If the role is NOT required on the project team, output `decision: \"reject\"`.\n\n"
        "## Output Format\n"
        "Output ONLY a JSON array containing one object per candidate. Each object must have:\n"
        "{\n"
        '  "decision": "confirm" | "reject",\n'
        '  "role_id": "<role_id>",\n'
        '  "competency_id": "<competency_id or null>",\n'
        '  "quote": "<exact substring from brief, required when decision==confirm>",\n'
        '  "level": 3,\n'
        '  "criticality": "must" | "should" | "nice",\n'
        '  "headcount": 1,\n'
        '  "confidence": 0.85,\n'
        '  "needs_review": false,\n'
        '  "reason": "<short explanation of why confirmed or rejected>"\n'
        "}\n"
        "Output ONLY the JSON array."
    )

    user_text = (
        f"Candidate competencies to verify ({len(cand_payload)}):\n{json.dumps(cand_payload, ensure_ascii=False, indent=2)}\n\n"
        f"Missing roles to verify ({len(role_payload)}):\n{json.dumps(role_payload, ensure_ascii=False, indent=2)}\n\n"
        f"Full brief text (locale={locale}):\n\n{brief_text}"
    )

    return [
        {"role": "system", "content": system_text},
        {"role": "user", "content": user_text},
    ]
