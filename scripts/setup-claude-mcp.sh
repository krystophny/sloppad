#!/usr/bin/env bash
set -euo pipefail

SETTINGS_PATH="${CLAUDE_SETTINGS_PATH:-$HOME/.claude/settings.json}"
SLOPTOOLS_BIN="${SLOPSHELL_SLOPTOOLS_BIN:-$HOME/.local/bin/sloptools}"
HELPY_BIN="${SLOPSHELL_HELPY_BIN:-$HOME/.local/bin/helpy}"
SLOPPY_PROJECT_DIR="${SLOPSHELL_SLOPPY_PROJECT_DIR:-$HOME}"
SLOPPY_DATA_DIR="${SLOPSHELL_SLOPPY_DATA_DIR:-$HOME/.local/share/sloppy}"

mkdir -p "$(dirname "$SETTINGS_PATH")"
if [[ -f "$SETTINGS_PATH" ]]; then
  cp "$SETTINGS_PATH" "$SETTINGS_PATH.bak.$(date +%Y%m%d%H%M%S)"
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "error: jq is required for $0" >&2
  exit 1
fi

BASE_JSON='{}'
if [[ -f "$SETTINGS_PATH" ]] && [[ -s "$SETTINGS_PATH" ]]; then
  BASE_JSON="$(cat "$SETTINGS_PATH")"
fi

TMP_OUT="$(mktemp)"
cleanup() {
  rm -f "$TMP_OUT"
}
trap cleanup EXIT

printf '%s\n' "$BASE_JSON" | jq -S \
  --arg sloptools_bin "$SLOPTOOLS_BIN" \
  --arg helpy_bin "$HELPY_BIN" \
  --arg project_dir "$SLOPPY_PROJECT_DIR" \
  --arg data_dir "$SLOPPY_DATA_DIR" '
  if type != "object" then
    error("settings root must be a JSON object")
  else
    .
  end
  | .mcpServers = (.mcpServers // {})
  | .mcpServers |= (if type == "object" then . else {} end)
  | .mcpServers.sloppy = {
      "type": "stdio",
      "command": $sloptools_bin,
      "args": ["mcp-server", "--project-dir", $project_dir, "--data-dir", $data_dir]
    }
  | .mcpServers.helpy = {
      "type": "stdio",
      "command": $helpy_bin,
      "args": ["mcp-stdio"]
    }
  | del(.mcpServers.sloptools)
  | del(.mcpServers.slopshell)
' >"$TMP_OUT"

mv "$TMP_OUT" "$SETTINGS_PATH"
echo "updated $SETTINGS_PATH"
echo "server keys: mcpServers.sloppy, mcpServers.helpy"
