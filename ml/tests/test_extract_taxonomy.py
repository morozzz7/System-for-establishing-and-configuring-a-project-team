"""Tests for ml.app.extract.taxonomy — YAML loader and synonym index."""

from __future__ import annotations

import pytest

from app.extract.taxonomy import TaxonomyRegistry, get_registry, reset_registry


@pytest.fixture(autouse=True)
def _reset():
    """Reset registry cache before each test."""
    reset_registry()
    yield
    reset_registry()


class TestTaxonomyLoad:
    def test_load_all_12_roles(self):
        reg = get_registry()
        assert len(reg.roles) == 12
        expected_ids = {
            "backend_engineer", "frontend_engineer", "qa_engineer", "devops_engineer",
            "data_engineer", "ml_engineer", "business_analyst", "system_analyst",
            "project_manager", "ux_ui_designer", "solution_architect", "security_engineer",
        }
        assert set(reg.roles.keys()) == expected_ids

    def test_competencies_loaded(self):
        reg = get_registry()
        assert len(reg.competencies) >= 100  # taxonomy says 101

    def test_core_competencies_populated(self):
        reg = get_registry()
        assert "lang_go" in reg.core_competencies_by_role["backend_engineer"]
        assert "fw_react" in reg.core_competencies_by_role["frontend_engineer"]

    def test_role_name_ru(self):
        reg = get_registry()
        assert reg.roles["backend_engineer"].name_ru == "Бэкенд-разработчик"
        assert reg.roles["frontend_engineer"].name_ru == "Фронтенд-разработчик"


class TestSynonymIndex:
    def test_backend_synonym(self):
        reg = get_registry()
        result = reg.lookup_synonym("бэкенд")
        assert result is not None
        assert result == ("role", "backend_engineer")

    def test_go_synonym(self):
        reg = get_registry()
        result = reg.lookup_synonym("го")
        assert result is not None
        assert result == ("competency", "lang_go")

    def test_react_synonym(self):
        reg = get_registry()
        result = reg.lookup_synonym("реакт")
        assert result is not None
        assert result == ("competency", "fw_react")

    def test_postgresql_synonym(self):
        reg = get_registry()
        result = reg.lookup_synonym("постгрес")
        assert result is not None
        assert result == ("competency", "db_postgresql")

    def test_mixed_case(self):
        reg = get_registry()
        assert reg.lookup_synonym("Backend") == ("role", "backend_engineer")
        assert reg.lookup_synonym("GO") == ("competency", "lang_go")
        assert reg.lookup_synonym("React") == ("competency", "fw_react")

    def test_unknown_synonym_returns_none(self):
        reg = get_registry()
        assert reg.lookup_synonym("unknown_nonexistent_xyz") is None

    def test_golang_synonym(self):
        reg = get_registry()
        result = reg.lookup_synonym("golang")
        assert result == ("competency", "lang_go")

    def test_k8s_synonym(self):
        reg = get_registry()
        result = reg.lookup_synonym("k8s")
        assert result == ("competency", "cloud_kubernetes")


class TestCompetencyRoles:
    def test_cloud_yandex_valid_for_devops(self):
        reg = get_registry()
        comp = reg.get_competency("cloud_yandex")
        assert comp is not None
        assert "devops_engineer" in comp.roles
        assert "solution_architect" in comp.roles

    def test_cloud_yandex_not_in_devops_core(self):
        """Many-to-many pairs are valid even if not in role.core_competencies."""
        reg = get_registry()
        assert "cloud_yandex" not in reg.core_competencies_by_role["devops_engineer"]

    def test_db_postgresql_roles(self):
        reg = get_registry()
        comp = reg.get_competency("db_postgresql")
        assert set(comp.roles) == {
            "backend_engineer", "data_engineer", "system_analyst", "devops_engineer"
        }
