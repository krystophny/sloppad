#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

"$SCRIPT_DIR/setup-codex-mcp.sh"
"$SCRIPT_DIR/setup-claude-mcp.sh"

echo "Configured Codex and Claude to use local stdio MCP servers: sloppy, helpy"
