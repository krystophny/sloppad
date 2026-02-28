#!/usr/bin/env bash
set -euo pipefail

MODEL_DIR="${TABURA_STT_MODEL_DIR:-$HOME/.local/share/tabura-stt/models}"
MODEL_FILE="${TABURA_STT_MODEL_FILE:-ggml-large-v3-turbo.bin}"
MODEL_URL="${TABURA_STT_MODEL_URL:-https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-large-v3-turbo.bin}"
SERVER_BIN="${WHISPER_SERVER_BIN:-whisper-server}"
HOST="${TABURA_STT_HOST:-127.0.0.1}"
PORT="${TABURA_STT_PORT:-8427}"
LANGUAGE="${TABURA_STT_LANGUAGE:-en,de}"
THREADS="${TABURA_STT_THREADS:-4}"
PROMPT="${TABURA_STT_PROMPT:-}"

# Check for existing model from voxtype to avoid re-downloading.
VOXTYPE_MODEL="$HOME/.local/share/voxtype/models/$MODEL_FILE"

if ! command -v "$SERVER_BIN" >/dev/null 2>&1; then
  echo "whisper.cpp server binary not found: $SERVER_BIN" >&2
  echo "Install whisper.cpp and ensure whisper-server is on PATH (or set WHISPER_SERVER_BIN)." >&2
  exit 1
fi

mkdir -p "$MODEL_DIR"
MODEL_PATH="$MODEL_DIR/$MODEL_FILE"
if [ ! -s "$MODEL_PATH" ]; then
  if [ -s "$VOXTYPE_MODEL" ]; then
    echo "Symlinking existing voxtype model: $VOXTYPE_MODEL -> $MODEL_PATH"
    ln -sf "$VOXTYPE_MODEL" "$MODEL_PATH"
  else
    echo "Downloading $MODEL_FILE to $MODEL_PATH"
    curl -fL --retry 3 --retry-delay 2 -o "$MODEL_PATH.tmp" "$MODEL_URL"
    mv "$MODEL_PATH.tmp" "$MODEL_PATH"
  fi
fi

echo "Starting whisper STT sidecar at http://$HOST:$PORT"
LANGUAGE_TRIMMED="$(printf '%s' "$LANGUAGE" | tr -d '[:space:]')"
if [[ "$LANGUAGE_TRIMMED" == *,* ]]; then
  IFS=',' read -r -a _langs <<< "$LANGUAGE_TRIMMED"
  _joined=""
  for _lang in "${_langs[@]}"; do
    if [ -z "$_lang" ]; then
      continue
    fi
    if [ -z "$_joined" ]; then
      _joined="$_lang"
    else
      _joined="$_joined, $_lang"
    fi
  done
  LANGUAGE="auto"
  if [ -n "$_joined" ] && [ -z "$PROMPT" ]; then
    PROMPT="The spoken language is one of: ${_joined}."
  fi
fi

args=(
  -m "$MODEL_PATH"
  --host "$HOST"
  --port "$PORT"
  -l "$LANGUAGE"
  --threads "$THREADS"
)
if [ -n "$PROMPT" ]; then
  args+=(--prompt "$PROMPT")
fi

exec "$SERVER_BIN" "${args[@]}"
