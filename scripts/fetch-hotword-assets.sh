#!/usr/bin/env bash
set -euo pipefail

PLATFORM="$(uname -s)"
if [ "$PLATFORM" = "Darwin" ]; then
  DEFAULT_DATA_ROOT="${HOME}/Library/Application Support/tabura"
else
  DEFAULT_DATA_ROOT="${XDG_DATA_HOME:-$HOME/.local/share}/tabura"
fi

DATA_DIR="${TABURA_WEB_DATA_DIR:-${TABURA_DATA_DIR:-${DEFAULT_DATA_ROOT}/web-data}}"
RUNTIME_DIR="${TABURA_HOTWORD_RUNTIME_DIR:-${DATA_DIR}/hotword-runtime}"
TRAIN_DIR="${TABURA_HOTWORD_TRAIN_DIR:-${DATA_DIR}/hotword-train}"
MODEL_PATH="${RUNTIME_DIR}/keyword.onnx"
META_PATH="${TRAIN_DIR}/active-model.json"
DOWNLOAD_URL="${TABURA_HOTWORD_DOWNLOAD_URL:-https://raw.githubusercontent.com/fwartner/home-assistant-wakewords-collection/main/en/computer/computer_v2.onnx}"
SOURCE_URL="${TABURA_HOTWORD_SOURCE_URL:-https://github.com/fwartner/home-assistant-wakewords-collection}"
README_URL="${TABURA_HOTWORD_README_URL:-https://github.com/fwartner/home-assistant-wakewords-collection/tree/main/en/computer}"
CATALOG_KEY="${TABURA_HOTWORD_CATALOG_KEY:-home-assistant-community:en/computer/computer_v2.onnx}"
DISPLAY_NAME="${TABURA_HOTWORD_DISPLAY_NAME:-Computer V2}"
PHRASE="${TABURA_HOTWORD_PHRASE:-computer}"
SOURCE_LABEL="${TABURA_HOTWORD_SOURCE_LABEL:-Home Assistant Community}"
UPSTREAM_FILE="${TABURA_HOTWORD_UPSTREAM_FILE:-computer_v2.onnx}"
REFRESH="${TABURA_HOTWORD_REFRESH:-0}"

log() {
  printf '[hotword-assets] %s\n' "$*"
}

mkdir -p "$RUNTIME_DIR" "$TRAIN_DIR"

if [ "$REFRESH" != "1" ] && [ -s "$MODEL_PATH" ]; then
  log "wake word already present at $MODEL_PATH"
else
  tmp_path="${MODEL_PATH}.tmp"
  rm -f "$tmp_path"
  log "downloading ${DISPLAY_NAME} to $MODEL_PATH"
  curl -fL --retry 3 --retry-delay 2 -o "$tmp_path" "$DOWNLOAD_URL"
  mv "$tmp_path" "$MODEL_PATH"
fi

rm -f "${MODEL_PATH}.data"

cat >"$META_PATH" <<EOF
{"catalog_key":"${CATALOG_KEY}","display_name":"${DISPLAY_NAME}","phrase":"${PHRASE}","source":"${SOURCE_LABEL}","source_url":"${SOURCE_URL}","readme_url":"${README_URL}","download_url":"${DOWNLOAD_URL}","upstream_file":"${UPSTREAM_FILE}","has_external_data":false}
EOF

log "active wake word: ${DISPLAY_NAME} (${PHRASE})"
log "runtime model: $MODEL_PATH"
