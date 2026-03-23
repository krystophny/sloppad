#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DEFAULT_PYTHON="$ROOT_DIR/tools/qwen3-tts/.venv/bin/python"
DEFAULT_SCRIPT="$ROOT_DIR/scripts/hotword-generate-qwen3tts.py"

if [[ -n "${TABURA_HOTWORD_QWEN3TTS_COMMAND:-}" ]]; then
  exec "${TABURA_HOTWORD_QWEN3TTS_COMMAND}" "$@"
fi

if [[ -x "$DEFAULT_PYTHON" && -f "$DEFAULT_SCRIPT" ]]; then
  exec "$DEFAULT_PYTHON" "$DEFAULT_SCRIPT" "$@"
fi

echo "Qwen3-TTS generator is not installed. Set TABURA_HOTWORD_QWEN3TTS_COMMAND or install tools/qwen3-tts/.venv." >&2
exit 1
