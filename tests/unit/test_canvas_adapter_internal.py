from __future__ import annotations

import io
import json
from pathlib import Path

import pytest

import tabula.canvas_adapter as adapter_module
from tabula.canvas_adapter import CanvasAdapter, has_display, launch_canvas_background


def test_has_display_detects_display_or_wayland() -> None:
    assert has_display({"DISPLAY": ":0"}) is True
    assert has_display({"WAYLAND_DISPLAY": "wayland-0"}) is True
    assert has_display({}) is False


def test_launch_canvas_background_uses_expected_command(monkeypatch, tmp_path: Path) -> None:
    seen: dict[str, object] = {}

    class FakeProc:
        stdin = None
        stdout = io.BytesIO()
        stderr = io.BytesIO()
        pid = 12345

        def poll(self):
            return 0

    def fake_popen(cmd, cwd=None, stdin=None, stdout=None, stderr=None):
        seen["cmd"] = cmd
        seen["cwd"] = cwd
        seen["stdin"] = stdin
        seen["stdout"] = stdout
        seen["stderr"] = stderr
        return FakeProc()

    monkeypatch.setattr(adapter_module.subprocess, "Popen", fake_popen)
    proc = launch_canvas_background(tmp_path, poll_interval_ms=321)
    assert proc is not None
    assert seen["cwd"] == tmp_path
    cmd = seen["cmd"]
    assert cmd[1:4] == ["-m", "tabula", "canvas"]
    assert "--poll-ms" in cmd
    assert seen["stdout"] == adapter_module.subprocess.PIPE
    assert seen["stderr"] == adapter_module.subprocess.PIPE


def test_canvas_process_launch_and_reuse(monkeypatch, tmp_path: Path) -> None:
    launched = {"count": 0}

    class FakeProc:
        stdin = None
        stdout = io.BytesIO()
        stderr = io.BytesIO()

        def __init__(self, poll_value: int | None, pid: int):
            self._poll_value = poll_value
            self.pid = pid

        def poll(self):
            return self._poll_value

    def fake_launch(project_dir: Path, *, poll_interval_ms: int = 250):
        launched["count"] += 1
        return FakeProc(None, 20000 + launched["count"])

    monkeypatch.setattr(adapter_module, "launch_canvas_background", fake_launch)
    adapter = CanvasAdapter(
        project_dir=tmp_path,
        headless=False,
        start_canvas=True,
        env={"DISPLAY": ":0"},
    )

    adapter.canvas_activate(session_id="s1")
    adapter.canvas_activate(session_id="s1")
    assert launched["count"] == 1

    adapter._canvas_proc = FakeProc(1, 99999)  # type: ignore[attr-defined]
    adapter.canvas_activate(session_id="s1")
    assert launched["count"] == 2


def test_fresh_canvas_mode_terminates_stale_pid_before_launch(monkeypatch, tmp_path: Path) -> None:
    seen: dict[str, int] = {"cleanup": 0}

    class FakeProc:
        stdin = None
        stdout = io.BytesIO()
        stderr = io.BytesIO()
        pid = 1234

        def poll(self):
            return None

    def fake_launch(project_dir: Path, *, poll_interval_ms: int = 250):
        return FakeProc()

    def fake_cleanup(self):
        seen["cleanup"] += 1

    monkeypatch.setattr(adapter_module, "launch_canvas_background", fake_launch)
    monkeypatch.setattr(CanvasAdapter, "_terminate_stale_canvas_from_pid_file", fake_cleanup)

    adapter = CanvasAdapter(
        project_dir=tmp_path,
        headless=False,
        fresh_canvas=True,
        start_canvas=True,
        env={"DISPLAY": ":0"},
    )
    adapter.canvas_activate(session_id="s1")
    assert seen["cleanup"] == 1


def test_canvas_launch_failure_is_reported_in_activation_and_status(monkeypatch, tmp_path: Path) -> None:
    class FakeProc:
        stdin = None
        stdout = io.BytesIO()
        stderr = io.BytesIO(b"ModuleNotFoundError: No module named 'PySide6'")
        pid = 777

        def poll(self):
            return 1

    monkeypatch.setattr(adapter_module, "launch_canvas_background", lambda _project_dir, poll_interval_ms=250: FakeProc())
    adapter = CanvasAdapter(
        project_dir=tmp_path,
        headless=False,
        start_canvas=True,
        env={"DISPLAY": ":0"},
    )

    activation = adapter.canvas_activate(session_id="s1")
    status = adapter.canvas_status(session_id="s1")

    assert activation["canvas_process_alive"] is False
    assert status["canvas_process_alive"] is False
    assert "PySide6" in str(activation["canvas_launch_error"])
    assert "PySide6" in str(status["canvas_launch_error"])


def test_canvas_activate_requires_nonempty_session_id(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_activate(session_id=" ")


def test_render_validates_inputs(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_render_text(session_id="s1", title=" ", markdown_or_text="x")
    with pytest.raises(ValueError):
        adapter.canvas_render_text(session_id="s1", title="t", markdown_or_text=1)  # type: ignore[arg-type]
    with pytest.raises(ValueError):
        adapter.canvas_render_image(session_id="s1", title="", path="x")
    with pytest.raises(ValueError):
        adapter.canvas_render_image(session_id="s1", title="img", path=" ")
    with pytest.raises(ValueError):
        adapter.canvas_render_pdf(session_id="s1", title="", path="x.pdf", page=0)
    with pytest.raises(ValueError):
        adapter.canvas_render_pdf(session_id="s1", title="doc", path="", page=0)
    with pytest.raises(ValueError):
        adapter.canvas_render_pdf(session_id="s1", title="doc", path="x.pdf", page=-1)


def test_history_limit_must_be_positive(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    with pytest.raises(ValueError):
        adapter.canvas_history(session_id="s1", limit=0)


def test_text_selection_feedback_updates_status_and_selection_tool(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    render = adapter.canvas_render_text(session_id="s1", title="draft", markdown_or_text="a\nb\nc")

    adapter._handle_canvas_feedback_line(
        json.dumps(
            {
                "kind": "text_selection",
                "event_id": render["artifact_id"],
                "line_start": 2,
                "line_end": 2,
                "text": "b",
            }
        )
    )

    status = adapter.canvas_status(session_id="s1")
    selection = adapter.canvas_selection(session_id="s1")
    assert status["selection"]["has_selection"] is True
    assert status["selection"]["line_start"] == 2
    assert status["selection"]["line_end"] == 2
    assert status["selection"]["text"] == "b"
    assert selection["selection"]["has_selection"] is True
    assert selection["selection"]["text"] == "b"


def test_text_selection_feedback_is_ignored_for_non_active_event(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    first = adapter.canvas_render_text(session_id="s1", title="first", markdown_or_text="one")
    adapter.canvas_render_text(session_id="s1", title="second", markdown_or_text="two")

    adapter._handle_canvas_feedback_line(
        json.dumps(
            {
                "kind": "text_selection",
                "event_id": first["artifact_id"],
                "line_start": 1,
                "line_end": 1,
                "text": "one",
            }
        )
    )
    assert adapter.canvas_selection(session_id="s1")["selection"]["has_selection"] is False


def test_text_selection_feedback_can_clear_selection(tmp_path: Path) -> None:
    adapter = CanvasAdapter(project_dir=tmp_path, headless=True, start_canvas=False)
    render = adapter.canvas_render_text(session_id="s1", title="draft", markdown_or_text="abc")
    adapter._handle_canvas_feedback_line(
        json.dumps(
            {
                "kind": "text_selection",
                "event_id": render["artifact_id"],
                "line_start": 1,
                "line_end": 1,
                "text": "a",
            }
        )
    )
    assert adapter.canvas_selection(session_id="s1")["selection"]["has_selection"] is True

    adapter._handle_canvas_feedback_line(
        json.dumps(
            {
                "kind": "text_selection",
                "event_id": render["artifact_id"],
                "line_start": None,
                "line_end": None,
                "text": None,
            }
        )
    )
    assert adapter.canvas_selection(session_id="s1")["selection"]["has_selection"] is False
