#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

cleanup_live_session() {
  local session_token="${SLOPSHELL_TEST_SESSION_TOKEN:-}"
  local cookie_args=()
  local runtime_json session_id
  if [ -n "$session_token" ]; then
    cookie_args=(-H "Cookie: slopshell_session=${session_token}")
  fi
  runtime_json="$(curl -fsS --max-time 5 "${cookie_args[@]}" http://127.0.0.1:8420/api/runtime/workspaces 2>/dev/null || true)"
  if [ -z "$runtime_json" ]; then
    return 0
  fi
  session_id="$(python3 -c 'import json,sys; data=json.load(sys.stdin); workspaces=data.get("workspaces") or []; print(((workspaces[0] or {}).get("chat_session_id") or "").strip())' <<<"$runtime_json" 2>/dev/null || true)"
  if [ -z "$session_id" ]; then
    return 0
  fi
  curl -fsS --max-time 5 "${cookie_args[@]}" \
    -H 'Content-Type: application/json' \
    -X POST "http://127.0.0.1:8420/api/chat/sessions/${session_id}/commands" \
    -d '{"command":"/clear"}' >/dev/null 2>&1 || true
}

trap cleanup_live_session EXIT

# ---------------------------------------------------------------------------
# Pre-flight: all required services must be running
# ---------------------------------------------------------------------------

fail() { printf 'FATAL: %s\n' "$1" >&2; exit 1; }

printf 'Checking services...\n'

curl -fsS --max-time 3 http://127.0.0.1:8420/api/setup >/dev/null \
  || fail 'Slopshell web server not running on :8420'

curl -fsS --max-time 3 -o /dev/null -w '' \
  -X POST http://127.0.0.1:8424/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input":"ok","voice":"en","response_format":"wav"}' \
  || fail 'Piper TTS not running on :8424'

curl -fsS --max-time 3 http://127.0.0.1:8427/healthz >/dev/null \
  || fail 'voxtype STT not running on :8427'

command -v ffmpeg >/dev/null 2>&1 \
  || fail 'ffmpeg not installed'

printf 'All services OK.\n'

# ---------------------------------------------------------------------------
# Seed a local auth session when the live server is password-protected.
# This avoids mutating the admin password just to run local browser E2E.
# ---------------------------------------------------------------------------

if [ -z "${SLOPSHELL_TEST_SESSION_TOKEN:-}" ]; then
  setup_json="$(curl -fsS --max-time 3 http://127.0.0.1:8420/api/setup || true)"
  if printf '%s' "$setup_json" | grep -q '"has_password":true'; then
    resolve_db_path() {
      if [ -n "${SLOPSHELL_E2E_DB_PATH:-}" ] && [ -f "${SLOPSHELL_E2E_DB_PATH}" ]; then
        printf '%s\n' "${SLOPSHELL_E2E_DB_PATH}"
        return 0
      fi
      if [ -f "${HOME}/Library/Application Support/slopshell/web-data/slopshell.db" ]; then
        printf '%s\n' "${HOME}/Library/Application Support/slopshell/web-data/slopshell.db"
        return 0
      fi
      if [ -f "${HOME}/.local/share/slopshell/slopshell.db" ]; then
        printf '%s\n' "${HOME}/.local/share/slopshell/slopshell.db"
        return 0
      fi
      if [ -f "${HOME}/.local/share/slopshell-web/slopshell.db" ]; then
        printf '%s\n' "${HOME}/.local/share/slopshell-web/slopshell.db"
        return 0
      fi
      return 1
    }

    if command -v sqlite3 >/dev/null 2>&1; then
      if DB_PATH="$(resolve_db_path)"; then
        export SLOPSHELL_TEST_SESSION_TOKEN="e2e-$(date +%s)-$$"
        now_epoch="$(date +%s)"
        sqlite3 "$DB_PATH" "INSERT OR REPLACE INTO auth_sessions (token,created_at) VALUES ('${SLOPSHELL_TEST_SESSION_TOKEN}', ${now_epoch});"
        printf 'Seeded auth session for local E2E: %s\n' "$SLOPSHELL_TEST_SESSION_TOKEN"
      else
        printf 'WARN: password-protected server detected but no local slopshell.db found; set SLOPSHELL_TEST_PASSWORD or SLOPSHELL_TEST_SESSION_TOKEN.\n' >&2
      fi
    else
      printf 'WARN: password-protected server detected but sqlite3 is unavailable; set SLOPSHELL_TEST_PASSWORD or SLOPSHELL_TEST_SESSION_TOKEN.\n' >&2
    fi
  fi
fi

# ---------------------------------------------------------------------------
# Generate speech WAV via Piper and pad with silence for VAD offset detection
# ---------------------------------------------------------------------------

SPEECH_WAV="/tmp/slopshell-e2e-speech-raw.wav"
PADDED_WAV="/tmp/slopshell-e2e-speech.wav"

printf 'Generating speech WAV via Piper TTS...\n'
curl -sS -X POST http://127.0.0.1:8424/v1/audio/speech \
  -H 'Content-Type: application/json' \
  -d '{"input":"Hello, this is a test of voice recording.","voice":"en","response_format":"wav"}' \
  -o "$SPEECH_WAV"

# Pad: 5s silence before speech + speech + 3s silence after.
# Chromium's fake audio capture can start consuming the file before the test
# click arms recording, so the leading silence needs to cover page boot,
# websocket connect, and MediaRecorder/VAD startup.
ffmpeg -hide_banner -loglevel error -nostdin -y \
  -f lavfi -t 5 -i anullsrc=r=22050:cl=mono \
  -i "$SPEECH_WAV" \
  -f lavfi -t 3 -i anullsrc=r=22050:cl=mono \
  -filter_complex '[0][1][2]concat=n=3:v=0:a=1[out]' \
  -map '[out]' -ar 22050 -ac 1 -c:a pcm_s16le "$PADDED_WAV"

printf 'Audio ready: %s\n' "$PADDED_WAV"

# ---------------------------------------------------------------------------
# Run Playwright E2E tests
# ---------------------------------------------------------------------------

export E2E_AUDIO_FILE="$PADDED_WAV"
cd "$ROOT_DIR"
npx playwright test --config=playwright.e2e.config.ts "$@"
