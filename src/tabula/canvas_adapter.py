from __future__ import annotations

import json
import os
import signal
import subprocess
import sys
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from pathlib import Path
from typing import Mapping
from uuid import uuid4

from .events import CanvasEvent, event_to_payload, parse_event_payload
from .state import CanvasState, reduce_state


def has_display(env: Mapping[str, str] | None = None) -> bool:
    source = env or os.environ
    return bool(source.get("DISPLAY") or source.get("WAYLAND_DISPLAY"))


def launch_canvas_background(project_dir: Path, *, poll_interval_ms: int = 250) -> subprocess.Popen[bytes]:
    cmd = [
        sys.executable,
        "-m",
        "tabula",
        "canvas",
        "--poll-ms",
        str(poll_interval_ms),
    ]
    return subprocess.Popen(cmd, cwd=project_dir, stdin=subprocess.PIPE)


@dataclass
class SessionRecord:
    state: CanvasState
    activated: bool
    history: list[CanvasEvent] = field(default_factory=list)


class CanvasAdapter:
    def __init__(
        self,
        *,
        project_dir: Path,
        start_canvas: bool = True,
        headless: bool = False,
        fresh_canvas: bool = False,
        poll_interval_ms: int = 250,
        env: Mapping[str, str] | None = None,
    ) -> None:
        self._project_dir = project_dir.resolve()
        self._start_canvas = start_canvas
        self._headless_override = headless
        self._fresh_canvas = fresh_canvas
        self._poll_interval_ms = poll_interval_ms
        self._env = env

        self._sessions: dict[str, SessionRecord] = {}
        self._canvas_proc: subprocess.Popen[bytes] | None = None

    def _effective_headless(self) -> bool:
        return self._headless_override or not has_display(self._env)

    def _canvas_pid_path(self) -> Path:
        return self._project_dir / ".tabula" / "canvas.pid"

    @staticmethod
    def _is_tabula_canvas_pid(pid: int) -> bool:
        cmdline_path = Path("/proc") / str(pid) / "cmdline"
        try:
            raw = cmdline_path.read_bytes()
        except OSError:
            return False
        text = raw.decode("utf-8", errors="ignore")
        return ("tabula" in text) and ("canvas" in text)

    def _clear_canvas_pid_file(self) -> None:
        try:
            self._canvas_pid_path().unlink()
        except FileNotFoundError:
            return

    def _write_canvas_pid_file(self, pid: int) -> None:
        path = self._canvas_pid_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(str(pid), encoding="utf-8")

    def _terminate_stale_canvas_from_pid_file(self) -> None:
        path = self._canvas_pid_path()
        if not path.exists():
            return

        try:
            pid = int(path.read_text(encoding="utf-8").strip())
        except ValueError:
            self._clear_canvas_pid_file()
            return

        if pid <= 0 or not self._is_tabula_canvas_pid(pid):
            self._clear_canvas_pid_file()
            return

        try:
            os.kill(pid, signal.SIGTERM)
        except ProcessLookupError:
            self._clear_canvas_pid_file()
            return
        except PermissionError:
            return

        deadline = time.time() + 1.0
        while time.time() < deadline:
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
                self._clear_canvas_pid_file()
                return
            except PermissionError:
                return
            time.sleep(0.05)

        try:
            os.kill(pid, signal.SIGKILL)
        except (ProcessLookupError, PermissionError):
            pass
        self._clear_canvas_pid_file()

    def _ensure_canvas_process(self) -> None:
        if self._effective_headless() or not self._start_canvas:
            return
        if self._canvas_proc is not None and self._canvas_proc.poll() is None:
            return
        if self._fresh_canvas:
            self._terminate_stale_canvas_from_pid_file()
        self._canvas_proc = launch_canvas_background(self._project_dir, poll_interval_ms=self._poll_interval_ms)
        if self._canvas_proc.pid > 0:
            self._write_canvas_pid_file(self._canvas_proc.pid)

    def _ensure_session(self, session_id: str) -> SessionRecord:
        if session_id not in self._sessions:
            self._sessions[session_id] = SessionRecord(state=CanvasState(), activated=False)
        return self._sessions[session_id]

    def list_sessions(self) -> list[str]:
        return sorted(self._sessions.keys())

    @staticmethod
    def _base_payload(kind: str) -> dict[str, object]:
        return {
            "event_id": str(uuid4()),
            "ts": datetime.now(timezone.utc).isoformat().replace("+00:00", "Z"),
            "kind": kind,
        }

    def _emit_to_canvas(self, event: CanvasEvent) -> None:
        self._ensure_canvas_process()
        proc = self._canvas_proc
        if proc is None or proc.stdin is None:
            return

        try:
            line = json.dumps(event_to_payload(event), separators=(",", ":")) + "\n"
            proc.stdin.write(line.encode("utf-8"))
            proc.stdin.flush()
        except (BrokenPipeError, OSError):
            self._canvas_proc = None
            self._clear_canvas_pid_file()

    def _record_event(self, session_id: str, event: CanvasEvent) -> SessionRecord:
        record = self._ensure_session(session_id)
        record.state = reduce_state(record.state, event)
        record.history.append(event)
        self._emit_to_canvas(event)
        return record

    def canvas_activate(self, *, session_id: str, mode_hint: str | None = None) -> dict[str, object]:
        if not session_id.strip():
            raise ValueError("session_id must be non-empty")
        # Backward-compatibility mapping for older clients.
        if mode_hint == "discussion":
            mode_hint = "review"
        record = self._ensure_session(session_id)
        record.activated = True
        self._ensure_canvas_process()
        return {
            "active": True,
            "headless": self._effective_headless(),
            "mode": record.state.mode,
            "mode_hint": mode_hint,
        }

    def canvas_render_text(self, *, session_id: str, title: str, markdown_or_text: str) -> dict[str, object]:
        if not title.strip():
            raise ValueError("title must be non-empty")
        if not isinstance(markdown_or_text, str):
            raise ValueError("markdown_or_text must be a string")

        self.canvas_activate(session_id=session_id)
        payload = self._base_payload("text_artifact")
        payload.update({"title": title, "text": markdown_or_text})
        event = parse_event_payload(payload, base_dir=self._project_dir)

        record = self._record_event(session_id, event)
        return {
            "artifact_id": event.event_id,
            "kind": "text_artifact",
            "mode": record.state.mode,
        }

    def canvas_render_image(self, *, session_id: str, title: str, path: str) -> dict[str, object]:
        if not title.strip():
            raise ValueError("title must be non-empty")
        if not isinstance(path, str) or not path.strip():
            raise ValueError("path must be a non-empty string")

        self.canvas_activate(session_id=session_id)
        payload = self._base_payload("image_artifact")
        payload.update({"title": title, "path": path})
        event = parse_event_payload(payload, base_dir=self._project_dir)

        record = self._record_event(session_id, event)
        return {
            "artifact_id": event.event_id,
            "kind": "image_artifact",
            "path": event.path,
            "mode": record.state.mode,
        }

    def canvas_render_pdf(self, *, session_id: str, title: str, path: str, page: int = 0) -> dict[str, object]:
        if not title.strip():
            raise ValueError("title must be non-empty")
        if not isinstance(path, str) or not path.strip():
            raise ValueError("path must be a non-empty string")
        if not isinstance(page, int) or page < 0:
            raise ValueError("page must be integer >= 0")

        self.canvas_activate(session_id=session_id)
        payload = self._base_payload("pdf_artifact")
        payload.update({"title": title, "path": path, "page": page})
        event = parse_event_payload(payload, base_dir=self._project_dir)

        record = self._record_event(session_id, event)
        return {
            "artifact_id": event.event_id,
            "kind": "pdf_artifact",
            "path": event.path,
            "page": event.page,
            "mode": record.state.mode,
        }

    def canvas_clear(self, *, session_id: str, reason: str | None = None) -> dict[str, object]:
        self.canvas_activate(session_id=session_id)
        payload = self._base_payload("clear_canvas")
        if reason is not None:
            payload["reason"] = reason
        event = parse_event_payload(payload, base_dir=self._project_dir)

        record = self._record_event(session_id, event)
        return {"cleared": True, "mode": record.state.mode}

    def canvas_status(self, *, session_id: str) -> dict[str, object]:
        record = self._ensure_session(session_id)
        active_event = record.state.active_event
        event_id = active_event.event_id if active_event is not None else None
        kind = active_event.kind if active_event is not None else None
        return {
            "mode": record.state.mode,
            "active": record.activated,
            "active_event_id": event_id,
            "active_kind": kind,
            "history_size": len(record.history),
            "headless": self._effective_headless(),
        }

    def canvas_history(self, *, session_id: str, limit: int = 20) -> dict[str, object]:
        if not isinstance(limit, int) or limit <= 0:
            raise ValueError("limit must be integer > 0")

        record = self._ensure_session(session_id)
        selected = record.history[-limit:]
        return {
            "session_id": session_id,
            "count": len(selected),
            "events": [event_to_payload(event) for event in selected],
        }
