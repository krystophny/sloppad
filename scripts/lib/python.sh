#!/usr/bin/env bash

tabura_python_meets_min_version() {
    local candidate="$1"
    local min_major="$2"
    local min_minor="$3"

    [ -x "$candidate" ] || return 1

    "$candidate" - "$min_major" "$min_minor" <<'PY' >/dev/null 2>&1
import sys

min_major = int(sys.argv[1])
min_minor = int(sys.argv[2])
raise SystemExit(0 if sys.version_info >= (min_major, min_minor) else 1)
PY
}

tabura_find_python3() {
    local min_major="${1:-3}"
    local min_minor="${2:-10}"
    local candidate resolved prefix
    local -a candidates=()
    local seen=""

    if [ -n "${TABURA_PYTHON3_BIN:-}" ]; then
        candidates+=("${TABURA_PYTHON3_BIN}")
    fi

    if resolved="$(command -v python3 2>/dev/null)"; then
        candidates+=("$resolved")
    fi

    candidates+=(
        /opt/homebrew/bin/python3
        /usr/local/bin/python3
        /usr/bin/python3
    )

    if command -v brew >/dev/null 2>&1; then
        prefix="$(brew --prefix python 2>/dev/null || true)"
        if [ -n "$prefix" ] && [ -x "$prefix/bin/python3" ]; then
            candidates+=("$prefix/bin/python3")
        fi
    fi

    for candidate in "${candidates[@]}"; do
        [ -n "$candidate" ] || continue
        if [ ! -x "$candidate" ] && resolved="$(command -v "$candidate" 2>/dev/null)"; then
            candidate="$resolved"
        fi
        case ":$seen:" in
            *":$candidate:"*) continue ;;
        esac
        seen="${seen:+${seen}:}${candidate}"
        if tabura_python_meets_min_version "$candidate" "$min_major" "$min_minor"; then
            printf '%s' "$candidate"
            return 0
        fi
    done

    return 1
}
