#!/usr/bin/env bash
set -u

MODE="${1:-scan-if-dirty}"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)}"

exec python3 "${PLUGIN_ROOT}/scripts/greprules-hook.py" "$MODE"
