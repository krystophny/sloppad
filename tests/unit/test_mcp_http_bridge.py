from __future__ import annotations

import io
import json
import sys
from typing import Any

from tabula.mcp_http_bridge import run_mcp_http_bridge


class _FakeResponse:
    def __init__(self, payload: dict[str, Any]) -> None:
        self._raw = json.dumps(payload).encode("utf-8")

    def __enter__(self) -> _FakeResponse:
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        return None

    def read(self) -> bytes:
        return self._raw


def test_bridge_forwards_request_and_returns_backend_payload(monkeypatch) -> None:
    captured: dict[str, Any] = {}

    def _fake_urlopen(request, timeout: float):
        captured["url"] = request.full_url
        captured["body"] = request.data.decode("utf-8")
        return _FakeResponse({"jsonrpc": "2.0", "id": 7, "result": {"ok": True}})

    stdin = io.StringIO('{"jsonrpc":"2.0","id":7,"method":"ping","params":{}}\n')
    stdout = io.StringIO()
    monkeypatch.setattr("urllib.request.urlopen", _fake_urlopen)
    monkeypatch.setattr(sys, "stdin", stdin)
    monkeypatch.setattr(sys, "stdout", stdout)

    rc = run_mcp_http_bridge(mcp_url="http://127.0.0.1:9420/mcp")

    assert rc == 0
    assert captured["url"] == "http://127.0.0.1:9420/mcp"
    assert json.loads(captured["body"])["method"] == "ping"
    response = json.loads(stdout.getvalue().strip())
    assert response["id"] == 7
    assert response["result"]["ok"] is True


def test_bridge_emits_parse_error_for_invalid_json(monkeypatch) -> None:
    stdin = io.StringIO("{not-json}\n")
    stdout = io.StringIO()
    monkeypatch.setattr(sys, "stdin", stdin)
    monkeypatch.setattr(sys, "stdout", stdout)

    rc = run_mcp_http_bridge(mcp_url="http://127.0.0.1:9420/mcp")

    assert rc == 0
    response = json.loads(stdout.getvalue().strip())
    assert response["error"]["code"] == -32700
    assert "bridge parse error" in response["error"]["message"]
