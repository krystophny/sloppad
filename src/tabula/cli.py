from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

from .events import event_schema
from .mcp_server import run_mcp_stdio_server
from .protocol import bootstrap_project


def _build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="tabula")
    sub = parser.add_subparsers(dest="command", required=True)

    p_canvas = sub.add_parser("canvas", help="launch canvas window")
    p_canvas.add_argument("--poll-ms", type=int, default=250)

    sub.add_parser("schema", help="print JSON schema")

    p_bootstrap = sub.add_parser("bootstrap", help="initialize tabula protocol files")
    p_bootstrap.add_argument("--project-dir", type=Path, default=Path("."))

    p_mcp = sub.add_parser("mcp-server", help="run tabula-canvas MCP server over stdio")
    p_mcp.add_argument("--project-dir", type=Path, default=Path("."))
    p_mcp.add_argument("--headless", action="store_true")
    p_mcp.add_argument("--no-canvas", action="store_true")
    p_mcp.add_argument("--poll-ms", type=int, default=250)

    p_run = sub.add_parser("run", help="launch interactive codex with tabula MCP preconfigured")
    p_run.add_argument("--project-dir", type=Path, default=Path("."))
    p_run.add_argument("--headless", action="store_true")
    p_run.add_argument("--no-canvas", action="store_true")
    p_run.add_argument("--poll-ms", type=int, default=250)
    p_run.add_argument("prompt", nargs="?", default=None)
    return parser


def _cmd_canvas(poll_ms: int) -> int:
    try:
        from .window import run_canvas
    except ModuleNotFoundError:
        print(
            "PySide6 is required for 'tabula canvas'. Install with: python -m pip install -e .[gui]",
            file=sys.stderr,
        )
        return 2
    return run_canvas(poll_interval_ms=poll_ms)


def _cmd_schema() -> int:
    print(json.dumps(event_schema(), indent=2, sort_keys=True))
    return 0


def _cmd_bootstrap(project_dir: Path) -> int:
    try:
        result = bootstrap_project(project_dir)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    print(f"project prepared: {result.paths.project_dir}")
    print(f"agents protocol: {result.paths.agents_path}")
    print(f"mcp config snippet: {result.paths.mcp_config_path}")
    if result.git_initialized:
        print("git initialized")
    return 0


def _cmd_mcp_server(project_dir: Path, headless: bool, no_canvas: bool, poll_ms: int) -> int:
    try:
        bootstrap = bootstrap_project(project_dir)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    return run_mcp_stdio_server(
        project_dir=bootstrap.paths.project_dir,
        headless=headless,
        poll_interval_ms=poll_ms,
        start_canvas=not no_canvas,
    )


def _cmd_run(
    project_dir: Path,
    *,
    headless: bool,
    no_canvas: bool,
    poll_ms: int,
    prompt: str | None,
) -> int:
    try:
        bootstrap = bootstrap_project(project_dir)
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 1

    target = bootstrap.paths.project_dir
    mcp_args = [
        "-m",
        "tabula",
        "mcp-server",
        "--project-dir",
        target.as_posix(),
        "--poll-ms",
        str(poll_ms),
    ]
    if headless:
        mcp_args.append("--headless")
    if no_canvas:
        mcp_args.append("--no-canvas")

    codex_cmd = [
        "codex",
        "--no-alt-screen",
        "--yolo",
        "--search",
        "-C",
        str(target),
        "-c",
        f"mcp_servers.tabula-canvas.command={json.dumps(sys.executable)}",
        "-c",
        f"mcp_servers.tabula-canvas.args={json.dumps(mcp_args)}",
    ]
    if prompt:
        codex_cmd.append(prompt)

    try:
        return subprocess.run(codex_cmd).returncode
    except FileNotFoundError:
        print("codex CLI not found on PATH", file=sys.stderr)
        return 1


def main(argv: list[str] | None = None) -> int:
    parser = _build_parser()
    raw_argv = list(sys.argv[1:] if argv is None else argv)
    if not raw_argv:
        parser.print_help(sys.stderr)
        return 2

    args = parser.parse_args(raw_argv)
    if args.command == "canvas":
        return _cmd_canvas(args.poll_ms)
    if args.command == "schema":
        return _cmd_schema()
    if args.command == "bootstrap":
        return _cmd_bootstrap(args.project_dir)
    if args.command == "mcp-server":
        return _cmd_mcp_server(args.project_dir, args.headless, args.no_canvas, args.poll_ms)
    if args.command == "run":
        return _cmd_run(
            args.project_dir,
            headless=args.headless,
            no_canvas=args.no_canvas,
            poll_ms=args.poll_ms,
            prompt=args.prompt,
        )

    parser.error("unknown command")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
