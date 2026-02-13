from __future__ import annotations

import subprocess
import sys
from pathlib import Path

import pytest

TESTS_DIR = Path(__file__).parent
JS_TEST_FILE = TESTS_DIR / "terminal_text_tests.mjs"


@pytest.fixture(scope="module")
def node_bin() -> str:
    for name in ("node", "node22", "node20"):
        try:
            result = subprocess.run(
                [name, "--version"],
                capture_output=True,
                text=True,
                timeout=5,
            )
            if result.returncode == 0:
                return name
        except FileNotFoundError:
            continue
    pytest.skip("node not found")


def test_terminal_text_normalizer(node_bin: str) -> None:
    result = subprocess.run(
        [node_bin, str(JS_TEST_FILE)],
        capture_output=True,
        text=True,
        timeout=30,
    )
    if result.returncode != 0:
        pytest.fail(
            f"terminal-text.js tests failed:\n{result.stdout}\n{result.stderr}"
        )
    assert "0 failed" in result.stdout, result.stdout
