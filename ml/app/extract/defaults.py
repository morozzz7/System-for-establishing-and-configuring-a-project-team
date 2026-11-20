"""Default role selection table for ambiguous (multi-role) competencies.

This is a temporary code-side fallback until `contracts/taxonomy.yaml` gains an
official `primary_role` field (approved by orchestrator, pending diff review).

Rules:
- For competencies with a single role, that role is the default.
- For competencies with multiple roles, this table provides the preferred role
  when the text does not explicitly name one.
- The table was derived from gold annotation frequency and domain heuristics.
"""

from __future__ import annotations

from .taxonomy import TaxonomyRegistry

# Default role for competencies that appear in several roles.
PRIMARY_ROLE_OVERRIDES: dict[str, str] = {
    # analysis
    "an_bpmn": "business_analyst",
    "an_integration": "system_analyst",
    "an_requirements": "business_analyst",
    "an_sql_analysis": "business_analyst",
    "an_user_stories": "business_analyst",
    # api
    "api_graphql": "backend_engineer",
    "api_grpc": "backend_engineer",
    "api_rest": "backend_engineer",
    # architecture
    "arch_c4": "system_analyst",
    "arch_ddd": "solution_architect",
    "arch_event_driven": "solution_architect",
    "arch_integration_patterns": "backend_engineer",
    "arch_microservices": "backend_engineer",
    "arch_system_design": "solution_architect",
    # cicd
    "cicd_github_actions": "devops_engineer",
    "cicd_gitlab": "devops_engineer",
    # cloud
    "cloud_aws": "devops_engineer",
    "cloud_azure": "devops_engineer",
    "cloud_docker": "devops_engineer",
    "cloud_gcp": "devops_engineer",
    "cloud_kubernetes": "devops_engineer",
    "cloud_yandex": "devops_engineer",
    # databases
    "db_clickhouse": "data_engineer",
    "db_elasticsearch": "backend_engineer",
    "db_mongodb": "backend_engineer",
    "db_mssql": "backend_engineer",
    "db_mysql": "backend_engineer",
    "db_pgvector": "backend_engineer",
    "db_postgresql": "backend_engineer",
    "db_redis": "backend_engineer",
    # domain
    "dom_fintech": "backend_engineer",
    "dom_logistics": "business_analyst",
    "dom_retail": "backend_engineer",
    "dom_telecom": "solution_architect",
    # dwh
    "dwh_modeling": "data_engineer",
    # frameworks
    "fw_fastapi": "backend_engineer",
    "fw_nodejs": "backend_engineer",
    # iac
    "iac_terraform": "devops_engineer",
    # languages
    "lang_bash": "devops_engineer",
    "lang_go": "backend_engineer",
    "lang_java": "backend_engineer",
    "lang_python": "data_engineer",
    "lang_sql": "data_engineer",
    "lang_typescript": "frontend_engineer",
    # ml
    "ml_llm": "ml_engineer",
    "ml_mlops": "ml_engineer",
    # messaging
    "mq_kafka": "backend_engineer",
    "mq_nats": "backend_engineer",
    "mq_rabbitmq": "backend_engineer",
    # observability
    "obs_opentelemetry": "devops_engineer",
    # process
    "proc_agile_scrum": "project_manager",
    "proc_code_review": "backend_engineer",
    "proc_estimation": "project_manager",
    "proc_git_flow": "backend_engineer",
    "proc_kanban": "project_manager",
    "proc_risk_management": "project_manager",
    "proc_stakeholder_mgmt": "project_manager",
    # qa / testing
    "qa_manual": "qa_engineer",
    "qa_test_design": "qa_engineer",
    "test_api": "qa_engineer",
    "test_performance": "qa_engineer",
    "test_playwright": "qa_engineer",
    "test_pytest": "qa_engineer",
    "test_security": "qa_engineer",
    "test_unit": "backend_engineer",
    # security
    "sec_appsec": "security_engineer",
    "sec_iam": "security_engineer",
    "sec_owasp": "security_engineer",
    "sec_pki": "security_engineer",
    "sec_threat_modeling": "security_engineer",
    # soft skills
    "soft_communication": "project_manager",
    "soft_mentoring": "solution_architect",
    "soft_negotiation": "project_manager",
    # stats
    "stat_probability": "ml_engineer",
    # ui/ux
    "ui_design_system": "ux_ui_designer",
    "ui_figma": "ux_ui_designer",
    "ui_wireframing": "ux_ui_designer",
    "ux_accessibility": "ux_ui_designer",
    "ux_research": "ux_ui_designer",
    "web_accessibility": "frontend_engineer",
    "web_css": "frontend_engineer",
}


def get_primary_role(competency_id: str, registry: TaxonomyRegistry) -> str | None:
    """Return the default role for a competency.

    Falls back to the only listed role for single-role competencies, then to the
    first listed role, then None.
    """
    if competency_id in PRIMARY_ROLE_OVERRIDES:
        return PRIMARY_ROLE_OVERRIDES[competency_id]
    comp = registry.get_competency(competency_id)
    if not comp:
        return None
    roles = comp.roles
    if len(roles) == 1:
        return roles[0]
    return roles[0] if roles else None
