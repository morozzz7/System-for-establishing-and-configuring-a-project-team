"""Tests for prompt template (extract-v3): format, security (injection guard), vocabulary with synonyms & roles."""

from __future__ import annotations

import pytest

from app.extract.prompt import PROMPT_VERSION, build_extract_prompt
from app.extract.taxonomy import get_registry, reset_registry


@pytest.fixture(autouse=True)
def _reset():
    reset_registry()
    yield
    reset_registry()


class TestPromptVersion:
    def test_prompt_version_is_extract_v3(self):
        assert PROMPT_VERSION == "extract-v3"


class TestPromptStructure:
    def test_returns_two_messages(self):
        registry = get_registry()
        messages = build_extract_prompt("Test brief text here", registry, "ru")
        assert len(messages) == 2
        assert messages[0]["role"] == "system"
        assert messages[1]["role"] == "user"

    def test_brief_text_only_in_user_message(self):
        """Security: brief_text MUST NOT appear in system message."""
        registry = get_registry()
        brief = "Уникальный текст брифа для теста инъекций XYZ123"
        messages = build_extract_prompt(brief, registry, "ru")
        system_msg = messages[0]["content"]
        user_msg = messages[1]["content"]

        assert brief not in system_msg
        assert "XYZ123" not in system_msg
        assert brief in user_msg

    def test_system_contains_role_ids_and_synonyms(self):
        registry = get_registry()
        messages = build_extract_prompt("test", registry, "ru")
        system_msg = messages[0]["content"]
        assert "backend_engineer" in system_msg
        assert "frontend_engineer" in system_msg
        assert "qa_engineer" in system_msg
        # Synonyms included in extract-v3
        assert "бэкенд" in system_msg

    def test_system_contains_competency_ids_synonyms_and_allowed_roles(self):
        registry = get_registry()
        messages = build_extract_prompt("test", registry, "ru")
        system_msg = messages[0]["content"]
        assert "lang_go" in system_msg
        assert "db_postgresql" in system_msg
        assert "cloud_yandex" in system_msg
        assert "golang" in system_msg
        assert "roles:" in system_msg

    def test_no_gold_brief_001_leak_in_examples(self):
        """Few-shot examples in extract-v3 must not contain brief_001 text."""
        registry = get_registry()
        messages = build_extract_prompt("test", registry, "ru")
        system_msg = messages[0]["content"]
        assert "MVP веб-сервиса для управления задачами" not in system_msg
        examples_section = system_msg.split("## Few-shot examples")[1]
        assert "Playwright" not in examples_section
        assert "Бэкенд на Go (middle)" not in examples_section

    def test_output_schema_reference_in_system(self):
        registry = get_registry()
        messages = build_extract_prompt("test", registry, "ru")
        system_msg = messages[0]["content"]
        assert "ExtractedRequirement[]" in system_msg
        assert "role_id" in system_msg
        assert "competency_id" in system_msg
        assert "quote" in system_msg
