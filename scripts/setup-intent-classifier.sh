#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INTENT_DIR="${REPO_ROOT}/services/intent-classifier"
VENV_DIR="${INTENT_DIR}/.venv"
MODEL_DIR="${INTENT_DIR}/model"

if [ ! -f "${INTENT_DIR}/main.py" ]; then
  echo "main.py not found in ${INTENT_DIR}" >&2
  exit 1
fi

if [ ! -f "${INTENT_DIR}/intents.json" ]; then
  echo "intents.json not found in ${INTENT_DIR}" >&2
  exit 1
fi

# --- Create venv and install ALL deps ---

if [ ! -x "${VENV_DIR}/bin/python" ]; then
  echo "Creating venv at ${VENV_DIR}"
  python3 -m venv "$VENV_DIR"
fi

echo "Installing runtime dependencies"
"${VENV_DIR}/bin/pip" install --upgrade pip
"${VENV_DIR}/bin/pip" install -r "${INTENT_DIR}/requirements.txt"

# --- Train ONNX model if missing ---

if [ ! -f "${MODEL_DIR}/model.onnx" ] || [ ! -f "${MODEL_DIR}/labels.json" ] || [ ! -d "${MODEL_DIR}/tokenizer" ]; then
  echo "Training intent classification model"
  echo "Installing training dependencies"
  "${VENV_DIR}/bin/pip" install torch datasets scikit-learn 'optimum[onnxruntime]' 'accelerate>=0.26.0'
  "${VENV_DIR}/bin/python" "${INTENT_DIR}/train.py" --output "${MODEL_DIR}"
  echo "Model trained and exported to ${MODEL_DIR}"
fi

# --- Verify the classifier starts ---

echo "Verifying classifier loads"
"${VENV_DIR}/bin/python" -c "
import sys
sys.path.insert(0, '${INTENT_DIR}')
from main import classifier
result = classifier.classify('switch to codex')
assert result[0] == 'switch_model', f'expected switch_model, got {result[0]}'
assert result[1] > 0.9, f'expected confidence > 0.9, got {result[1]}'
print(f'OK: intent={result[0]}, confidence={result[1]:.2f}')
"

echo "Intent classifier ready"
echo "Start: ${VENV_DIR}/bin/uvicorn main:app --app-dir ${INTENT_DIR} --host 127.0.0.1 --port 8425"
