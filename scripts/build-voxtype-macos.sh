#!/usr/bin/env bash
set -euo pipefail

VOXTYPE_REPO="${VOXTYPE_REPO:-https://github.com/peteonrails/voxtype.git}"
VOXTYPE_BRANCH="${VOXTYPE_BRANCH:-feature/single-daemon-openai-stt-api}"
INSTALL_DIR="${VOXTYPE_INSTALL_DIR:-${HOME}/.local/bin}"
ASSUME_YES="${TABURA_ASSUME_YES:-0}"

log()  { printf '[build-voxtype] %s\n' "$*"; }
fail() { printf '[build-voxtype] ERROR: %s\n' "$*" >&2; exit 1; }

confirm_default_yes() {
    local prompt="$1"
    if [ "$ASSUME_YES" = "1" ]; then return 0; fi
    if [ ! -t 0 ]; then return 0; fi
    local response
    read -r -p "$prompt [Y/n] " response
    case "$response" in
        "" | [Yy] | [Yy][Ee][Ss]) return 0 ;;
        *) return 1 ;;
    esac
}

print_help() {
    cat <<USAGE
Usage: scripts/build-voxtype-macos.sh [options]

Builds voxtype from source on macOS with patches for platform compatibility.
Voxtype is a Rust application that provides local OpenAI-compatible STT.
Upstream is Linux-only; this script patches evdev/inotify deps to be
Linux-conditional so the service mode compiles on macOS.

Options:
  --yes       Non-interactive mode
  -h, --help  Show this help

Environment:
  VOXTYPE_REPO         Git clone URL (default: peteonrails/voxtype)
  VOXTYPE_BRANCH       Branch to build (default: feature/single-daemon-openai-stt-api)
  VOXTYPE_INSTALL_DIR  Binary install directory (default: ~/.local/bin)
  TABURA_ASSUME_YES=1  Same as --yes
USAGE
}

while [ "$#" -gt 0 ]; do
    case "$1" in
        --yes) ASSUME_YES=1; shift ;;
        -h|--help) print_help; exit 0 ;;
        *) fail "unknown argument: $1" ;;
    esac
done

# --- Prerequisites ---

[ "$(uname -s)" = "Darwin" ] || fail "this script is for macOS only"

command -v cargo >/dev/null 2>&1 || fail "cargo not found; install Rust: https://rustup.rs"
command -v cmake >/dev/null 2>&1 || fail "cmake not found; install: brew install cmake"
command -v git >/dev/null 2>&1 || fail "git not found"

# --- Clone ---

BUILD_DIR="$(mktemp -d -t voxtype-build-XXXXXX)"
trap 'rm -rf "$BUILD_DIR"' EXIT

log "Cloning $VOXTYPE_REPO (branch: $VOXTYPE_BRANCH)"
git clone --depth 1 --branch "$VOXTYPE_BRANCH" "$VOXTYPE_REPO" "$BUILD_DIR/voxtype"

cd "$BUILD_DIR/voxtype"

# --- Apply macOS compatibility patches ---

log "Patching Cargo.toml: making evdev/inotify Linux-conditional"

# Remove evdev and inotify from unconditional [dependencies].
# Use awk for reliable multi-line Cargo.toml editing.
awk '
    /^evdev[[:space:]]*=/ { next }
    /^inotify[[:space:]]*=.*Watch .dev.input/ { next }
    { print }
' Cargo.toml > Cargo.toml.tmp
mv Cargo.toml.tmp Cargo.toml

# Append Linux-only dependency section before [features].
awk '
    /^\[features\]/ {
        print "[target.'\''cfg(target_os = \"linux\")'\''.dependencies]"
        print "evdev = \"0.12\""
        print "inotify = \"0.10\""
        print ""
    }
    { print }
' Cargo.toml > Cargo.toml.tmp
mv Cargo.toml.tmp Cargo.toml

log "Patching src/error.rs: gating From<evdev::Error> impl"

# Guard the From<evdev::Error> impl with cfg(target_os = "linux").
awk '
    /^impl From<evdev::Error>/ {
        print "#[cfg(target_os = \"linux\")]"
    }
    { print }
' src/error.rs > src/error.rs.tmp
mv src/error.rs.tmp src/error.rs

log "Patching src/hotkey/mod.rs: adding non-Linux stub"

# Replace the hotkey module with a version that has platform guards.
cat > src/hotkey/mod.rs <<'HOTKEY_MOD'
//! Hotkey detection module (Linux-only, stubbed on other platforms)

#[cfg(target_os = "linux")]
pub mod evdev_listener;

use crate::config::HotkeyConfig;
use crate::error::HotkeyError;
use tokio::sync::mpsc;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum HotkeyEvent {
    Pressed {
        model_override: Option<String>,
    },
    Released,
    Cancel,
}

#[async_trait::async_trait]
pub trait HotkeyListener: Send + Sync {
    async fn start(&mut self) -> Result<mpsc::Receiver<HotkeyEvent>, HotkeyError>;
    async fn stop(&mut self) -> Result<(), HotkeyError>;
}

#[cfg(target_os = "linux")]
pub fn create_listener(
    config: &HotkeyConfig,
    secondary_model: Option<String>,
) -> Result<Box<dyn HotkeyListener>, HotkeyError> {
    let mut listener = evdev_listener::EvdevListener::new(config)?;
    listener.set_secondary_model(secondary_model);
    Ok(Box::new(listener))
}

#[cfg(not(target_os = "linux"))]
pub fn create_listener(
    _config: &HotkeyConfig,
    _secondary_model: Option<String>,
) -> Result<Box<dyn HotkeyListener>, HotkeyError> {
    Err(HotkeyError::NoKeyboard)
}
HOTKEY_MOD

# --- Build ---

ARCH="$(uname -m)"
FEATURES=""
if [ "$ARCH" = "arm64" ]; then
    FEATURES="gpu-metal"
    log "Building with Metal GPU support (Apple Silicon)"
else
    log "Building without GPU acceleration (Intel Mac)"
fi

log "Building voxtype (this may take several minutes on first build)"
if [ -n "$FEATURES" ]; then
    cargo build --release --features "$FEATURES"
else
    cargo build --release
fi

# --- Install ---

mkdir -p "$INSTALL_DIR"
cp target/release/voxtype "$INSTALL_DIR/voxtype"
chmod +x "$INSTALL_DIR/voxtype"

log "Installed: $INSTALL_DIR/voxtype"

if ! printf ':%s:' "$PATH" | grep -Fq ":${INSTALL_DIR}:"; then
    log "WARNING: $INSTALL_DIR is not in PATH; add it to your shell profile"
fi

# --- Download model ---

if confirm_default_yes "Download voxtype model large-v3-turbo (~1.5 GB)?"; then
    "$INSTALL_DIR/voxtype" setup --download --model large-v3-turbo --no-post-install || {
        log "WARNING: model download failed; you can retry later with:"
        log "  voxtype setup --download --model large-v3-turbo --no-post-install"
    }
fi

log "Build complete. Verify with: voxtype --version"
