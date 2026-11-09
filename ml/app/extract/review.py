"""Review routing for low-confidence competency pairs (ADR-006 §2).

A requirement with a competency, `needs_review == true` and
`confidence <= REVIEW_CONFIDENCE_MAX` is a low-confidence pair: it must not be
fed to `/v1/optimize` before a human confirms it. It is returned in the
additive `requirements_for_review` field instead of `requirements`.

Role-only requirements (`competency is None`) stay in the main list: a role
without a technology produces no pair false positive and is still useful for
staffing.

`REVIEW_CONFIDENCE_MAX` is a single named constant (starting hypothesis, same
class as the `METRICS.md` weights); it is calibrated against expert review
decisions later.
"""

from __future__ import annotations

from .schemas import ExtractedRequirement

REVIEW_CONFIDENCE_MAX = 0.60


def split_for_review(
    requirements: list[ExtractedRequirement],
) -> tuple[list[ExtractedRequirement], list[ExtractedRequirement]]:
    """Split `requirements` into `(main, review)` preserving relative order.

    A requirement goes to `review` iff it has a competency, is flagged
    `needs_review` and its confidence is `<= REVIEW_CONFIDENCE_MAX`.
    """
    main: list[ExtractedRequirement] = []
    review: list[ExtractedRequirement] = []
    for req in requirements:
        if (
            req.competency is not None
            and req.needs_review
            and req.confidence <= REVIEW_CONFIDENCE_MAX
        ):
            review.append(req)
        else:
            main.append(req)
    return main, review
