#!/usr/bin/env bash
set -euo pipefail

ROOT="${TABURA_VLLM_ROOT:-$HOME/.local/share/tabura-vllm}"
VENV="${TABURA_VLLM_VENV:-$ROOT/venv}"
DOWNLOAD_DIR="${TABURA_VLLM_DOWNLOAD_DIR:-$ROOT/models}"
HF_HOME="${HF_HOME:-$ROOT/huggingface}"
HOST="${TABURA_VLLM_HOST:-127.0.0.1}"
PORT="${TABURA_VLLM_PORT:-8426}"
MODEL="${TABURA_VLLM_MODEL:-QuantTrio/Qwen3.5-9B-AWQ}"
SERVED_MODEL_NAME="${TABURA_VLLM_SERVED_MODEL_NAME:-tabura-qwen-9b}"
MAX_MODEL_LEN="${TABURA_VLLM_MAX_MODEL_LEN:-4096}"
GPU_MEMORY_UTILIZATION="${TABURA_VLLM_GPU_MEMORY_UTILIZATION:-0.75}"
PYTHON_VERSION="${TABURA_VLLM_PYTHON:-3.12}"
REASONING_PARSER="${TABURA_VLLM_REASONING_PARSER:-qwen3}"
TOOL_CALL_PARSER="${TABURA_VLLM_TOOL_CALL_PARSER:-qwen3_coder}"
ENABLE_AUTO_TOOL_CHOICE="${TABURA_VLLM_ENABLE_AUTO_TOOL_CHOICE:-1}"
VLLM_BIN="${VENV}/bin/vllm"
PYTHON_BIN="${VENV}/bin/python3"
UV_BIN=""

if command -v uv >/dev/null 2>&1; then
    UV_BIN="$(command -v uv)"
elif [ -x "${HOME}/.local/bin/uv" ]; then
    UV_BIN="${HOME}/.local/bin/uv"
elif [ -x "/usr/bin/uv" ]; then
    UV_BIN="/usr/bin/uv"
elif [ -x "/opt/homebrew/bin/uv" ]; then
    UV_BIN="/opt/homebrew/bin/uv"
fi

if [ -z "${UV_BIN}" ]; then
    echo "uv is required to set up the local vLLM runtime." >&2
    echo "Install uv first, for example: pacman -S uv or brew install uv" >&2
    exit 1
fi

mkdir -p "${ROOT}" "${DOWNLOAD_DIR}" "${HF_HOME}"

if [ ! -x "${VLLM_BIN}" ]; then
    "${UV_BIN}" venv --python "${PYTHON_VERSION}" "${VENV}"
    "${UV_BIN}" pip install --python "${PYTHON_BIN}" vllm
fi

args=(
    serve "${MODEL}"
    --host "${HOST}"
    --port "${PORT}"
    --served-model-name "${SERVED_MODEL_NAME}"
    --max-model-len "${MAX_MODEL_LEN}"
    --gpu-memory-utilization "${GPU_MEMORY_UTILIZATION}"
    --download-dir "${DOWNLOAD_DIR}"
    --reasoning-parser "${REASONING_PARSER}"
    --tool-call-parser "${TOOL_CALL_PARSER}"
)

if [ "${ENABLE_AUTO_TOOL_CHOICE}" = "1" ]; then
    args+=(--enable-auto-tool-choice)
fi

exec env HF_HOME="${HF_HOME}" "${VLLM_BIN}" "${args[@]}"
