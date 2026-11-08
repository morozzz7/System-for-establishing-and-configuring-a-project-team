"""Taxonomy registry: loads contracts/taxonomy.yaml once, caches globally.

Provides O(1) synonym lookup for roles and competencies, core-competency mapping,
and full role/competency metadata. Thread-safe on first load via module-level singleton.
"""

from __future__ import annotations

import os
import threading
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from ruamel.yaml import YAML

# Repo root -> taxonomy path (relative to this file: ml/app/extract/taxonomy.py)
# In tests: parents[3] = repo root
# In Docker: TAXONOMY_PATH env var overrides this
_TAXONOMY_PATH_DEFAULT = Path(__file__).resolve().parents[3] / "contracts" / "taxonomy.yaml"
_TAXONOMY_PATH = Path(os.environ.get("TAXONOMY_PATH", str(_TAXONOMY_PATH_DEFAULT)))


@dataclass(frozen=True)
class Role:
    """A role from taxonomy.yaml."""

    role_id: str
    name_en: str
    name_ru: str
    family: str
    synonyms: list[str]
    core_competencies: list[str]
    typical_grades: list[str]


@dataclass(frozen=True)
class Competency:
    """A competency from taxonomy.yaml."""

    competency_id: str
    name_en: str
    name_ru: str
    category: str
    roles: list[str]
    synonyms: list[str]


@dataclass
class TaxonomyRegistry:
    """In-memory taxonomy with synonym index for O(1) lookup."""

    roles: dict[str, Role] = field(default_factory=dict)
    competencies: dict[str, Competency] = field(default_factory=dict)
    synonym_index: dict[str, tuple[str, str]] = field(default_factory=dict)
    # synonym (lowercase) -> (kind: "role"|"competency", id)
    core_competencies_by_role: dict[str, list[str]] = field(default_factory=dict)

    # ------------------------------------------------------------------
    # Lookup helpers
    # ------------------------------------------------------------------

    def lookup_synonym(self, text: str) -> tuple[str, str] | None:
        """Return (kind, id) for a synonym (case-insensitive), or None."""
        return self.synonym_index.get(text.strip().lower())

    def get_role(self, role_id: str) -> Role | None:
        return self.roles.get(role_id)

    def get_competency(self, competency_id: str) -> Competency | None:
        return self.competencies.get(competency_id)


# ---------------------------------------------------------------------------
# Loader
# ---------------------------------------------------------------------------


def _load_taxonomy(path: Path = _TAXONOMY_PATH) -> TaxonomyRegistry:
    """Parse taxonomy.yaml and build the registry with synonym index."""
    yaml = YAML(typ="safe")
    with open(path, "r", encoding="utf-8") as f:
        data: dict[str, Any] = yaml.load(f)

    registry = TaxonomyRegistry()

    # Roles
    for r in data.get("roles", []):
        role = Role(
            role_id=r["role_id"],
            name_en=r["name_en"],
            name_ru=r["name_ru"],
            family=r["family"],
            synonyms=list(r.get("synonyms", [])),
            core_competencies=list(r.get("core_competencies", [])),
            typical_grades=list(r.get("typical_grades", [])),
        )
        registry.roles[role.role_id] = role
        registry.core_competencies_by_role[role.role_id] = list(role.core_competencies)

        # Index role_id itself, names, and all synonyms
        registry.synonym_index[role.role_id.lower()] = ("role", role.role_id)
        for name_field in (role.name_en, role.name_ru):
            key = name_field.strip().lower()
            if key and key not in registry.synonym_index:
                registry.synonym_index[key] = ("role", role.role_id)
        for syn in role.synonyms:
            key = syn.strip().lower()
            if key and key not in registry.synonym_index:
                registry.synonym_index[key] = ("role", role.role_id)

    # Competencies
    for c in data.get("competencies", []):
        comp = Competency(
            competency_id=c["competency_id"],
            name_en=c["name_en"],
            name_ru=c["name_ru"],
            category=c["category"],
            roles=list(c.get("roles", [])),
            synonyms=list(c.get("synonyms", [])),
        )
        registry.competencies[comp.competency_id] = comp

        # Index competency_id itself, names, and all synonyms
        registry.synonym_index[comp.competency_id.lower()] = ("competency", comp.competency_id)
        # Also index the display names (e.g. "Go" -> lang_go, "PostgreSQL" -> db_postgresql)
        for name_field in (comp.name_en, comp.name_ru):
            key = name_field.strip().lower()
            if key and key not in registry.synonym_index:
                registry.synonym_index[key] = ("competency", comp.competency_id)
        for syn in comp.synonyms:
            key = syn.strip().lower()
            if key and key not in registry.synonym_index:
                registry.synonym_index[key] = ("competency", comp.competency_id)

    return registry


# ---------------------------------------------------------------------------
# Global singleton (thread-safe lazy init)
# ---------------------------------------------------------------------------

_lock = threading.Lock()
_instance: TaxonomyRegistry | None = None


def get_registry(path: Path | None = None) -> TaxonomyRegistry:
    """Return the global TaxonomyRegistry, loading it on first call."""
    global _instance
    if _instance is not None:
        return _instance
    with _lock:
        if _instance is not None:
            return _instance
        _instance = _load_taxonomy(path or _TAXONOMY_PATH)
        return _instance


def reset_registry() -> None:
    """Reset the cached registry (for testing)."""
    global _instance
    with _lock:
        _instance = None
