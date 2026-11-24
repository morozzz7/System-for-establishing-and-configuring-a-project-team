"""Review routing for competency pairs that need human confirmation.

Every competency pair with `needs_review == true` is routed to the additive
`requirements_for_review` field, never to `/v1/optimize`, regardless of its
numeric confidence. A caller's `min_confidence` may lift an unanchored pair's
confidence above the old 0.60 cutoff; that must not let it bypass review.

Role-only requirements (`competency is None`) stay in the main list: a role
without a technology produces no pair false positive and is still useful for
staffing.

`REVIEW_CONFIDENCE_MAX` is kept as the named threshold from ADR-006 §2 for
reporting; routing itself is flag-based, not confidence-based.
"""

from __future__ import annotations

from .schemas import ExtractedRequirement

REVIEW_CONFIDENCE_MAX = 0.60


def split_for_review(
    requirements: list[ExtractedRequirement],
) -> tuple[list[ExtractedRequirement], list[ExtractedRequirement]]:
    """Split `requirements` into `(main, review)` preserving relative order.

    A competency pair goes to `review` iff it is flagged `needs_review`.
    """
    main: list[ExtractedRequirement] = []
    review: list[ExtractedRequirement] = []
    for req in requirements:
        if req.competency is not None and req.needs_review:
            review.append(req)
        else:
            main.append(req)
    return main, review
