from __future__ import annotations

import json
import urllib.request
from pathlib import Path

import pytest

pytest.importorskip("playwright")
from playwright.sync_api import Page, TimeoutError as PlaywrightTimeoutError, expect

TEST_PASSWORD = "e2e-test-pw-42"
TERMINAL_READY_TIMEOUT = 15_000
MARKER_TIMEOUT = 20_000

_TERM_BUFFER_JS = """() => {
    const term = window._tabulaTerminal;
    if (!term) return '';
    const lines = [];
    for (let i = 0; i < term.buffer.active.length; i++) {
        const line = term.buffer.active.getLine(i);
        if (line) lines.push(line.translateToString(true));
    }
    return lines.join('\\n');
}"""


def _login(page: Page, base_url: str) -> None:
    page.goto(base_url)
    page.wait_for_selector("#view-auth", state="visible", timeout=5_000)
    page.fill("#auth-password", TEST_PASSWORD)
    page.click("#auth-submit")
    page.wait_for_selector("#view-main", state="visible", timeout=10_000)


def _wait_terminal_ready(page: Page) -> None:
    page.wait_for_function(
        """() => {
            const term = window._tabulaTerminal;
            if (!term) return false;
            for (let i = 0; i < term.rows; i++) {
                const line = term.buffer.active.getLine(i);
                if (line && line.translateToString(true).trim().length > 0)
                    return true;
            }
            return false;
        }""",
        timeout=TERMINAL_READY_TIMEOUT,
    )


def _wait_for_marker(page: Page, marker: str, timeout: int = MARKER_TIMEOUT) -> None:
    try:
        page.wait_for_function(
            f"""(marker) => {{
                const term = window._tabulaTerminal;
                if (!term) return false;
                for (let i = 0; i < term.buffer.active.length; i++) {{
                    const line = term.buffer.active.getLine(i);
                    if (line && line.translateToString(true).includes(marker))
                        return true;
                }}
                return false;
            }}""",
            arg=marker,
            timeout=timeout,
        )
    except PlaywrightTimeoutError as exc:
        buf = page.evaluate(_TERM_BUFFER_JS)
        tail = "\n".join(buf.splitlines()[-30:])
        raise AssertionError(
            f"marker '{marker}' not found in terminal output tail:\n{tail}"
        ) from exc


def _type_in_terminal(page: Page, text: str) -> None:
    page.click("#terminal-container")
    page.keyboard.type(text, delay=30)


def _screenshot(page: Page, name: str, screenshot_dir: Path) -> None:
    page.screenshot(path=str(screenshot_dir / name))


def test_setup_password_flow(page: Page, base_url: str, screenshot_dir: Path) -> None:
    page.goto(base_url)
    page.wait_for_selector("#view-auth", state="visible", timeout=5_000)

    expect(page.locator("#auth-subtitle")).to_contain_text("Set up")
    expect(page.locator("#auth-submit")).to_have_text("Set Password")
    _screenshot(page, "auth_setup.png", screenshot_dir)

    page.fill("#auth-password", TEST_PASSWORD)
    page.click("#auth-submit")
    page.wait_for_selector("#view-main", state="visible", timeout=10_000)
    _screenshot(page, "main_after_setup.png", screenshot_dir)


def test_login_flow(page: Page, base_url: str, screenshot_dir: Path) -> None:
    page.goto(base_url)
    page.wait_for_selector("#view-auth", state="visible", timeout=5_000)

    expect(page.locator("#auth-subtitle")).to_contain_text("Enter your admin password")
    expect(page.locator("#auth-submit")).to_have_text("Login")
    _screenshot(page, "login.png", screenshot_dir)

    page.fill("#auth-password", TEST_PASSWORD)
    page.click("#auth-submit")
    page.wait_for_selector("#view-main", state="visible", timeout=10_000)
    _screenshot(page, "main_after_login.png", screenshot_dir)


def test_local_terminal_renders(page: Page, base_url: str, screenshot_dir: Path) -> None:
    _login(page, base_url)
    _wait_terminal_ready(page)

    expect(page.locator("#terminal-container .xterm")).to_be_visible()
    _screenshot(page, "terminal_prompt.png", screenshot_dir)


def test_terminal_echo(page: Page, base_url: str, screenshot_dir: Path) -> None:
    _login(page, base_url)
    _wait_terminal_ready(page)

    _type_in_terminal(page, "echo PLAYWRIGHT_MARKER\n")
    _wait_for_marker(page, "PLAYWRIGHT_MARKER")
    _screenshot(page, "terminal_echo.png", screenshot_dir)


def test_terminal_resize_on_connect(
    page: Page, base_url: str, screenshot_dir: Path
) -> None:
    _login(page, base_url)
    _wait_terminal_ready(page)

    dims = page.evaluate(
        "() => ({ rows: window._tabulaTerminal.rows, cols: window._tabulaTerminal.cols })"
    )

    _type_in_terminal(page, "stty size\n")

    expected = f"{dims['rows']} {dims['cols']}"
    _wait_for_marker(page, expected)
    _screenshot(page, "terminal_resize.png", screenshot_dir)


def test_launch_claude(
    page: Page, base_url: str, screenshot_dir: Path, server_info: dict
) -> None:
    _login(page, base_url)
    _wait_terminal_ready(page)

    page.select_option("#assistant-select", "claude")
    page.click("#btn-launch-ai")
    _wait_for_marker(page, "MOCK_CLAUDE_OK")
    _screenshot(page, "launch_claude.png", screenshot_dir)


def test_launch_codex(
    page: Page, base_url: str, screenshot_dir: Path, server_info: dict
) -> None:
    _login(page, base_url)
    _wait_terminal_ready(page)

    page.select_option("#assistant-select", "codex")
    page.click("#btn-launch-ai")
    _wait_for_marker(page, "MOCK_CODEX_OK")
    _screenshot(page, "launch_codex.png", screenshot_dir)


def test_canvas_artifact_display(
    page: Page, base_url: str, screenshot_dir: Path, server_info: dict
) -> None:
    _login(page, base_url)
    _wait_terminal_ready(page)
    page.wait_for_timeout(1_000)

    daemon_url = f"http://127.0.0.1:{server_info['daemon_port']}/mcp"

    def _mcp_call(msg_id: int, method: str, params: dict) -> None:
        body = json.dumps(
            {"jsonrpc": "2.0", "id": msg_id, "method": method, "params": params}
        ).encode()
        req = urllib.request.Request(
            daemon_url,
            method="POST",
            headers={"Content-Type": "application/json"},
            data=body,
        )
        urllib.request.urlopen(req, timeout=5)

    _mcp_call(
        1,
        "initialize",
        {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "e2e-test"},
        },
    )
    _mcp_call(
        2,
        "tools/call",
        {
            "name": "canvas_render_text",
            "arguments": {
                "session_id": "e2e-test",
                "title": "E2E Test Artifact",
                "markdown_or_text": "# Hello Playwright\n\nThis is a test artifact.",
            },
        },
    )

    page.wait_for_selector("#canvas-text", state="visible", timeout=10_000)
    page.wait_for_function(
        """() => {
            const el = document.getElementById('canvas-text');
            return el && el.textContent.includes('Hello Playwright');
        }""",
        timeout=10_000,
    )
    expect(page.locator("#canvas-mode")).to_have_text("review")
    _screenshot(page, "canvas_artifact.png", screenshot_dir)


def test_mobile_viewport(page: Page, base_url: str, screenshot_dir: Path) -> None:
    page.set_viewport_size({"width": 375, "height": 812})
    _login(page, base_url)

    direction = page.evaluate(
        "() => getComputedStyle(document.getElementById('workspace')).flexDirection"
    )
    assert direction == "column", f"expected column layout, got {direction}"
    _screenshot(page, "mobile_layout.png", screenshot_dir)


def test_logout_clears_session(
    page: Page, base_url: str, screenshot_dir: Path
) -> None:
    _login(page, base_url)
    page.click("#btn-logout")
    page.wait_for_selector("#view-auth", state="visible", timeout=5_000)
    _screenshot(page, "after_logout.png", screenshot_dir)
