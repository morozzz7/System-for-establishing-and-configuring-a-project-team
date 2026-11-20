"""Post-processor: validate role-competency associations and flag review cases.

Validation gate (3.3 & ADR-005):
- A requirement with a competency is valid iff `role` exists in taxonomy (and in
  request `allowed_role_ids` if restricted), `competency` exists in taxonomy (and in
  request `allowed_competency_ids` if restricted), and `role_id in competency.roles`.
- `role.core_competencies` is NEVER a rejection gate; it is used only as a weak
  confidence signal (`needs_review = True` when `confidence < 0.75` and
  `competency_id not in role.core_competencies`).
- Roles are NEVER rewritten automatically (Principle 5 / ADR-005). Invalid pairs
  are rejected here (after LLM feedback retry in `llm_extract.py`) and counted in
  `dropped_count` (`diagnostics.post_processor_dropped`).
- Role-only requirements (`competency is None`) are kept only when `role_id` is
  valid, and are flagged with `needs_review = True`.
"""

from __future__ import annotations

from typing import Iterable

from .schemas import ExtractedRequirement
from .taxonomy import TaxonomyRegistry


class RoleCompetencyValidator:
    """Validate role-competency associations without automatic role rewriting."""

    def __init__(
        self,
        taxonomy: TaxonomyRegistry,
        *,
        allowed_role_ids: Iterable[str] | None = None,
        allowed_competency_ids: Iterable[str] | None = None,
    ) -> None:
        self.taxonomy = taxonomy
        self.allowed_role_ids: set[str] | None = (
            set(allowed_role_ids) if allowed_role_ids is not None else None
        )
        self.allowed_competency_ids: set[str] | None = (
            set(allowed_competency_ids) if allowed_competency_ids is not None else None
        )
        self.dropped_count = 0
        self.dropped_unknown_taxonomy = 0
        self.dropped_invalid_pair = 0

    def check_pair(
        self,
        role_id: str,
        comp_id: str | None,
    ) -> tuple[bool, str, str]:
        """Check whether `(role_id, comp_id)` is valid against taxonomy & request subset.

        Returns:
            (is_valid, category, reason)
            where category is "" | "unknown_taxonomy" | "invalid_pair".
        """
        role = self.taxonomy.get_role(role_id)
        if role is None:
            return (
                False,
                "unknown_taxonomy",
                f"role_id '{role_id}' does not exist in taxonomy.",
            )
        if self.allowed_role_ids is not None and role_id not in self.allowed_role_ids:
            return (
                False,
                "unknown_taxonomy",
                f"role_id '{role_id}' is not in request taxonomy.role_ids {sorted(self.allowed_role_ids)}.",
            )

        if comp_id is None:
            return True, "", ""

        comp = self.taxonomy.get_competency(comp_id)
        if comp is None:
            return (
                False,
                "unknown_taxonomy",
                f"competency_id '{comp_id}' does not exist in taxonomy.",
            )
        if (
            self.allowed_competency_ids is not None
            and comp_id not in self.allowed_competency_ids
        ):
            return (
                False,
                "unknown_taxonomy",
                f"competency_id '{comp_id}' is not in request taxonomy.competency_ids.",
            )

        if role_id not in comp.roles:
            allowed = [
                r
                for r in comp.roles
                if self.allowed_role_ids is None or r in self.allowed_role_ids
            ]
            return (
                False,
                "invalid_pair",
                f"role '{role_id}' is not allowed for competency '{comp_id}'. Allowed roles: {allowed}.",
            )

        return True, "", ""

    def validate_and_fix(
        self, requirements: list[ExtractedRequirement]
    ) -> list[ExtractedRequirement]:
        """Validate role-competency associations and apply review flags.

        Never rewrites roles automatically. Invalid requirements are dropped
        and counted in `self.dropped_count`.
        """
        self.dropped_count = 0
        self.dropped_unknown_taxonomy = 0
        self.dropped_invalid_pair = 0
        valid: list[ExtractedRequirement] = []

        for req in requirements:
            validated = self._validate_single(req)
            if validated is not None:
                valid.append(validated)
            else:
                self.dropped_count += 1

        return valid

    def _validate_single(
        self, req: ExtractedRequirement
    ) -> ExtractedRequirement | None:
        """Validate a single requirement without rewriting role_id."""
        role_id = req.role.role_id
        comp_id = req.competency.competency_id if req.competency else None

        ok, category, _reason = self.check_pair(role_id, comp_id)
        if not ok:
            if category == "unknown_taxonomy":
                self.dropped_unknown_taxonomy += 1
            elif category == "invalid_pair":
                self.dropped_invalid_pair += 1
            return None

        # Role-only requirement — keep but force review
        if req.competency is None:
            updated = req.model_copy(update={"needs_review": True})
            return self._apply_review_flags(updated)

        return self._apply_review_flags(req)

    def _apply_review_flags(
        self, req: ExtractedRequirement
    ) -> ExtractedRequirement:
        """Set needs_review for weak-confidence or contradictory signals.

        Signals:
        - confidence < 0.6
        - weak non-core signal: confidence < 0.75 and competency_id not in role.core_competencies
        - ambiguous evidence (quote shorter than 3 chars)
        - contradictory signals: junior (level <= 2) + architect/lead/principal
        - unclear effort assumptions
        """
        needs_review = req.needs_review or req.confidence < 0.6

        role_id = req.role.role_id
        role = self.taxonomy.get_role(role_id)
        if (
            req.competency is not None
            and role is not None
            and req.confidence < 0.75
            and req.competency.competency_id not in role.core_competencies
        ):
            needs_review = True

        # Ambiguous evidence: raw quote shorter than the contract minLength=3
        if req.evidence:
            all_short = all(len(ev.quote) < 3 for ev in req.evidence)
            if all_short:
                needs_review = True

        # Contradictory signals: junior + architect/lead/principal
        level = req.level
        text = " ".join(req.assumptions).lower() + " " + role_id.lower()
        has_architect = (
            "architect" in text
            or "архитектор" in text
            or role_id == "solution_architect"
        )
        has_lead = (
            "lead" in text
            or "лид" in text
            or "principal" in text
            or "принципал" in text
        )
        if level <= 2 and (has_architect or has_lead):
            needs_review = True

        # Unclear effort assumptions: no timeline-related assumption
        assumptions_text = " ".join(req.assumptions).lower()
        has_effort_assumption = any(
            keyword in assumptions_text
            for keyword in (
                "недел",
                "месяц",
                "дн",
                "person-days",
                "timeline",
                "срок",
                "default",
                "estimate",
                "оценка",
            )
        )
        if not has_effort_assumption:
            needs_review = True

        if needs_review == req.needs_review:
            return req

        return req.model_copy(update={"needs_review": needs_review})
