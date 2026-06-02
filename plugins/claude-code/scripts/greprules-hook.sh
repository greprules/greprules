#!/usr/bin/env bash
set -u

MODE="${1:-scan-if-dirty}"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)}"
GREPRULES="${GREPRULES_CLI_PATH:-${PLUGIN_ROOT}/bin/greprules}"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$PWD}"
STATE_DIR="${CLAUDE_PLUGIN_DATA:-${PROJECT_DIR}/.greprules/plugin-data}"
LOG_PATH="${STATE_DIR}/hook.log"
DIRTY_MARKER="${STATE_DIR}/dirty"
LAST_SCAN_PATH="${STATE_DIR}/last-scan"
LAST_SUMMARY_PATH="${STATE_DIR}/last-summary.txt"
MIN_INTERVAL_SECONDS="${GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS:-45}"
MAX_CHANGED_FILES="${GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES:-100}"

mkdir -p "$STATE_DIR" 2>/dev/null || true

log_msg() {
  printf '%s %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')" "$*" >> "$LOG_PATH" 2>/dev/null || true
}

emit_context() {
  local event_name="$1"
  shift
  local message="$1"
  [[ -z "${message// }" ]] && exit 0
  if command -v python3 >/dev/null 2>&1; then
    HOOK_EVENT_NAME="$event_name" HOOK_MESSAGE="$message" python3 - <<'PY'
import json
import os

event_name = os.environ.get("HOOK_EVENT_NAME", "PostToolUse")
message = os.environ.get("HOOK_MESSAGE", "").strip()
if message:
    print(json.dumps({
        "hookSpecificOutput": {
            "hookEventName": event_name,
            "additionalContext": message,
        }
    }))
PY
  else
    printf '{"hookSpecificOutput":{"hookEventName":"%s","additionalContext":"%s"}}\n' \
      "$event_name" \
      "$(printf '%s' "$message" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' ' ')"
  fi
}

json_field() {
  local expression="$1"
  if ! command -v python3 >/dev/null 2>&1; then
    return 1
  fi
  local input
  input="$(cat)"
  JSON_INPUT="$input" python3 - "$expression" <<'PY'
import json
import os
import sys

expression = sys.argv[1]
try:
    data = json.loads(os.environ.get("JSON_INPUT", ""))
except Exception:
    sys.exit(1)

value = data
for part in expression.split("."):
    if isinstance(value, dict) and part in value:
        value = value[part]
    else:
        sys.exit(1)

if isinstance(value, bool):
    print("true" if value else "false")
elif value is None:
    print("")
elif isinstance(value, (list, dict)):
    print(json.dumps(value))
else:
    print(value)
PY
}

summarize_agent_result() {
  local path="$1"
  if ! command -v python3 >/dev/null 2>&1 || [[ ! -f "$path" ]]; then
    printf 'greprules scan finished. Full result: %s\n' "$path"
    return 0
  fi
  python3 - "$path" <<'PY'
import json
import sys

path = sys.argv[1]
with open(path, "r", encoding="utf-8") as handle:
    data = json.load(handle)

status = data.get("status", "unknown")
findings = data.get("findings") or []
warnings = data.get("warnings") or []
errors = data.get("errors") or []

lines = [f"greprules automatic changed-file scan completed with status: {status}."]
if warnings:
    lines.append("Warnings: " + "; ".join(str(item) for item in warnings[:3]))
if errors:
    lines.append("Errors: " + "; ".join(str(item) for item in errors[:3]))

if not findings:
    lines.append("No OpenGrep findings were reported for the current changed-file scan.")
else:
    lines.append(f"OpenGrep findings: {len(findings)}. Review likely true positives before editing further.")
    for finding in findings[:10]:
        start = finding.get("start") or {}
        path_value = finding.get("path") or "<unknown>"
        line = start.get("line") or 0
        rule_id = finding.get("ruleId") or "<unknown-rule>"
        severity = finding.get("severity") or "unknown"
        message = (finding.get("message") or "").replace("\n", " ").strip()
        lines.append(f"- {severity} {rule_id} {path_value}:{line} {message}")
    if len(findings) > 10:
        lines.append(f"- {len(findings) - 10} additional finding(s) omitted from hook context.")

lines.append("Full result: .greprules/out/agent-result.json")
print("\n".join(lines))
PY
}

auto_scan_enabled() {
  case "${GREPRULES_AUTO_SCAN:-true}" in
    0|false|FALSE|no|NO|off|OFF)
      return 1
      ;;
  esac
  return 0
}

ensure_git_repo() {
  cd "$PROJECT_DIR" 2>/dev/null || return 1
  git rev-parse --show-toplevel >/dev/null 2>&1
}

changed_file_count() {
  git status --short --untracked-files=all 2>/dev/null | wc -l | tr -d ' '
}

mark_dirty() {
  if ! auto_scan_enabled; then
    log_msg "auto scan disabled; dirty marker skipped"
    exit 0
  fi
  if ! ensure_git_repo; then
    log_msg "dirty marker skipped outside git repo: $PROJECT_DIR"
    exit 0
  fi
  {
    printf 'project=%s\n' "$PROJECT_DIR"
    printf 'markedAt=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  } > "$DIRTY_MARKER" 2>/dev/null || true
  log_msg "marked dirty: $PROJECT_DIR"
}

doctor_context() {
  if ! auto_scan_enabled; then
    log_msg "auto scan disabled; doctor skipped"
    exit 0
  fi

  if [[ ! -x "$GREPRULES" ]]; then
    emit_context "SessionStart" "greprules is not ready: greprules executable was not found. Build the CLI and put it on PATH, or set GREPRULES_CLI_PATH."
    exit 0
  fi

  if ! ensure_git_repo; then
    log_msg "doctor skipped outside git repo: $PROJECT_DIR"
    exit 0
  fi

  local doctor_output
  if ! doctor_output="$("$GREPRULES" doctor --format json 2>&1)"; then
    log_msg "doctor failed: $doctor_output"
    emit_context "SessionStart" "greprules readiness check failed: ${doctor_output}"
    exit 0
  fi

  local registry_ok lock_exists active_ok recommended
  registry_ok="$(printf '%s' "$doctor_output" | json_field "registry.ok" 2>/dev/null || true)"
  lock_exists="$(printf '%s' "$doctor_output" | json_field "lock.exists" 2>/dev/null || true)"
  active_ok="$(printf '%s' "$doctor_output" | json_field "opengrep.active.ok" 2>/dev/null || true)"
  recommended="$(printf '%s' "$doctor_output" | json_field "recommendedCommands" 2>/dev/null || true)"

  if [[ "$registry_ok" == "true" && "$lock_exists" == "true" && "$active_ok" == "true" ]]; then
    log_msg "doctor ok"
    exit 0
  fi

  emit_context "SessionStart" "greprules needs setup before automatic scans. Registry ready: ${registry_ok:-unknown}; lockfile exists: ${lock_exists:-unknown}; OpenGrep ready: ${active_ok:-unknown}. Recommended commands: ${recommended:-greprules doctor --format json}"
}

scan_if_dirty() {
  if ! auto_scan_enabled; then
    log_msg "auto scan disabled"
    exit 0
  fi

  if [[ ! -f "$DIRTY_MARKER" ]]; then
    log_msg "scan skipped; no dirty marker"
    exit 0
  fi

  if [[ -f "$LAST_SCAN_PATH" ]]; then
    local now last elapsed
    now="$(date +%s)"
    last="$(cat "$LAST_SCAN_PATH" 2>/dev/null || printf '0')"
    elapsed=$((now - last))
    if [[ "$elapsed" -lt "$MIN_INTERVAL_SECONDS" ]]; then
      log_msg "scan skipped; min interval not reached: ${elapsed}s < ${MIN_INTERVAL_SECONDS}s"
      exit 0
    fi
  fi

  if [[ ! -x "$GREPRULES" ]]; then
    emit_context "Stop" "greprules automatic scan skipped: greprules executable was not found. Build the CLI and put it on PATH, or set GREPRULES_CLI_PATH."
    exit 0
  fi

  if ! ensure_git_repo; then
    log_msg "scan skipped outside git repo: $PROJECT_DIR"
    exit 0
  fi

  local count
  count="$(changed_file_count)"
  if [[ "$count" == "0" ]]; then
    rm -f "$DIRTY_MARKER" 2>/dev/null || true
    log_msg "scan skipped; no changed files"
    exit 0
  fi
  if [[ "$count" -gt "$MAX_CHANGED_FILES" ]]; then
    emit_context "Stop" "greprules automatic scan skipped because ${count} changed files exceed the automatic limit (${MAX_CHANGED_FILES}). Run /greprules:scan when ready."
    exit 0
  fi

  local doctor_output
  if ! doctor_output="$("$GREPRULES" doctor --format json 2>&1)"; then
    log_msg "doctor failed: $doctor_output"
    emit_context "Stop" "greprules automatic scan skipped because doctor failed: ${doctor_output}"
    exit 0
  fi

  local registry_ok lock_exists active_ok recommended
  registry_ok="$(printf '%s' "$doctor_output" | json_field "registry.ok" 2>/dev/null || true)"
  lock_exists="$(printf '%s' "$doctor_output" | json_field "lock.exists" 2>/dev/null || true)"
  active_ok="$(printf '%s' "$doctor_output" | json_field "opengrep.active.ok" 2>/dev/null || true)"
  recommended="$(printf '%s' "$doctor_output" | json_field "recommendedCommands" 2>/dev/null || true)"

  if [[ "$lock_exists" != "true" && "$registry_ok" == "true" ]]; then
    local fetch_output
    if ! fetch_output="$("$GREPRULES" fetch 2>&1)"; then
      log_msg "fetch failed: $fetch_output"
      emit_context "Stop" "greprules automatic scan skipped because rule pack fetch failed: ${fetch_output}"
      exit 0
    fi
    if ! doctor_output="$("$GREPRULES" doctor --format json 2>&1)"; then
      emit_context "Stop" "greprules fetched rule packs, but readiness check failed: ${doctor_output}"
      exit 0
    fi
    active_ok="$(printf '%s' "$doctor_output" | json_field "opengrep.active.ok" 2>/dev/null || true)"
    recommended="$(printf '%s' "$doctor_output" | json_field "recommendedCommands" 2>/dev/null || true)"
  fi

  if [[ "$active_ok" != "true" ]]; then
    emit_context "Stop" "greprules automatic scan skipped because OpenGrep is not ready. Recommended commands: ${recommended:-greprules setup-opengrep}"
    exit 0
  fi

  local scan_output
  if ! scan_output="$("$GREPRULES" scan --changed 2>&1)"; then
    log_msg "scan failed: $scan_output"
    emit_context "Stop" "greprules automatic changed-file scan failed: ${scan_output}"
    exit 0
  fi

  local agent_result=".greprules/out/agent-result.json"
  local summary
  summary="$(summarize_agent_result "$agent_result" 2>&1 || true)"
  printf '%s\n' "$summary" > "$LAST_SUMMARY_PATH" 2>/dev/null || true
  date +%s > "$LAST_SCAN_PATH" 2>/dev/null || true
  rm -f "$DIRTY_MARKER" 2>/dev/null || true
  emit_context "Stop" "$summary"
}

case "$MODE" in
  mark-dirty)
    mark_dirty
    ;;
  scan-if-dirty)
    scan_if_dirty
    ;;
  doctor)
    doctor_context
    ;;
  *)
    log_msg "unknown hook mode: $MODE"
    exit 0
    ;;
esac
