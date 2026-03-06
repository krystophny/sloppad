#!/usr/bin/env bash
set -euo pipefail

VOXTYPE_BIN="${VOXTYPE_BIN:-voxtype}"
VLLM_BIN="${VLLM_BIN:-vllm}"
HOST="${TABURA_STT_HOST:-127.0.0.1}"
PORT="${TABURA_STT_PORT:-8427}"
LANGUAGE_RAW="${TABURA_STT_LANGUAGE:-en,de}"
THREADS="${TABURA_STT_THREADS:-4}"
PROMPT="${TABURA_STT_PROMPT:-}"
FALLBACK_MODEL="${TABURA_STT_MODEL:-large-v3-turbo}"
PROVIDER_RAW="${TABURA_STT_PROVIDER:-auto}"
VOXTRAL_MODEL="${TABURA_VOXTRAL_MODEL:-mistralai/Voxtral-Mini-3B-2507}"
# Keep the OpenAI request model compatible with existing Tabura callers.
VOXTRAL_SERVED_MODEL="${TABURA_VOXTRAL_SERVED_MODEL:-whisper-1}"
VOXTRAL_DTYPE="${TABURA_VOXTRAL_DTYPE:-auto}"
VOXTRAL_MAX_MODEL_LEN="${TABURA_VOXTRAL_MAX_MODEL_LEN:-32768}"
VOXTRAL_EXTRA_ARGS="${TABURA_VOXTRAL_EXTRA_ARGS:-}"
DRY_RUN="${TABURA_STT_DRY_RUN:-0}"

normalize_provider() {
  local raw
  raw="$(printf '%s' "${1:-}" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
  case "$raw" in
    ""|auto) printf 'auto\n' ;;
    voxtral|mistral|vllm) printf 'voxtral\n' ;;
    voxtype|whisper) printf 'voxtype\n' ;;
    *)
      echo "unsupported TABURA_STT_PROVIDER: $1" >&2
      exit 1
      ;;
  esac
}

run_cmd() {
  if [ "$DRY_RUN" = "1" ]; then
    printf 'DRY_RUN:'
    for arg in "$@"; do
      printf ' %q' "$arg"
    done
    printf '\n'
    return 0
  fi
  exec "$@"
}

can_run_voxtral() {
  command -v "$VLLM_BIN" >/dev/null 2>&1 || return 1
  command -v python3 >/dev/null 2>&1 || return 1
  python3 - <<'PY' >/dev/null 2>&1
import importlib.util, sys
required = ("vllm", "mistral_common")
ok = all(importlib.util.find_spec(name) is not None for name in required)
sys.exit(0 if ok else 1)
PY
}

start_voxtral() {
  echo "Starting Voxtral STT service at http://$HOST:$PORT (model=$VOXTRAL_MODEL served-model=$VOXTRAL_SERVED_MODEL)"
  local -a args
  args=(
    serve
    "$VOXTRAL_MODEL"
    --host "$HOST"
    --port "$PORT"
    --served-model-name "$VOXTRAL_SERVED_MODEL"
  )
  if [ -n "$VOXTRAL_DTYPE" ]; then
    args+=(--dtype "$VOXTRAL_DTYPE")
  fi
  if [ -n "$VOXTRAL_MAX_MODEL_LEN" ]; then
    args+=(--max-model-len "$VOXTRAL_MAX_MODEL_LEN")
  fi
  if [ -n "$VOXTRAL_EXTRA_ARGS" ]; then
    # shellcheck disable=SC2206
    local extra=( $VOXTRAL_EXTRA_ARGS )
    args+=("${extra[@]}")
  fi
  run_cmd "$VLLM_BIN" "${args[@]}"
}

start_voxtype() {
  if ! command -v "$VOXTYPE_BIN" >/dev/null 2>&1; then
    echo "voxtype binary not found: $VOXTYPE_BIN" >&2
    echo "Install voxtype and ensure it is in PATH (or set VOXTYPE_BIN)." >&2
    exit 1
  fi

  local language_csv primary_language language_mode
  language_csv="$(printf '%s' "$LANGUAGE_RAW" | tr '[:upper:]' '[:lower:]' | tr -d '[:space:]')"
  if [ -z "$language_csv" ]; then
    language_csv="en,de"
  fi
  primary_language="${language_csv%%,*}"
  language_mode="$primary_language"
  if [[ "$language_csv" == *,* ]]; then
    language_mode="auto"
  fi

  echo "Starting voxtype STT service at http://$HOST:$PORT (languages=$language_csv model=$FALLBACK_MODEL)"

  export VOXTYPE_SERVICE_ENABLED=true
  export VOXTYPE_SERVICE_HOST="$HOST"
  export VOXTYPE_SERVICE_PORT="$PORT"
  export VOXTYPE_SERVICE_ALLOWED_LANGUAGES="$language_csv"
  export VOXTYPE_LANGUAGE="$language_mode"
  export VOXTYPE_MODEL="$FALLBACK_MODEL"
  export VOXTYPE_THREADS="$THREADS"
  export VOXTYPE_HOTKEY_ENABLED=false

  local -a args
  args=(
    --service
    --service-host "$HOST"
    --service-port "$PORT"
    --no-hotkey
    --model "$FALLBACK_MODEL"
    --language "$language_mode"
    --threads "$THREADS"
  )
  if [ -n "$PROMPT" ]; then
    args+=(--initial-prompt "$PROMPT")
  fi
  run_cmd "$VOXTYPE_BIN" "${args[@]}"
}

provider="$(normalize_provider "$PROVIDER_RAW")"
case "$provider" in
  voxtral)
    if ! can_run_voxtral; then
      echo "TABURA_STT_PROVIDER=voxtral requested, but local Voxtral runtime is unavailable." >&2
      echo "Need: vllm plus Python packages 'vllm' and 'mistral_common'." >&2
      exit 1
    fi
    start_voxtral
    ;;
  voxtype)
    start_voxtype
    ;;
  auto)
    if can_run_voxtral; then
      start_voxtral
    fi
    echo "Voxtral runtime unavailable; falling back to voxtype STT." >&2
    start_voxtype
    ;;
esac
