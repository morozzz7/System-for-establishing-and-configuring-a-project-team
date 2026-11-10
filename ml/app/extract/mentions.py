"""Deterministic generic / passing technology-mention gate (ADR-006 §3).

A requirement whose evidence quote contains a generic marker (a conversational
remark or a generic stack summary) and no concrete task cue is not a project
requirement. The gate is deterministic and independent of the LLM: the
`extract-v3` prompt already forbids such items, but an instruction is not a
guarantee (see `devops_engineer/cloud_docker` from «разумеется, Docker, как у
всех»).

Normalization: case-folded, `ё -> е`.
"""

from __future__ import annotations

# Conversational / generic-stack markers (ADR-006 §3). All lowercase, `ё -> е`.
GENERIC_MARKERS: tuple[str, ...] = (
    "разумеется",
    "как у всех",
    "как обычно",
    "как всегда",
    "само собой",
    "по умолчанию",
    "как все",
    "очевидно",
    "общеизвестно",
    "всем известно",
    "стек:",
    "технологический стек",
    "общий стек",
    "стек компании",
)

# Concrete-task cue stems (ADR-006 §3). Presence of any of them means the
# sentence describes an actual project task, so the item is kept (with
# `needs_review=True`) instead of being dropped.
TASK_CUE_STEMS: tuple[str, ...] = (
    "настра",
    "разворач",
    "разверн",
    "собира",
    "собер",
    "пиш",
    "напиш",
    "разраб",
    "внедр",
    "поддерж",
    "эксплуат",
    "управля",
    "описыва",
    "опиш",
    "тестир",
    "проверя",
    "мигрир",
    "перенос",
    "депло",
    "резервн",
    "реплик",
    "обновля",
    "монитор",
    "наблюда",
    "дашборд",
    "кластер",
    "балансир",
    "шифр",
    "аутентифик",
    "авториз",
    "интегр",
    "проектир",
    "документир",
    "конфигурир",
    "настрой",
)


def _normalize(text: str) -> str:
    """Case-fold and fold `ё -> е` for marker/cue matching."""
    return text.casefold().replace("ё", "е")


def has_generic_marker(quote: str) -> bool:
    """Return True if `quote` contains a generic/passing-mention marker."""
    if not quote:
        return False
    norm = _normalize(quote)
    return any(marker in norm for marker in GENERIC_MARKERS)


def has_task_cue(quote: str) -> bool:
    """Return True if `quote` contains a concrete-task cue stem."""
    if not quote:
        return False
    norm = _normalize(quote)
    return any(cue in norm for cue in TASK_CUE_STEMS)


def is_generic_mention(quote: str) -> bool:
    """Return True iff `quote` has a generic marker and no concrete task cue."""
    return has_generic_marker(quote) and not has_task_cue(quote)
