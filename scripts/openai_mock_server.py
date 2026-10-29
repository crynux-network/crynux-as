#!/usr/bin/env python3
"""Local OpenAI-compatible mock for Crynux AS Provider Key Sharing e2e."""

from __future__ import annotations

import argparse
import json
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.parse import urlparse

MODEL = "gpt-5.6-sol"
FIXED_CONTENT = "mock-ok"
PROMPT_TOKENS = 10
COMPLETION_TOKENS = 5
TOTAL_TOKENS = PROMPT_TOKENS + COMPLETION_TOKENS


def chat_usage() -> dict[str, Any]:
    return {
        "prompt_tokens": PROMPT_TOKENS,
        "completion_tokens": COMPLETION_TOKENS,
        "total_tokens": TOTAL_TOKENS,
        "prompt_tokens_details": {"cached_tokens": 0},
        "completion_tokens_details": {"reasoning_tokens": 0},
    }


def responses_usage() -> dict[str, Any]:
    return {
        "input_tokens": PROMPT_TOKENS,
        "output_tokens": COMPLETION_TOKENS,
        "total_tokens": TOTAL_TOKENS,
        "input_tokens_details": {"cached_tokens": 0},
        "output_tokens_details": {"reasoning_tokens": 0},
    }


def chat_completion(model: str, created: int) -> dict[str, Any]:
    return {
        "id": f"chatcmpl-mock-{created}",
        "object": "chat.completion",
        "created": created,
        "model": model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": FIXED_CONTENT},
                "finish_reason": "stop",
            }
        ],
        "usage": chat_usage(),
    }


def responses_body(model: str, created: int) -> dict[str, Any]:
    resp_id = f"resp_mock_{created}"
    return {
        "id": resp_id,
        "object": "response",
        "created_at": created,
        "status": "completed",
        "model": model,
        "output": [
            {
                "type": "message",
                "id": f"{resp_id}_msg",
                "role": "assistant",
                "status": "completed",
                "content": [{"type": "output_text", "text": FIXED_CONTENT}],
            }
        ],
        "usage": responses_usage(),
    }


def models_list() -> dict[str, Any]:
    return {
        "object": "list",
        "data": [
            {
                "id": MODEL,
                "object": "model",
                "created": 0,
                "owned_by": "openai",
            }
        ],
    }


class Handler(BaseHTTPRequestHandler):
    server_version = "CrynuxOpenAIMock/1.0"

    def log_message(self, fmt: str, *args: Any) -> None:
        print(f"{self.command} {self.path} {args[1] if len(args) > 1 else ''}".strip())

    def _read_json(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length", "0") or "0")
        if length <= 0:
            return {}
        raw = self.rfile.read(length)
        if not raw:
            return {}
        try:
            data = json.loads(raw.decode("utf-8"))
        except json.JSONDecodeError:
            return {}
        return data if isinstance(data, dict) else {}

    def _write_json(self, status: int, body: dict[str, Any]) -> None:
        payload = json.dumps(body).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.send_header("x-request-id", f"mock-{int(time.time())}")
        self.end_headers()
        self.wfile.write(payload)

    def _write_sse(self, frames: list[str]) -> None:
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.send_header("Connection", "close")
        self.send_header("x-request-id", f"mock-{int(time.time())}")
        self.end_headers()
        for frame in frames:
            self.wfile.write(frame.encode("utf-8"))
            self.wfile.flush()
        # Terminate the upstream stream so AS adapters observe EOF after final events.
        try:
            self.close_connection = True
        except Exception:
            pass

    def do_GET(self) -> None:
        path = urlparse(self.path).path.rstrip("/")
        if path == "/v1/models":
            self._write_json(200, models_list())
            return
        self._write_json(404, {"error": {"message": "not found", "type": "invalid_request_error"}})

    def do_POST(self) -> None:
        path = urlparse(self.path).path.rstrip("/")
        body = self._read_json()
        model = str(body.get("model") or MODEL)
        created = int(time.time())
        stream = bool(body.get("stream"))

        if path == "/v1/chat/completions":
            if stream:
                self._write_sse(self._chat_stream_frames(model, created))
            else:
                self._write_json(200, chat_completion(model, created))
            return

        if path == "/v1/responses":
            if stream:
                self._write_sse(self._responses_stream_frames(model, created))
            else:
                self._write_json(200, responses_body(model, created))
            return

        self._write_json(404, {"error": {"message": "not found", "type": "invalid_request_error"}})

    def _chat_stream_frames(self, model: str, created: int) -> list[str]:
        cid = f"chatcmpl-mock-{created}"

        def chunk(delta: dict[str, Any], finish: str | None = None, usage: dict[str, Any] | None = None) -> str:
            obj: dict[str, Any] = {
                "id": cid,
                "object": "chat.completion.chunk",
                "created": created,
                "model": model,
                "choices": [],
            }
            if usage is None:
                obj["choices"] = [
                    {
                        "index": 0,
                        "delta": delta,
                        "finish_reason": finish,
                    }
                ]
            else:
                obj["choices"] = []
                obj["usage"] = usage
            return f"data: {json.dumps(obj)}\n\n"

        return [
            chunk({"role": "assistant"}),
            chunk({"content": FIXED_CONTENT}),
            chunk({}, finish="stop"),
            chunk({}, usage=chat_usage()),
            "data: [DONE]\n\n",
        ]

    def _responses_stream_frames(self, model: str, created: int) -> list[str]:
        resp_id = f"resp_mock_{created}"
        completed = responses_body(model, created)

        def event(typ: str, payload: dict[str, Any]) -> str:
            body = dict(payload)
            body["type"] = typ
            return f"event: {typ}\ndata: {json.dumps(body)}\n\n"

        return [
            event(
                "response.created",
                {"response": {"id": resp_id, "object": "response", "model": model, "status": "in_progress"}},
            ),
            event("response.output_text.delta", {"delta": FIXED_CONTENT}),
            event("response.completed", {"response": completed}),
        ]


def main() -> None:
    parser = argparse.ArgumentParser(description="OpenAI mock server for Crynux AS e2e")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=18080)
    args = parser.parse_args()
    server = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"openai mock listening on http://{args.host}:{args.port}")
    server.serve_forever()


if __name__ == "__main__":
    main()
