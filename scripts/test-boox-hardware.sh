#!/usr/bin/env bash
set -euo pipefail

# Boox hardware validation orchestrator.
# Runs the parts that can be automated against an attached Onyx Boox device
# and prints the manual checklist that owns the rest of the closure evidence.
# Closure rules live in docs/boox-validation.md.

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

artifact_dir="$repo_root/artifacts/boox-validation"
mkdir -p "$artifact_dir"

log() {
  printf '\n[%s] %s\n' "boox-hardware" "$1" >&2
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

resolve_android_sdk() {
  local candidate
  candidate="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-}}"
  if [[ -z "$candidate" ]]; then
    for guess in "$HOME/android-sdk" "$HOME/Android/Sdk"; do
      if [[ -d "$guess" ]]; then
        candidate="$guess"
        break
      fi
    done
  fi
  if [[ -z "$candidate" ]]; then
    echo "ANDROID_HOME not set and no SDK found at \$HOME/android-sdk or \$HOME/Android/Sdk" >&2
    exit 1
  fi
  export ANDROID_HOME="$candidate"
  export ANDROID_SDK_ROOT="$candidate"
  export PATH="$candidate/platform-tools:$candidate/emulator:$PATH"
}

require_cmd adb
require_cmd gradle
resolve_android_sdk

log "checking attached devices"
mapfile -t devices < <(adb devices | awk 'NR>1 && $2=="device" {print $1}')
if [[ "${#devices[@]}" -eq 0 ]]; then
  echo "no adb devices in 'device' state; connect a Boox device with USB debugging enabled" >&2
  exit 1
fi
if [[ "${#devices[@]}" -gt 1 ]]; then
  echo "more than one adb device attached; set ANDROID_SERIAL to choose one" >&2
  printf '  %s\n' "${devices[@]}" >&2
  exit 1
fi
serial="${ANDROID_SERIAL:-${devices[0]}}"
export ANDROID_SERIAL="$serial"

manufacturer="$(adb -s "$serial" shell getprop ro.product.manufacturer | tr -d '\r' | tr '[:upper:]' '[:lower:]')"
model="$(adb -s "$serial" shell getprop ro.product.model | tr -d '\r')"
android_sdk="$(adb -s "$serial" shell getprop ro.build.version.sdk | tr -d '\r')"
android_release="$(adb -s "$serial" shell getprop ro.build.version.release | tr -d '\r')"

log "device: serial=$serial manufacturer=$manufacturer model=$model android=$android_release sdk=$android_sdk"

if [[ "$manufacturer" != "onyx" ]]; then
  echo "device manufacturer '$manufacturer' is not 'onyx'; refusing to run Boox validation" >&2
  exit 1
fi

device_summary="$artifact_dir/device.txt"
{
  echo "serial: $serial"
  echo "manufacturer: $manufacturer"
  echo "model: $model"
  echo "android_release: $android_release"
  echo "android_sdk: $android_sdk"
  echo "captured_at: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
} >"$device_summary"
log "device summary recorded at $device_summary"

log "installing debug APK"
gradle -p platforms/android :app:installDebug --console=plain --no-daemon

package_name="com.slopshell.android"
log "launching $package_name/.MainActivity"
adb -s "$serial" shell am start -W -n "$package_name/.MainActivity" >/dev/null

log "waiting 3s for first frame"
sleep 3

screenshot="$artifact_dir/launch.png"
adb -s "$serial" exec-out screencap -p >"$screenshot"
log "launch screenshot saved to $screenshot"

cat <<'CHECKLIST'

Manual checklist (record pass/fail for each item; missing item = fail):

  1. Boox status panel shows manufacturer, sdk=true, TouchHelper=true, EpdController=true.
  2. Drawing a stroke increments the strokes counter on the panel.
  3. The same stroke arrives at the server as an ink_stroke payload.
  4. Canvas content uses the e-ink CSS (white bg, black text, no animations).
  5. After first canvas render, refresh counter shows M/N applied with M >= 1.
  6. A second canvas update increments both M and N; no persistent ghost.
  7. WebView contrast keeps gradient/low-contrast canvas content readable.
  8. Black-screen dialogue mode still enters/exits normally on this device.

Attach the screenshot under artifacts/boox-validation/ and the device summary
to the PR or issue that claims Boox readiness. Closure rules are in
docs/boox-validation.md.

CHECKLIST
