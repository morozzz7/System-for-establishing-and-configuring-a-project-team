"""LLM configuration via environment variables (pydantic-settings).

Environment variables (prefix LLM_):
  LLM_BASE_URL          — OpenAI-compatible base URL (default: https://openrouter.ai/api/v1)
  LLM_API_KEY           — API key; empty/missing → LLM unavailable
  LLM_MODEL             — model name (default: qwen/qwen-2.5-72b-instruct)
  LLM_TIMEOUT_MS        — request timeout in milliseconds per HTTP call (default: 180000)
  LLM_MAX_RETRIES       — repair-retry attempts on JSON parse failure (default: 2)
  LLM_ENSEMBLE_RUNS     — number of parallel extraction calls in union ensemble (default: 3)
  LLM_VERIFY_CANDIDATES — enable word-boundary candidate scanner + LLM verification (default: True)
  LLM_ROLE_SAFETY_NET   — enable word-boundary role scanner + missing-role recovery (default: True)

Sources (in priority order):
  1. Environment variables (Docker, CI)
  2. ml/.env (service-specific local config)
  3. .env in repo root / current directory
"""

from __future__ import annotations

from functools import lru_cache
from pathlib import Path

from pydantic import Field
from pydantic_settings import BaseSettings, SettingsConfigDict


def _find_env_files() -> tuple[str, ...]:
    """Return candidate .env paths in increasing priority order (later overrides earlier)."""
    ml_dir = Path(__file__).resolve().parents[2]
    repo_root = ml_dir.parent
    return (
        str(repo_root / ".env"),
        str(Path.cwd() / ".env"),
        str(ml_dir / ".env"),
    )


class LLMSettings(BaseSettings):
    """LLM service configuration loaded from environment and .env files."""

    model_config = SettingsConfigDict(
        env_prefix="LLM_",
        env_file=_find_env_files(),
        env_file_encoding="utf-8",
        extra="ignore",
    )

    base_url: str = "https://openrouter.ai/api/v1"
    api_key: str = ""
    model: str = "qwen/qwen-2.5-72b-instruct"
    timeout_ms: int = 180000
    max_retries: int = Field(default=2, ge=0, le=10)
    ensemble_runs: int = Field(default=3, ge=1, le=10)
    verify_candidates: bool = True
    role_safety_net: bool = True

    @property
    def is_available(self) -> bool:
        """True when a non-placeholder API key is configured."""
        key = self.api_key.strip()
        return bool(key) and key not in (
            "your-key-here",
            "your-openrouter-api-key-here",
        )


@lru_cache(maxsize=1)
def get_settings() -> LLMSettings:
    """Return cached LLMSettings singleton (call clear_settings_cache() to reset)."""
    return LLMSettings()


def clear_settings_cache() -> None:
    """Clear the cached settings (for testing)."""
    get_settings.cache_clear()
