from __future__ import annotations

from pathlib import Path
from types import SimpleNamespace

import pytest

import tabula.protocol as protocol


def test_bootstrap_preserves_existing_agents_and_writes_sidecar(tmp_path) -> None:
    agents = tmp_path / "AGENTS.md"
    agents.write_text("# AGENTS\n\ncustom\n", encoding="utf-8")

    result = protocol.bootstrap_project(tmp_path)
    assert result.agents_preserved is True
    assert agents.read_text(encoding="utf-8") == "# AGENTS\n\ncustom\n"
    sidecar = tmp_path / ".tabula" / "AGENTS.tabula.md"
    assert sidecar.exists()
    text = sidecar.read_text(encoding="utf-8")
    assert protocol.AGENTS_PROTOCOL_BEGIN in text
    assert protocol.AGENTS_PROTOCOL_END in text


def test_ensure_gitignore_idempotent_when_patterns_present(tmp_path: Path) -> None:
    gitignore = tmp_path / ".gitignore"
    gitignore.write_text(
        "\n".join(protocol.GITIGNORE_BINARY_PATTERNS) + "\n",
        encoding="utf-8",
    )
    protocol._ensure_gitignore(tmp_path)
    lines = gitignore.read_text(encoding="utf-8").splitlines()
    assert lines == protocol.GITIGNORE_BINARY_PATTERNS


def test_ensure_gitignore_appends_with_separator(tmp_path: Path) -> None:
    gitignore = tmp_path / ".gitignore"
    gitignore.write_text("custom\n", encoding="utf-8")
    protocol._ensure_gitignore(tmp_path)
    text = gitignore.read_text(encoding="utf-8")
    assert "custom\n\n.tabula/artifacts/" in text


def test_bootstrap_raises_when_git_init_fails(tmp_path: Path, monkeypatch) -> None:
    def fake_run(*args, **kwargs):
        return SimpleNamespace(returncode=1, stdout="", stderr="git failed")

    monkeypatch.setattr(protocol.subprocess, "run", fake_run)
    with pytest.raises(RuntimeError, match="git failed"):
        protocol.bootstrap_project(tmp_path)
