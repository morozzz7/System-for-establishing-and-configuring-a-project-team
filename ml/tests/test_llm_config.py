"""Tests for LLM configuration (pydantic-settings)."""

from __future__ import annotations

import os

import pytest

from app.llm.config import LLMSettings, clear_settings_cache, get_settings


@pytest.fixture(autouse=True)
def _clean_env():
    """Remove LLM_ env vars before each test, restore after.

    Also disable .env file loading so local .env files don't leak into tests.
    """
    saved = {}
    for key in list(os.environ):
        if key.startswith("LLM_"):
            saved[key] = os.environ.pop(key)
    # Disable .env file loading — tests must rely only on explicit env vars / defaults
    prev_env_file = LLMSettings.model_config.get("env_file")
    LLMSettings.model_config["env_file"] = None
    clear_settings_cache()
    yield
    # Restore
    for key in list(os.environ):
        if key.startswith("LLM_"):
            del os.environ[key]
    for key, val in saved.items():
        os.environ[key] = val
    LLMSettings.model_config["env_file"] = prev_env_file
    clear_settings_cache()


class TestLLMSettingsDefaults:
    def test_default_base_url(self):
        s = LLMSettings()
        assert s.base_url == "https://openrouter.ai/api/v1"

    def test_default_model(self):
        s = LLMSettings()
        assert s.model == "qwen/qwen-2.5-72b-instruct"

    def test_default_timeout(self):
        s = LLMSettings()
        assert s.timeout_ms == 180000

    def test_default_ensemble_and_safety_flags(self):
        s = LLMSettings()
        assert s.ensemble_runs == 3
        assert s.verify_candidates is True
        assert s.role_safety_net is True

    def test_default_max_retries(self):
        s = LLMSettings()
        assert s.max_retries == 2

    def test_empty_api_key(self):
        s = LLMSettings()
        assert s.api_key == ""


class TestLLMSettingsEnvOverride:
    def test_override_via_env(self):
        os.environ["LLM_API_KEY"] = "test-key-123"
        os.environ["LLM_MODEL"] = "custom/model"
        os.environ["LLM_TIMEOUT_MS"] = "5000"
        clear_settings_cache()
        s = LLMSettings()
        assert s.api_key == "test-key-123"
        assert s.model == "custom/model"
        assert s.timeout_ms == 5000

    def test_get_settings_cached(self):
        os.environ["LLM_API_KEY"] = "cached-key"
        clear_settings_cache()
        s1 = get_settings()
        s2 = get_settings()
        assert s1 is s2
        assert s1.api_key == "cached-key"


class TestLLMAvailability:
    def test_unavailable_when_no_key(self):
        s = LLMSettings()
        assert s.is_available is False

    def test_available_when_key_set(self):
        s = LLMSettings(api_key="sk-test-12345")
        assert s.is_available is True

    def test_unavailable_when_empty_string(self):
        s = LLMSettings(api_key="")
        assert s.is_available is False
