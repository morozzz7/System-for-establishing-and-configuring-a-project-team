"""Effort estimator v2: parse timeline + team size, allocate with calibrated weights.

Calibrated on gold briefs (eval/gold). Always returns a three-point range
(min, likely, max). Falls back to per-role defaults only when no timeline is found.
Decoupled from `rules.py` (3.7 / ADR-005).
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field

from .schemas import EffortRange, EvidenceSpan, ExtractCompetency, ExtractRole


@dataclass
class PartialRequirement:
    """A partial requirement before effort estimation."""

    requirement_id: str
    role: ExtractRole
    competency: ExtractCompetency | None
    level: int
    criticality: str  # "must" | "should" | "nice"
    headcount: int
    evidence: list[EvidenceSpan]
    confidence: float
    assumptions: list[str] = field(default_factory=list)


# ---------------------------------------------------------------------------
# Timeline regex patterns (Russian + English)
# ---------------------------------------------------------------------------

_TIMELINE_PATTERNS: list[tuple[re.Pattern[str], str]] = [
    # Months (Russian)
    (re.compile(r"(\d+(?:[.,]\d+)?)\s*(?:месяц(?:а|ев)?|мес\.?)\b", re.IGNORECASE), "month"),
    # Months (English)
    (re.compile(r"(\d+(?:[.,]\d+)?)\s*months?\b", re.IGNORECASE), "month"),
    # Weeks (Russian)
    (re.compile(r"(\d+(?:[.,]\d+)?)\s*недел(?:ю|и|ь|ями|ях|ям)?\b", re.IGNORECASE), "week"),
    # Weeks (English)
    (re.compile(r"(\d+(?:[.,]\d+)?)\s*weeks?\b", re.IGNORECASE), "week"),
    # Days (Russian)
    (re.compile(r"(\d+(?:[.,]\d+)?)\s*(?:рабочих\s*)?(?:дней|дня|день|дн\.)\b", re.IGNORECASE), "day"),
    # Days (English)
    (re.compile(r"(\d+(?:[.,]\d+)?)\s*(?:working\s*)?days?\b", re.IGNORECASE), "day"),
]

# Working days per unit
_DAYS_PER_UNIT = {
    "month": 21,  # matches gold calibration
    "week": 5,
    "day": 1,
}

# Team size regex patterns (Russian + English)
_TEAM_SIZE_PATTERNS: list[re.Pattern[str]] = [
    # "команда 2-3 человека", "команда из 5 человек", "team of 3-5 people"
    re.compile(r"(?:команда|team)\s*(?:из\s*)?(\d+)(?:\s*[-–—]\s*(\d+))?\s*(?:человек[а-я]*|people|person|сотрудник[а-я]*|specialist|специалист[а-я]*)", re.IGNORECASE),
    # "2-3 человека", "5 people"
    re.compile(r"(\d+)\s*[-–—]\s*(\d+)\s*(?:человек[а-я]*|people|person|сотрудник[а-я]*|specialist|специалист[а-я]*)", re.IGNORECASE),
    # "1 человек", "3 people" (single number)
    re.compile(r"(\d+)\s*(?:человек[а-я]*|people|person|сотрудник[а-я]*|specialist|специалист[а-я]*)", re.IGNORECASE),
    # "ninja / middle / senior" counts handled by explicit number near role
    re.compile(r"(\d+)\s*(?:бэкенд|фронтенд|аналитик|тестировщик|девops|дата[- ]?инженер|архитектор|дизайнер|менеджер|разработчик[а-я]*|qa|devops|инженер[а-я]*|security)", re.IGNORECASE),
]

# Default effort by role family + level (likely person-days) — used only when
# no timeline is found.
_DEFAULT_EFFORT: dict[str, dict[int, float]] = {
    "engineering": {2: 20, 3: 30, 4: 40, 5: 35},
    "quality": {2: 15, 3: 20, 4: 25, 5: 20},
    "infrastructure": {2: 15, 3: 25, 4: 35, 5: 30},
    "data": {2: 20, 3: 30, 4: 40, 5: 35},
    "analysis": {2: 15, 3: 20, 4: 25, 5: 20},
    "management": {2: 15, 3: 20, 4: 25, 5: 25},
    "design": {2: 15, 3: 20, 4: 20, 5: 15},
    "security": {2: 15, 3: 20, 4: 30, 5: 25},
}

# Three-point multipliers
_MIN_FACTOR = 0.7
_MAX_FACTOR = 1.4

# Calibrated weights: effort = timeline_days × weight × level_factor.
_ROLE_COMP_WEIGHTS: dict[tuple[str, str], float] = {
    # backend
    ("backend_engineer", "lang_go"): 1.7,
    ("backend_engineer", "lang_java"): 0.95,
    ("backend_engineer", "lang_python"): 0.95,
    ("backend_engineer", "fw_spring"): 0.95,
    ("backend_engineer", "fw_fastapi"): 0.8,
    ("backend_engineer", "fw_django"): 0.8,
    ("backend_engineer", "api_rest"): 0.95,
    ("backend_engineer", "api_grpc"): 0.5,
    ("backend_engineer", "db_postgresql"): 0.32,
    ("backend_engineer", "db_mysql"): 0.3,
    ("backend_engineer", "db_mongodb"): 0.3,
    ("backend_engineer", "mq_kafka"): 0.32,
    ("backend_engineer", "arch_microservices"): 0.87,
    ("backend_engineer", "arch_event_driven"): 0.5,
    ("backend_engineer", "arch_integration_patterns"): 0.29,
    ("backend_engineer", "cloud_kubernetes"): 0.5,
    ("backend_engineer", "cloud_docker"): 0.35,
    ("backend_engineer", "dom_fintech"): 0.24,
    ("backend_engineer", "dom_retail"): 0.24,
    ("backend_engineer", "dom_telecom"): 0.24,
    ("backend_engineer", "dom_logistics"): 0.24,
    # frontend
    ("frontend_engineer", "fw_react"): 1.27,
    ("frontend_engineer", "fw_vue"): 0.8,
    ("frontend_engineer", "fw_angular"): 0.8,
    ("frontend_engineer", "lang_typescript"): 1.5,
    ("frontend_engineer", "lang_javascript"): 0.8,
    ("frontend_engineer", "web_css"): 0.5,
    ("frontend_engineer", "web_accessibility"): 0.16,
    ("frontend_engineer", "api_rest"): 0.35,
    ("frontend_engineer", "test_playwright"): 0.5,
    ("frontend_engineer", "ui_design_system"): 0.5,
    # qa
    ("qa_engineer", "test_playwright"): 0.71,
    ("qa_engineer", "test_selenium"): 0.6,
    ("qa_engineer", "test_api"): 0.5,
    ("qa_engineer", "qa_test_design"): 0.95,
    ("qa_engineer", "qa_manual"): 0.71,
    ("qa_engineer", "test_performance"): 0.6,
    ("qa_engineer", "test_security"): 0.5,
    ("qa_engineer", "test_unit"): 0.4,
    ("qa_engineer", "lang_python"): 0.4,
    # devops
    ("devops_engineer", "cloud_kubernetes"): 0.87,
    ("devops_engineer", "cicd_gitlab"): 0.48,
    ("devops_engineer", "cicd_github_actions"): 0.48,
    ("devops_engineer", "iac_terraform"): 0.43,
    ("devops_engineer", "cloud_docker"): 0.48,
    ("devops_engineer", "cloud_aws"): 0.5,
    ("devops_engineer", "cloud_yandex"): 0.28,
    ("devops_engineer", "cloud_azure"): 0.5,
    ("devops_engineer", "cloud_gcp"): 0.5,
    ("devops_engineer", "obs_prometheus"): 0.24,
    ("devops_engineer", "obs_grafana"): 0.24,
    ("devops_engineer", "obs_opentelemetry"): 0.3,
    ("devops_engineer", "lang_python"): 0.25,
    ("devops_engineer", "lang_bash"): 0.25,
    ("devops_engineer", "lang_go"): 0.3,
    # data
    ("data_engineer", "lang_python"): 0.95,
    ("data_engineer", "lang_sql"): 0.87,
    ("data_engineer", "db_postgresql"): 0.32,
    ("data_engineer", "db_clickhouse"): 0.24,
    ("data_engineer", "db_mysql"): 0.2,
    ("data_engineer", "etl_orchestration"): 0.65,
    ("data_engineer", "dwh_modeling"): 0.43,
    ("data_engineer", "mq_kafka"): 0.29,
    ("data_engineer", "ml_recsys"): 0.95,
    ("data_engineer", "stat_probability"): 0.5,
    # ml
    ("ml_engineer", "lang_python"): 0.95,
    ("ml_engineer", "ml_pytorch"): 0.71,
    ("ml_engineer", "ml_sklearn"): 0.75,
    ("ml_engineer", "ml_nlp"): 0.52,
    ("ml_engineer", "ml_cv"): 0.75,
    ("ml_engineer", "ml_recsys"): 0.75,
    ("ml_engineer", "ml_mlops"): 0.36,
    ("ml_engineer", "stat_probability"): 0.5,
    # security
    ("security_engineer", "sec_appsec"): 0.43,
    ("security_engineer", "sec_owasp"): 0.35,
    ("security_engineer", "sec_threat_modeling"): 0.5,
    ("security_engineer", "sec_iam"): 0.4,
    ("security_engineer", "sec_pki"): 0.35,
    ("security_engineer", "sec_siem"): 0.35,
    # architect
    ("solution_architect", "arch_system_design"): 0.29,
    ("solution_architect", "arch_microservices"): 0.79,
    ("solution_architect", "arch_event_driven"): 0.79,
    ("solution_architect", "arch_c4"): 0.5,
    ("solution_architect", "arch_ddd"): 0.5,
    ("solution_architect", "arch_integration_patterns"): 0.7,
    ("solution_architect", "sec_threat_modeling"): 0.5,
    ("solution_architect", "dom_telecom"): 0.79,
    ("solution_architect", "dom_fintech"): 0.8,
    ("solution_architect", "mq_kafka"): 0.3,
    ("solution_architect", "db_postgresql"): 0.22,
    # analyst
    ("business_analyst", "an_requirements"): 0.48,
    ("business_analyst", "an_bpmn"): 0.5,
    ("business_analyst", "an_user_stories"): 0.5,
    ("business_analyst", "an_sql_analysis"): 0.71,
    ("business_analyst", "proc_stakeholder_mgmt"): 0.4,
    ("system_analyst", "an_requirements"): 0.5,
    ("system_analyst", "an_integration"): 0.67,
    ("system_analyst", "api_rest"): 0.5,
    ("system_analyst", "arch_c4"): 0.5,
    ("system_analyst", "lang_sql"): 0.5,
    # pm / design
    ("project_manager", "proc_agile_scrum"): 0.95,
    ("project_manager", "proc_estimation"): 0.6,
    ("project_manager", "proc_risk_management"): 0.5,
    ("project_manager", "proc_stakeholder_mgmt"): 0.5,
    ("project_manager", "proc_kanban"): 0.8,
    ("ux_ui_designer", "ui_figma"): 0.95,
    ("ux_ui_designer", "ui_wireframing"): 0.48,
    ("ux_ui_designer", "ux_research"): 0.5,
    ("ux_ui_designer", "ui_design_system"): 0.6,
    ("ux_ui_designer", "ux_accessibility"): 0.3,
}

# Fallback defaults by competency id prefix / category
_CATEGORY_DEFAULT_WEIGHTS: dict[str, float] = {
    "lang_": 0.8,
    "fw_": 0.8,
    "db_": 0.3,
    "mq_": 0.3,
    "cloud_": 0.5,
    "iac_": 0.4,
    "cicd_": 0.5,
    "obs_": 0.25,
    "qa_": 0.5,
    "test_": 0.5,
    "api_": 0.5,
    "arch_": 0.5,
    "an_": 0.5,
    "ml_": 0.7,
    "sec_": 0.35,
    "etl_": 0.6,
    "dwh_": 0.5,
    "proc_": 0.6,
    "ux_": 0.5,
    "ui_": 0.6,
    "dom_": 0.25,
    "soft_": 0.2,
}

# Fallback defaults by role family
_FAMILY_DEFAULT_WEIGHTS: dict[str, float] = {
    "engineering": 0.5,
    "quality": 0.5,
    "infrastructure": 0.5,
    "data": 0.6,
    "analysis": 0.5,
    "management": 0.6,
    "design": 0.6,
    "security": 0.35,
}


@dataclass
class TimelineParse:
    """Result of parsing a timeline expression."""

    raw_value: float  # e.g. 8
    unit: str  # "month" | "week" | "day"
    person_days: float  # total working person-days


def parse_timeline(text: str) -> TimelineParse | None:
    """Extract the first timeline expression from text. Returns None if not found."""
    for pattern, unit in _TIMELINE_PATTERNS:
        m = pattern.search(text)
        if m:
            raw = float(m.group(1).replace(",", "."))
            if raw <= 0:
                continue
            person_days = raw * _DAYS_PER_UNIT[unit]
            return TimelineParse(raw_value=raw, unit=unit, person_days=person_days)
    return None


def parse_team_size(text: str) -> int | None:
    """Extract the largest team size number from the brief text."""
    best: int | None = None
    for pat in _TEAM_SIZE_PATTERNS:
        for m in pat.finditer(text):
            vals = [int(g) for g in m.groups() if g is not None]
            for v in vals:
                if 1 <= v <= 20:
                    if best is None or v > best:
                        best = v
    return best


def _get_role_family(role_id: str) -> str:
    """Map role_id to a rough family for default effort/weight lookup."""
    mapping = {
        "backend_engineer": "engineering",
        "frontend_engineer": "engineering",
        "qa_engineer": "quality",
        "devops_engineer": "infrastructure",
        "data_engineer": "data",
        "ml_engineer": "data",
        "business_analyst": "analysis",
        "system_analyst": "analysis",
        "project_manager": "management",
        "ux_ui_designer": "design",
        "solution_architect": "engineering",
        "security_engineer": "security",
    }
    return mapping.get(role_id, "engineering")


def _get_weight(role_id: str, competency_id: str | None) -> float:
    """Return calibrated weight for (role, competency).

    Falls back to category default, then role-family default.
    """
    if competency_id:
        exact = _ROLE_COMP_WEIGHTS.get((role_id, competency_id))
        if exact is not None:
            return exact

        # Category default by prefix
        for prefix, weight in _CATEGORY_DEFAULT_WEIGHTS.items():
            if competency_id.startswith(prefix):
                return weight

    # Role-family default
    family = _get_role_family(role_id)
    return _FAMILY_DEFAULT_WEIGHTS.get(family, 0.5)


def _level_factor(level: int, factor: float) -> float:
    """Level adjustment: each step away from middle changes effort by factor."""
    return 1.0 + (level - 3) * factor


class EffortEstimator:
    """Estimate effort as a three-point range (min, likely, max) in person-days."""

    def __init__(
        self,
        min_factor: float = _MIN_FACTOR,
        max_factor: float = _MAX_FACTOR,
        default_likely: float = 20.0,
        level_factor: float = 0.1,
        role_comp_weights: dict[tuple[str, str], float] | None = None,
    ):
        self.min_factor = min_factor
        self.max_factor = max_factor
        self.default_likely = default_likely
        self.level_factor = level_factor
        self.role_comp_weights = role_comp_weights or _ROLE_COMP_WEIGHTS

    def estimate_for_fields(
        self,
        role_id: str,
        comp_id: str | None,
        level: int,
        headcount: int,
        brief_text: str,
        effective_team_size: int | None = None,
    ) -> tuple[EffortRange, str]:
        """Compute `(EffortRange, assumption)` directly from scalar fields."""
        timeline = parse_timeline(brief_text)
        team_size = parse_team_size(brief_text)
        eff_team = max(team_size or 1, effective_team_size or 1)
        timeline_days = timeline.person_days if timeline else None
        return self._compute_range(
            role_id=role_id,
            comp_id=comp_id,
            level=level,
            headcount=headcount,
            timeline_days=timeline_days,
            effective_team_size=eff_team,
        )

    def estimate_all(
        self,
        partials: list[PartialRequirement],
        brief_text: str,
    ) -> list[EffortRange]:
        """Estimate effort for all partial requirements together."""
        timeline = parse_timeline(brief_text)
        team_size = parse_team_size(brief_text)
        effective_team_size = max(team_size or 1, len(partials) or 1)
        timeline_days = timeline.person_days if timeline else None

        results: list[EffortRange] = []
        for p in partials:
            results.append(
                self._estimate_one(p, timeline_days, effective_team_size)
            )
        return results

    def estimate(self, partial: PartialRequirement, brief_text: str) -> EffortRange:
        """Backward-compatible single requirement estimation."""
        timeline = parse_timeline(brief_text)
        team_size = parse_team_size(brief_text)
        effective_team_size = max(team_size or 1, 1)
        timeline_days = timeline.person_days if timeline else None
        return self._estimate_one(partial, timeline_days, effective_team_size)

    def _compute_range(
        self,
        *,
        role_id: str,
        comp_id: str | None,
        level: int,
        headcount: int,
        timeline_days: float | None,
        effective_team_size: int,
    ) -> tuple[EffortRange, str]:
        if timeline_days is not None:
            weight = _get_weight(role_id, comp_id)
            likely = timeline_days * weight * _level_factor(level, self.level_factor)
            assumption = (
                f"Timeline parsed: {timeline_days:g} working days; "
                f"team size {effective_team_size}; headcount {headcount}; "
                f"weight {weight:.2f}; level factor {_level_factor(level, self.level_factor):.2f}"
            )
        else:
            family = _get_role_family(role_id)
            level_efforts = _DEFAULT_EFFORT.get(family, _DEFAULT_EFFORT["engineering"])
            likely = level_efforts.get(level, self.default_likely)
            assumption = (
                f"No timeline found; default estimate {likely:g} person-days "
                f"for {family} level {level}"
            )

        # Clamp to schema bounds [0.5, 2000]
        likely = max(0.5, min(2000.0, likely))
        min_val = max(0.5, round(self.min_factor * likely, 1))
        max_val = min(2000.0, round(self.max_factor * likely, 1))
        likely = round(likely, 1)

        if min_val > likely:
            min_val = likely
        if max_val < likely:
            max_val = likely

        return (
            EffortRange(
                min_person_days=min_val,
                likely_person_days=likely,
                max_person_days=max_val,
                unit="person_days",
            ),
            assumption,
        )

    def _estimate_one(
        self,
        partial: PartialRequirement,
        timeline_days: float | None,
        effective_team_size: int,
    ) -> EffortRange:
        """Estimate effort for a single partial requirement."""
        role_id = partial.role.role_id
        comp_id = partial.competency.competency_id if partial.competency else None
        effort, assumption = self._compute_range(
            role_id=role_id,
            comp_id=comp_id,
            level=partial.level,
            headcount=partial.headcount,
            timeline_days=timeline_days,
            effective_team_size=effective_team_size,
        )
        if assumption not in partial.assumptions:
            partial.assumptions.append(assumption)
        return effort
