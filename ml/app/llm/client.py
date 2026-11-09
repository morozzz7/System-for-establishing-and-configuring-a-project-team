"""OpenAI-compatible async HTTP client for LLM inference.

Does NOT retry internally — caller handles retry logic.
Does NOT fall back silently — raises typed exceptions.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

import httpx

from .config import LLMSettings


# ---------------------------------------------------------------------------
# Response / error types
# ---------------------------------------------------------------------------


@dataclass(frozen=True)
class LLMResponse:
    """Successful LLM response."""

    content: str
    tokens_in: int
    tokens_out: int
    model: str
    finish_reason: str | None = None


class LLMError(Exception):
    """Base class for LLM errors."""


class LLMUnavailableError(LLMError):
    """No API key, or connection refused (service unreachable)."""


class LLMTimeoutError(LLMError):
    """Request timed out."""


class LLMErrorResponse(LLMError):
    """HTTP 4xx/5xx or malformed response body."""

    def __init__(self, message: str, status_code: int | None = None) -> None:
        super().__init__(message)
        self.status_code = status_code


# ---------------------------------------------------------------------------
# Client
# ---------------------------------------------------------------------------


class LLMClient:
    """Async OpenAI-compatible chat completion client.

    Usage:
        client = LLMClient(settings)
        resp = await client.chat(messages=[...], temperature=0, seed=42)
    """

    def __init__(self, settings: LLMSettings) -> None:
        if not settings.is_available:
            raise LLMUnavailableError(
                "LLM_API_KEY is not configured; LLM extraction is unavailable"
            )
        self._settings = settings
        self._url = f"{settings.base_url.rstrip('/')}/chat/completions"

    async def chat(
        self,
        messages: list[dict[str, Any]],
        *,
        temperature: float = 0,
        seed: int | None = None,
        max_tokens: int | None = None,
        timeout_ms: int | None = None,
        response_format: dict[str, Any] | None = None,
    ) -> LLMResponse:
        """Send a chat completion request and return the parsed response.

        Raises:
            LLMUnavailableError: connection refused
            LLMTimeoutError: request timed out
            LLMErrorResponse: HTTP error or malformed body
        """
        is_openrouter = "openrouter.ai" in self._url.lower()
        is_deepseek = "deepseek.com" in self._url.lower()

        headers: dict[str, str] = {
            "Authorization": f"Bearer {self._settings.api_key}",
            "Content-Type": "application/json",
            "HTTP-Referer": "teamopt-ml-service",
            "X-Title": "TeamOpt ML",
        }

        body: dict[str, Any] = {
            "model": self._settings.model,
            "messages": messages,
            "temperature": temperature,
        }
        if seed is not None:
            body["seed"] = seed
        if max_tokens is not None:
            body["max_tokens"] = min(max_tokens, 8192) if is_deepseek else max_tokens
        if is_openrouter and ":free" in self._settings.model:
            body["reasoning"] = {"exclude": True, "effort": "low"}
        if response_format is not None:
            body["response_format"] = response_format

        effective_timeout_ms = (
            timeout_ms if timeout_ms is not None else self._settings.timeout_ms
        )
        if effective_timeout_ms <= 0:
            raise LLMTimeoutError("LLM request budget exhausted (timeout_ms <= 0)")
        timeout_s = effective_timeout_ms / 1000.0

        try:
            async with httpx.AsyncClient(timeout=timeout_s) as http:
                resp = await http.post(self._url, json=body, headers=headers)
                if (
                    is_deepseek
                    and resp.status_code in (400, 422)
                    and "seed" in body
                    and "seed" in resp.text.lower()
                ):
                    body_no_seed = {k: v for k, v in body.items() if k != "seed"}
                    resp = await http.post(
                        self._url, json=body_no_seed, headers=headers
                    )
        except httpx.ConnectError as exc:
            raise LLMUnavailableError(
                f"Cannot connect to LLM endpoint: {exc}"
            ) from exc
        except httpx.TimeoutException as exc:
            raise LLMTimeoutError(
                f"LLM request timed out after {timeout_s}s"
            ) from exc

        # Check HTTP status
        if resp.status_code >= 400:
            raise LLMErrorResponse(
                f"LLM returned HTTP {resp.status_code}: {resp.text[:500]}",
                status_code=resp.status_code,
            )

        # Parse response body
        try:
            data = resp.json()
        except Exception as exc:
            raise LLMErrorResponse(
                f"LLM returned non-JSON body: {resp.text[:200]}",
                status_code=resp.status_code,
            ) from exc

        try:
            choice = data["choices"][0]
            msg = choice["message"]
            content: str = msg.get("content") or msg.get("reasoning_content") or ""
            finish_reason: str | None = choice.get("finish_reason")
            usage = data.get("usage", {})
            tokens_in = usage.get("prompt_tokens", 0)
            tokens_out = usage.get("completion_tokens", 0)
            model = data.get("model", self._settings.model)
        except (KeyError, IndexError, TypeError) as exc:
            if isinstance(data, dict) and data.get("error"):
                raise LLMErrorResponse(
                    f"LLM provider error in 200 response: {data['error']}",
                    status_code=502,
                ) from exc
            raise LLMErrorResponse(
                f"Unexpected LLM response structure: {exc} | body={resp.text[:400]}"
            ) from exc

        return LLMResponse(
            content=content,
            tokens_in=tokens_in,
            tokens_out=tokens_out,
            model=model,
            finish_reason=finish_reason,
        )
