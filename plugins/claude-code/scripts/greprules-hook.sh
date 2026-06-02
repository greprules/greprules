#!/usr/bin/env bash
set -u

MODE="${1:-scan-if-dirty}"
PLUGIN_ROOT="${CLAUDE_PLUGIN_ROOT:-$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)}"
GREPRULES="${PLUGIN_ROOT}/bin/greprules"
PROJECT_DIR="${CLAUDE_PROJECT_DIR:-$PWD}"

plugin_state_dir() {
  if [[ -n "${GREPRULES_PLUGIN_STATE_DIR:-}" ]]; then
    printf '%s' "$GREPRULES_PLUGIN_STATE_DIR"
    return
  fi
  printf '%s' "${PROJECT_DIR}/.greprules/plugin-data/claude-code"
}

STATE_DIR="$(plugin_state_dir)"
LOG_PATH="${STATE_DIR}/hook.log"
DIRTY_MARKER="${STATE_DIR}/dirty"
DIRTY_FILES_PATH="${STATE_DIR}/dirty-files"
SCAN_TARGETS_PATH="${STATE_DIR}/scan-targets.txt"
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
    if event_name in {"PostToolUse", "PostToolBatch", "UserPromptSubmit"}:
        payload = {
            "hookSpecificOutput": {
                "hookEventName": event_name,
                "additionalContext": message,
            }
        }
    elif event_name == "StopBlock":
        payload = {
            "decision": "block",
            "reason": message,
        }
    else:
        payload = {
            "continue": True,
            "systemMessage": message,
        }
    print(json.dumps(payload))
PY
  else
    local escaped
    escaped="$(printf '%s' "$message" | sed 's/\\/\\\\/g; s/"/\\"/g' | tr '\n' ' ')"
    case "$event_name" in
      PostToolUse|PostToolBatch|UserPromptSubmit)
        printf '{"hookSpecificOutput":{"hookEventName":"%s","additionalContext":"%s"}}\n' "$event_name" "$escaped"
        ;;
      StopBlock)
        printf '{"decision":"block","reason":"%s"}\n' "$escaped"
        ;;
      *)
        printf '{"continue":true,"systemMessage":"%s"}\n' "$escaped"
        ;;
    esac
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

repo = data.get("repo") or {}
scan = data.get("scan") or {}
targets = scan.get("targets") or []
scan_label = "changed-file" if repo.get("changedMode") else "edited-file"

lines = [f"greprules automatic {scan_label} scan completed with status: {status}."]
if warnings:
    lines.append("Warnings: " + "; ".join(str(item) for item in warnings[:3]))
if errors:
    lines.append("Errors: " + "; ".join(str(item) for item in errors[:3]))

if not findings:
    lines.append("No OpenGrep findings were reported for the current automatic scan.")
else:
    lines.append(
        f"OpenGrep reported {len(findings)} finding(s) on files you just edited. "
        "Review .greprules/out/agent-result.json: classify each as true/false positive, "
        "explain reasoning, and fix or justify before finishing."
    )
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

if targets:
    lines.append("Scanned targets: " + ", ".join(str(item) for item in targets[:10]))
    if len(targets) > 10:
        lines.append(f"- {len(targets) - 10} additional target(s) omitted from hook context.")

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

ensure_project_dir() {
  cd "$PROJECT_DIR" 2>/dev/null || return 1
}

capture_edited_files() {
  if ! command -v python3 >/dev/null 2>&1; then
    return 1
  fi
  local hook_input
  hook_input="$(cat)"
  PROJECT_DIR_FOR_HOOK="$PROJECT_DIR" HOOK_INPUT_JSON="$hook_input" python3 - <<'PY'
import json
import os
import sys

raw = os.environ.get("HOOK_INPUT_JSON", "")
if not raw.strip():
    sys.exit(0)

try:
    payload = json.loads(raw)
except Exception:
    sys.exit(0)

tool_input = payload.get("tool_input") or payload.get("toolInput") or {}
tool_response = payload.get("tool_response") or payload.get("toolResponse") or {}
project_dir = os.environ.get("PROJECT_DIR_FOR_HOOK") or os.getcwd()
root = os.path.realpath(project_dir)
path_keys = {"file_path", "filePath", "notebook_path", "notebookPath"}
tool_name = str(payload.get("tool_name") or payload.get("toolName") or "")
if tool_name in {"Edit", "MultiEdit", "Write", "NotebookEdit"}:
    path_keys.add("path")

seen = set()

def add_path(value):
    if not isinstance(value, str) or not value.strip():
        return
    candidate = value.strip()
    if os.path.isabs(candidate):
        absolute = os.path.realpath(candidate)
    else:
        absolute = os.path.realpath(os.path.join(root, candidate))
    try:
        rel = os.path.relpath(absolute, root)
    except ValueError:
        return
    if rel == ".." or rel.startswith(".." + os.sep) or os.path.isabs(rel):
        return
    if not os.path.exists(absolute):
        return
    if rel not in seen:
        seen.add(rel)

def walk(value):
    if isinstance(value, dict):
        for key, item in value.items():
            if key in path_keys:
                add_path(item)
            else:
                walk(item)
    elif isinstance(value, list):
        for item in value:
            walk(item)

walk(tool_input)
walk(tool_response)
for rel in sorted(seen):
    print(rel)
PY
}

dedupe_dirty_files() {
  if [[ ! -f "$DIRTY_FILES_PATH" ]]; then
    return 0
  fi
  local tmp
  tmp="${DIRTY_FILES_PATH}.tmp"
  sort -u "$DIRTY_FILES_PATH" > "$tmp" 2>/dev/null && mv "$tmp" "$DIRTY_FILES_PATH" 2>/dev/null || rm -f "$tmp" 2>/dev/null || true
}

prepare_scan_targets() {
  : > "$SCAN_TARGETS_PATH" 2>/dev/null || return 1
  if [[ ! -f "$DIRTY_FILES_PATH" ]]; then
    return 0
  fi
  while IFS= read -r target; do
    [[ -z "$target" ]] && continue
    case "$target" in
      /*|..|../*|*/../*)
        continue
        ;;
    esac
    if [[ -e "${PROJECT_DIR}/${target}" ]]; then
      printf '%s\n' "$target"
    fi
  done < "$DIRTY_FILES_PATH" | sort -u > "$SCAN_TARGETS_PATH" 2>/dev/null || true
}

scan_target_count() {
  if [[ ! -f "$SCAN_TARGETS_PATH" ]]; then
    printf '0'
    return
  fi
  awk 'NF {count++} END {print count + 0}' "$SCAN_TARGETS_PATH" 2>/dev/null || printf '0'
}

mark_dirty() {
  if ! auto_scan_enabled; then
    log_msg "auto scan disabled; dirty marker skipped"
    exit 0
  fi
  if ! ensure_project_dir; then
    log_msg "dirty marker skipped because project dir is not available: $PROJECT_DIR"
    exit 0
  fi
  local edited_files
  edited_files="$(capture_edited_files 2>/dev/null || true)"
  if [[ -n "$edited_files" ]]; then
    printf '%s\n' "$edited_files" >> "$DIRTY_FILES_PATH" 2>/dev/null || true
    dedupe_dirty_files
  fi
  {
    printf 'project=%s\n' "$PROJECT_DIR"
    printf 'markedAt=%s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
  } > "$DIRTY_MARKER" 2>/dev/null || true
  log_msg "marked dirty: $PROJECT_DIR files=$(printf '%s' "$edited_files" | tr '\n' ',' | sed 's/,$//')"
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

  if ! ensure_project_dir; then
    log_msg "doctor skipped because project dir is not available: $PROJECT_DIR"
    exit 0
  fi

  local doctor_output
  if ! doctor_output="$("$GREPRULES" doctor --format json 2>&1)"; then
    log_msg "doctor failed: $doctor_output"
    emit_context "SessionStart" "greprules readiness check failed: ${doctor_output}"
    exit 0
  fi

  local registry_ok lock_exists active_ok recommended system_ok system_path system_version active_mode setup_guidance
  registry_ok="$(printf '%s' "$doctor_output" | json_field "registry.ok" 2>/dev/null || true)"
  lock_exists="$(printf '%s' "$doctor_output" | json_field "lock.exists" 2>/dev/null || true)"
  active_ok="$(printf '%s' "$doctor_output" | json_field "opengrep.active.ok" 2>/dev/null || true)"
  recommended="$(printf '%s' "$doctor_output" | json_field "recommendedCommands" 2>/dev/null || true)"
  system_ok="$(printf '%s' "$doctor_output" | json_field "opengrep.system.ok" 2>/dev/null || true)"
  system_path="$(printf '%s' "$doctor_output" | json_field "opengrep.system.runtime.path" 2>/dev/null || true)"
  system_version="$(printf '%s' "$doctor_output" | json_field "opengrep.system.runtime.version" 2>/dev/null || true)"
  active_mode="$(printf '%s' "$doctor_output" | json_field "config.config.opengrep.mode" 2>/dev/null || true)"

  if [[ "$registry_ok" == "true" && "$lock_exists" == "true" && "$active_ok" == "true" ]]; then
    log_msg "doctor ok"
    exit 0
  fi

  setup_guidance=""
  if [[ "$active_ok" != "true" ]]; then
    setup_guidance=" Run /greprules:configure or /greprules:doctor to choose an OpenGrep runtime."
    if [[ "$system_ok" == "true" ]]; then
      setup_guidance="${setup_guidance} System OpenGrep was detected at ${system_path:-opengrep}"
      if [[ -n "$system_version" ]]; then
        setup_guidance="${setup_guidance} (version ${system_version})"
      fi
      setup_guidance="${setup_guidance}."
    else
      setup_guidance="${setup_guidance} No system opengrep was found on PATH."
    fi
  fi

  emit_context "SessionStart" "greprules needs setup before automatic scans. Registry ready: ${registry_ok:-unknown}; lockfile exists: ${lock_exists:-unknown}; OpenGrep ready: ${active_ok:-unknown}; configured OpenGrep mode: ${active_mode:-unknown}. Recommended commands: ${recommended:-greprules doctor --format json}.${setup_guidance}"
}

scan_if_dirty() {
  local hook_input stop_active
  hook_input="$(cat 2>/dev/null || true)"
  stop_active="$(printf '%s' "$hook_input" | json_field "stop_hook_active" 2>/dev/null || true)"
  if [[ "$stop_active" == "true" ]]; then
    log_msg "stop_hook_active=true; skipping automatic scan block"
    exit 0
  fi

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

  if ! ensure_project_dir; then
    log_msg "scan skipped because project dir is not available: $PROJECT_DIR"
    exit 0
  fi

  local count
  prepare_scan_targets
  count="$(scan_target_count)"
  if [[ "$count" == "0" ]]; then
    rm -f "$DIRTY_MARKER" "$DIRTY_FILES_PATH" "$SCAN_TARGETS_PATH" 2>/dev/null || true
    log_msg "scan skipped; no edited files captured"
    exit 0
  fi
  if [[ "$count" -gt "$MAX_CHANGED_FILES" ]]; then
    emit_context "Stop" "greprules automatic scan skipped because ${count} edited files exceed the automatic limit (${MAX_CHANGED_FILES}). Run /greprules:scan when ready."
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
  if ! scan_output="$("$GREPRULES" scan --targets-from "$SCAN_TARGETS_PATH" 2>&1)"; then
    log_msg "scan failed: $scan_output"
    emit_context "Stop" "greprules automatic edited-file scan failed: ${scan_output}"
    exit 0
  fi

  local agent_result=".greprules/out/agent-result.json"
  local summary
  summary="$(summarize_agent_result "$agent_result" 2>&1 || true)"
  printf '%s\n' "$summary" > "$LAST_SUMMARY_PATH" 2>/dev/null || true
  date +%s > "$LAST_SCAN_PATH" 2>/dev/null || true
  rm -f "$DIRTY_MARKER" "$DIRTY_FILES_PATH" "$SCAN_TARGETS_PATH" 2>/dev/null || true
  emit_context "StopBlock" "$summary"
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
