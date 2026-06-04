#!/usr/bin/env python3
"""Codex hook adapter for greprules.

Codex-specific hook JSON parsing and hook output formatting live here. The
Go CLI only exposes provider-neutral agent-state and agent-scan primitives.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional


DEFAULT_MIN_INTERVAL_SECONDS = 45
DEFAULT_MAX_CHANGED_FILES = 100
DEFAULT_LAST_MESSAGE_LIMIT = 6000
PLUGIN_ROOT = Path(os.environ.get("PLUGIN_ROOT") or os.environ.get("CLAUDE_PLUGIN_ROOT", Path(__file__).resolve().parents[1])).resolve()
WROTE_OUTPUT = False
CODEX_HOOK_STATES = {
    "session_start": "SessionStart",
    "post_tool_use": "PostToolUse",
    "stop": "Stop",
}
SCAN_COUNTS_RE = re.compile(r"findings=(\d+), warnings=(\d+), errors=(\d+)")


def bool_env(name: str, default: bool = True) -> bool:
    value = os.environ.get(name, "").strip().lower()
    if value == "":
        return default
    return value not in {"0", "false", "no", "off"}


def int_env(name: str, default: int) -> int:
    try:
        parsed = int(os.environ.get(name, "").strip())
    except ValueError:
        return default
    return parsed if parsed >= 0 else default


def project_dir(payload: Dict[str, Any]) -> Path:
    raw = string_from_any(payload.get("cwd")) or os.environ.get("CODEX_PROJECT_DIR") or os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
    try:
        return Path(raw).expanduser().resolve()
    except Exception:
        return Path.cwd()


def state_dir(root: Path) -> Path:
    override = os.environ.get("GREPRULES_PLUGIN_STATE_DIR", "").strip()
    path = Path(override).expanduser() if override else root / ".greprules" / "plugin-data" / "agent"
    path.mkdir(parents=True, exist_ok=True)
    return path


def codex_config_path() -> Path:
    raw_home = os.environ.get("CODEX_HOME", "").strip()
    home = Path(raw_home).expanduser() if raw_home else Path.home() / ".codex"
    return home / "config.toml"


def codex_hook_warning() -> str:
    path = codex_config_path()
    if not path.exists():
        return ""
    try:
        config = path.read_text(encoding="utf-8")
    except Exception:
        return ""
    plugin_section = config_section(config, '[plugins."greprules@greprules"]')
    if "enabled = true" not in plugin_section:
        return (
            " Codex greprules plugin is not enabled in ~/.codex/config.toml, so automatic scans will not run. "
            "Open /plugins, install or enable greprules, then start a new Codex session."
        )
    missing: List[str] = []
    for key, label in CODEX_HOOK_STATES.items():
        header = f'[hooks.state."greprules@greprules:hooks/hooks.json:{key}:0:0"]'
        section = config_section(config, header)
        if 'trusted_hash = "sha256:' not in section:
            missing.append(label)
    if not missing:
        return ""
    return (
        " Codex greprules hooks are installed but not fully trusted; automatic scans may not run. "
        "Missing trusted hooks: "
        + ", ".join(missing)
        + ". Open /hooks, trust the greprules hook entries, then start a new Codex session."
    )


def config_section(config: str, header: str) -> str:
    lines = config.splitlines()
    capture = False
    out: List[str] = []
    for line in lines:
        stripped = line.strip()
        if stripped == header:
            capture = True
            out.append(stripped)
            continue
        if capture and stripped.startswith("[") and stripped.endswith("]"):
            break
        if capture:
            out.append(stripped)
    return "\n".join(out)


def greprules_bin() -> str:
    return str(PLUGIN_ROOT / "bin" / ("greprules.exe" if os.name == "nt" else "greprules"))


def run_cli(args: Iterable[str], root: Path, timeout: int = 900) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [greprules_bin(), *args],
        cwd=str(root),
        text=True,
        capture_output=True,
        timeout=timeout,
        check=False,
    )


def agent_config(root: Path) -> Dict[str, Any]:
    proc = run_cli(["config", "inspect", "--root", str(root), "--format", "json"], root, timeout=60)
    if proc.returncode != 0:
        log_msg(root, "config inspect failed: " + (proc.stderr or proc.stdout).strip())
        return {}
    try:
        resolution = json.loads(proc.stdout)
    except json.JSONDecodeError:
        log_msg(root, "config inspect returned non-JSON: " + proc.stdout.strip())
        return {}
    config = resolution.get("config") if isinstance(resolution, dict) else {}
    agent = config.get("agent") if isinstance(config, dict) else {}
    return agent if isinstance(agent, dict) else {}


def agent_bool(agent: Dict[str, Any], key: str, default: bool, *env_names: str) -> bool:
    value = agent.get(key, default)
    parsed = value if isinstance(value, bool) else default
    for env_name in env_names:
        raw = os.environ.get(env_name, "").strip().lower()
        if raw:
            return raw not in {"0", "false", "no", "off"}
    return parsed


def agent_int(agent: Dict[str, Any], key: str, default: int, *env_names: str) -> int:
    value = agent.get(key, default)
    parsed = value if isinstance(value, int) and value >= 0 else default
    for env_name in env_names:
        raw = os.environ.get(env_name, "").strip()
        if not raw:
            continue
        try:
            override = int(raw)
        except ValueError:
            continue
        if override >= 0:
            return override
    return parsed


def read_hook_input() -> Dict[str, Any]:
    raw = sys.stdin.read()
    if not raw.strip():
        return {}
    try:
        payload = json.loads(raw)
    except json.JSONDecodeError:
        return {}
    return payload if isinstance(payload, dict) else {}


def write_hook_output(payload: Dict[str, Any]) -> None:
    global WROTE_OUTPUT
    WROTE_OUTPUT = True
    print(json.dumps(payload, separators=(",", ":")))


def hook_event(payload: Dict[str, Any]) -> str:
    return string_from_any(first_present(payload, "hook_event_name", "hookEventName"))


def emit_system_message(message: str, payload: Optional[Dict[str, Any]] = None) -> None:
    message = message.strip()
    if not message:
        return
    event = hook_event(payload or {})
    if event in {"SessionStart", "PostToolUse", "UserPromptSubmit", "SubagentStart"}:
        write_hook_output({
            "hookSpecificOutput": {
                "hookEventName": event,
                "additionalContext": message,
            }
        })
        return
    write_hook_output({"continue": True, "systemMessage": message})


def emit_block(message: str) -> None:
    message = message.strip()
    if message:
        write_hook_output({"decision": "block", "reason": message})


def emit_greprules_review_block(scan_message: str, payload: Dict[str, Any]) -> None:
    scan_message = scan_message.strip()
    if not scan_message:
        return
    previous = string_from_any(first_present(payload, "last_assistant_message", "lastAssistantMessage")).strip()
    if previous:
        limit = int_env("GREPRULES_LAST_ASSISTANT_MESSAGE_LIMIT", DEFAULT_LAST_MESSAGE_LIMIT)
        truncated = len(previous) > limit > 0
        if limit > 0:
            previous = previous[:limit]
        previous_block = (
            "\n\nPrevious assistant response to preserve as the main answer:\n"
            "<previous_response>\n"
            + previous
            + ("\n[truncated]" if truncated else "")
            + "\n</previous_response>"
        )
    else:
        previous_block = ""
    reason = (
        "greprules finished an automatic edited-file scan that needs review.\n\n"
        "Do not replace the user's main development result with greprules-only output. "
        "Your final response must keep the original development-work summary as the primary answer, "
        "then append a short `greprules` section as secondary context. "
        "Read `.greprules/out/agent-result.json`, triage findings as true positive, false positive, or needs investigation, "
        "and mention only actionable security or scan-quality issues. "
        "If a likely true positive requires code changes, propose the fix or apply it only when that matches the user's request.\n\n"
        "greprules scan summary:\n"
        + scan_message
        + previous_block
    )
    emit_block(reason)


def scan_has_actionable_output(message: str) -> bool:
    match = SCAN_COUNTS_RE.search(message)
    if not match:
        return True
    findings, warnings, errors = (int(value) for value in match.groups())
    return findings > 0 or warnings > 0 or errors > 0


def log_msg(root: Path, message: str) -> None:
    try:
        path = state_dir(root) / "hook.log"
        with path.open("a", encoding="utf-8") as handle:
            handle.write(time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()) + " " + message + "\n")
    except Exception:
        return


def hook_bool(payload: Dict[str, Any], *keys: str) -> bool:
    for key in keys:
        value = payload.get(key)
        if isinstance(value, bool):
            return value
        if isinstance(value, str):
            return value == "true"
    return False


def string_from_any(value: Any) -> str:
    return value if isinstance(value, str) else ""


def first_present(payload: Dict[str, Any], *keys: str) -> Any:
    for key in keys:
        if key in payload:
            return payload[key]
    return None


def capture_edited_paths(payload: Dict[str, Any]) -> List[str]:
    path_keys = {"file_path", "filePath", "notebook_path", "notebookPath"}
    tool_name = string_from_any(first_present(payload, "tool_name", "toolName"))
    is_patch_tool = tool_name == "apply_patch" or tool_name.endswith(".apply_patch")
    if is_patch_tool or tool_name in {"Edit", "MultiEdit", "Write", "NotebookEdit"}:
        path_keys.add("path")

    seen = set()
    paths: List[str] = []

    def add_path(path: str) -> None:
        path = path.strip()
        if path and path not in seen:
            seen.add(path)
            paths.append(path)

    def walk(value: Any) -> None:
        if isinstance(value, dict):
            for key, item in value.items():
                if key in path_keys:
                    path = string_from_any(item)
                    add_path(path)
                    continue
                walk(item)
        elif isinstance(value, list):
            for item in value:
                walk(item)

    tool_input = first_present(payload, "tool_input", "toolInput")
    if is_patch_tool and isinstance(tool_input, dict):
        extract_patch_paths(string_from_any(first_present(tool_input, "command", "cmd", "patch")), add_path)
    elif is_patch_tool and isinstance(tool_input, str):
        extract_patch_paths(tool_input, add_path)
    walk(tool_input)
    walk(first_present(payload, "tool_response", "toolResponse"))
    return sorted(paths)


def extract_patch_paths(command: str, add_path: Any) -> None:
    if not command:
        return
    prefixes = (
        "*** Add File: ",
        "*** Update File: ",
        "*** Delete File: ",
        "*** Move to: ",
    )
    for raw_line in command.splitlines():
        line = raw_line.strip()
        for prefix in prefixes:
            if line.startswith(prefix):
                add_path(line[len(prefix):])
                break


def clear_dirty(root: Path) -> None:
    proc = run_cli(["agent-state", "clear", "--root", str(root), "--state-dir", str(state_dir(root))], root)
    if proc.returncode != 0:
        log_msg(root, "agent-state clear failed: " + (proc.stderr or proc.stdout).strip())


def record_scan_attempt(root: Path) -> None:
    proc = run_cli(["agent-state", "record-scan", "--root", str(root), "--state-dir", str(state_dir(root))], root)
    if proc.returncode != 0:
        log_msg(root, "agent-state record-scan failed: " + (proc.stderr or proc.stdout).strip())


def mark_dirty(root: Path, payload: Dict[str, Any]) -> int:
    agent = agent_config(root)
    if not agent_bool(agent, "trackEditedFiles", True, "GREPRULES_TRACK_EDITED_FILES"):
        log_msg(root, "edited file tracking disabled")
        clear_dirty(root)
        return 0
    if not root.is_dir():
        log_msg(root, f"dirty marker skipped because project dir is not available: {root}")
        return 0
    paths = capture_edited_paths(payload)
    if not paths:
        log_msg(root, "dirty marker skipped; no path candidates captured")
        return 0

    command = ["agent-state", "mark-dirty", "--root", str(root), "--state-dir", str(state_dir(root)), "--cwd", str(root)]
    for path in paths:
        command.extend(["--path", path])
    proc = run_cli(command, root)
    if proc.returncode != 0:
        sys.stderr.write((proc.stderr or proc.stdout or "greprules agent-state mark-dirty failed").strip() + "\n")
        return proc.returncode
    try:
        result = json.loads(proc.stdout)
    except json.JSONDecodeError:
        log_msg(root, "agent-state mark-dirty returned non-JSON: " + proc.stdout.strip())
        return 0
    files = result.get("files") if isinstance(result, dict) else []
    if files:
        log_msg(root, "marked dirty: " + str(root) + " files=" + ",".join(str(item) for item in files))
    else:
        log_msg(root, "dirty marker skipped; no scan candidate files captured")
    return 0


def last_scan_recent(root: Path, agent: Dict[str, Any]) -> bool:
    min_interval = agent_int(agent, "autoScanMinIntervalSeconds", DEFAULT_MIN_INTERVAL_SECONDS, "GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS")
    if min_interval <= 0:
        return False
    try:
        last = int((state_dir(root) / "last-scan").read_text(encoding="utf-8").strip())
    except Exception:
        return False
    return int(time.time()) - last < min_interval


def scan_if_dirty(root: Path, payload: Dict[str, Any]) -> int:
    agent = agent_config(root)
    if hook_bool(payload, "stop_hook_active", "stopHookActive"):
        log_msg(root, "stop_hook_active=true; skipping automatic scan block")
        clear_dirty(root)
        return 0
    if not agent_bool(agent, "autoScan", False, "GREPRULES_AUTO_SCAN"):
        log_msg(root, "auto scan disabled; preserving edited file state")
        return 0
    if not (state_dir(root) / "dirty").exists():
        log_msg(root, "scan skipped; no dirty marker")
        return 0
    if last_scan_recent(root, agent):
        log_msg(root, "scan skipped; min interval not reached")
        clear_dirty(root)
        return 0
    if not root.is_dir():
        log_msg(root, f"scan skipped because project dir is not available: {root}")
        record_scan_attempt(root)
        clear_dirty(root)
        return 0

    too_many_message = (
        "greprules automatic scan skipped because {count} edited files exceed the automatic limit ({limit}). "
        "Run the $greprules-scan-edited or $greprules-scan-working-tree skill when ready."
    )
    proc = run_cli(
        [
            "agent-scan",
            "edited",
            "--root",
            str(root),
            "--state-dir",
            str(state_dir(root)),
            "--label",
            "edited-file",
            "--automatic",
            "--format",
            "json",
            "--max-targets",
            str(agent_int(agent, "autoScanMaxChangedFiles", DEFAULT_MAX_CHANGED_FILES, "GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES")),
            "--too-many-message",
            too_many_message,
        ],
        root,
        timeout=900,
    )
    if proc.returncode != 0:
        emit_system_message("greprules automatic edited-file scan failed: " + (proc.stderr or proc.stdout or "unknown scan failure").strip(), payload)
        return 0
    try:
        outcome = json.loads(proc.stdout)
    except json.JSONDecodeError:
        emit_system_message("greprules automatic edited-file scan failed: could not parse agent-scan output", payload)
        return 0
    if not isinstance(outcome, dict):
        return 0
    message = string_from_any(outcome.get("message"))
    if not message:
        return 0
    status = outcome.get("status")
    if status == "scanned":
        try:
            (state_dir(root) / "last-summary.txt").write_text(message + "\n", encoding="utf-8")
        except Exception:
            pass
        if scan_has_actionable_output(message):
            emit_greprules_review_block(message, payload)
        else:
            log_msg(root, "scan completed without actionable findings")
    elif status == "needs_pack_selection":
        log_msg(root, "agent pack selection required: " + message)
        emit_greprules_review_block(message, payload)
    else:
        log_msg(root, "scan skipped or failed: " + message)
        emit_system_message(message, payload)
    return 0


def nested(payload: Dict[str, Any], *keys: str) -> Any:
    value: Any = payload
    for key in keys:
        if not isinstance(value, dict):
            return None
        value = value.get(key)
    return value


def fallback_string(value: Any, fallback: str) -> str:
    return value if isinstance(value, str) and value else fallback


def doctor_context(root: Path) -> int:
    agent = agent_config(root)
    if not agent_bool(agent, "autoScan", False, "GREPRULES_AUTO_SCAN"):
        log_msg(root, "auto scan disabled; doctor skipped")
        return 0
    if not root.is_dir():
        log_msg(root, f"doctor skipped because project dir is not available: {root}")
        return 0
    proc = run_cli(["doctor", "--root", str(root), "--format", "json"], root, timeout=180)
    if proc.returncode != 0:
        log_msg(root, "doctor failed: " + (proc.stderr or proc.stdout).strip())
        emit_system_message("greprules readiness check failed: " + (proc.stderr or proc.stdout).strip(), {"hook_event_name": "SessionStart"})
        return 0
    try:
        report = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        emit_system_message("greprules readiness check failed: could not parse doctor JSON: " + str(exc), {"hook_event_name": "SessionStart"})
        return 0

    registry_ok = bool(nested(report, "registry", "ok"))
    lock_exists = bool(nested(report, "lock", "exists"))
    active_ok = bool(nested(report, "opengrep", "active", "ok"))
    hook_warning = codex_hook_warning()
    if registry_ok and active_ok and not hook_warning:
        log_msg(root, "doctor ok")
        return 0
    if registry_ok and active_ok and hook_warning:
        emit_system_message("greprules automatic scan hooks need attention." + hook_warning, {"hook_event_name": "SessionStart"})
        return 0

    setup_guidance = ""
    if not active_ok:
        setup_guidance = " Run the $greprules-configure or $greprules-doctor skill to choose an OpenGrep runtime."
        system = nested(report, "opengrep", "system") or {}
        runtime = system.get("runtime") if isinstance(system, dict) else None
        if isinstance(system, dict) and system.get("ok") and isinstance(runtime, dict):
            setup_guidance += " System OpenGrep was detected at " + fallback_string(runtime.get("path"), "opengrep")
            if runtime.get("version"):
                setup_guidance += " (version " + str(runtime.get("version")) + ")"
            setup_guidance += "."
        else:
            setup_guidance += " No system opengrep was found on PATH."

    recommended_commands = report.get("recommendedCommands")
    recommended = ", ".join(str(item) for item in recommended_commands) if isinstance(recommended_commands, list) and recommended_commands else "greprules doctor --format json"

    rule_pack_guidance = ""
    if not lock_exists:
        if registry_ok:
            rule_pack_guidance = " Rule packs are not fetched yet; the first scan can fetch them automatically."
        else:
            rule_pack_guidance = " Rule packs are not fetched yet, and registry access is needed before the first scan can fetch them."

    mode = fallback_string(nested(report, "config", "config", "opengrep", "mode"), "unknown")
    message = (
        f"greprules needs attention before scans. Registry ready: {str(registry_ok).lower()}; "
        f"rule packs fetched: {str(lock_exists).lower()}; OpenGrep ready: {str(active_ok).lower()}; "
        f"configured OpenGrep mode: {mode}. Recommended commands: {recommended}.{setup_guidance}{rule_pack_guidance}{hook_warning}"
    )
    emit_system_message(message, {"hook_event_name": "SessionStart"})
    return 0


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "scan-if-dirty"
    payload = read_hook_input()
    root = project_dir(payload)
    if mode == "mark-dirty":
        return mark_dirty(root, payload)
    if mode == "scan-if-dirty":
        code = scan_if_dirty(root, payload)
        if not WROTE_OUTPUT and hook_event(payload) == "Stop":
            write_hook_output({"continue": True})
        return code
    if mode == "doctor":
        return doctor_context(root)
    sys.stderr.write(f"unknown greprules Codex hook mode: {mode}\n")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
