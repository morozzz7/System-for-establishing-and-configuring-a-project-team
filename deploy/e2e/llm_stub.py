"""Deterministic OpenAI-compatible LLM stand-in for the end-to-end stack.

Serves one recorded compact-schema response for every chat completion request so that
`/v1/extract` stays reproducible without an external LLM provider.
"""

from __future__ import annotations

import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

FIXTURE_PATH = Path(os.environ.get("LLM_STUB_FIXTURE", "/app/response.json"))
MODEL = os.environ.get("LLM_STUB_MODEL", "e2e-stub")


def load_content() -> tuple[str, int, int]:
    fixture: dict[str, Any] = json.loads(FIXTURE_PATH.read_text(encoding="utf-8"))
    return (
        fixture["content"],
        int(fixture.get("tokens_in", 0)),
        int(fixture.get("tokens_out", 0)),
    )


CONTENT, TOKENS_IN, TOKENS_OUT = load_content()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def respond(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802 - stdlib handler API
        if self.path == "/healthz":
            self.respond(200, {"status": "ok"})
        else:
            self.respond(404, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802 - stdlib handler API
        length = int(self.headers.get("Content-Length", "0"))
        if length:
            self.rfile.read(length)
        if not self.path.rstrip("/").endswith("chat/completions"):
            self.respond(404, {"error": "not found"})
            return
        self.respond(
            200,
            {
                "id": "chatcmpl-e2e-stub",
                "object": "chat.completion",
                "created": 0,
                "model": MODEL,
                "choices": [
                    {
                        "index": 0,
                        "message": {"role": "assistant", "content": CONTENT},
                        "finish_reason": "stop",
                    }
                ],
                "usage": {
                    "prompt_tokens": TOKENS_IN,
                    "completion_tokens": TOKENS_OUT,
                    "total_tokens": TOKENS_IN + TOKENS_OUT,
                },
            },
        )

    def log_message(self, format: str, *args: object) -> None:
        return


if __name__ == "__main__":
    ThreadingHTTPServer(("0.0.0.0", 8000), Handler).serve_forever()
