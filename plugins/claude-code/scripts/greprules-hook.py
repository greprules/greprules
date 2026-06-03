#!/usr/bin/env python3
"""Claude Code hook adapter for greprules.

Claude-specific hook JSON parsing and hook output formatting live here. The
Go CLI only exposes provider-neutral agent-state and agent-scan primitives.
"""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional


DEFAULT_MIN_INTERVAL_SECONDS = 45
DEFAULT_MAX_CHANGED_FILES = 100
PLUGIN_ROOT = Path(os.environ.get("CLAUDE_PLUGIN_ROOT", Path(__file__).resolve().parents[1])).resolve()


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


def project_dir() -> Path:
    raw = os.environ.get("CLAUDE_PROJECT_DIR") or os.getcwd()
    try:
        return Path(raw).expanduser().resolve()
    except Exception:
        return Path.cwd()


def state_dir(root: Path) -> Path:
    override = os.environ.get("GREPRULES_PLUGIN_STATE_DIR", "").strip()
    path = Path(override).expanduser() if override else root / ".greprules" / "plugin-data" / "agent"
    path.mkdir(parents=True, exist_ok=True)
    return path


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
    print(json.dumps(payload, separators=(",", ":")))


def emit_system_message(message: str) -> None:
    message = message.strip()
    if message:
        write_hook_output({"continue": True, "systemMessage": message})


def emit_block(message: str) -> None:
    message = message.strip()
    if message:
        write_hook_output({"decision": "block", "reason": message})


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
    if tool_name in {"Edit", "MultiEdit", "Write", "NotebookEdit"}:
        path_keys.add("path")

    seen = set()
    paths: List[str] = []

    def walk(value: Any) -> None:
        if isinstance(value, dict):
            for key, item in value.items():
                if key in path_keys:
                    path = string_from_any(item)
                    if path and path not in seen:
                        seen.add(path)
                        paths.append(path)
                    continue
                walk(item)
        elif isinstance(value, list):
            for item in value:
                walk(item)

    walk(first_present(payload, "tool_input", "toolInput"))
    walk(first_present(payload, "tool_response", "toolResponse"))
    return sorted(paths)


def clear_dirty(root: Path) -> None:
    proc = run_cli(["agent-state", "clear", "--root", str(root), "--state-dir", str(state_dir(root))], root)
    if proc.returncode != 0:
        log_msg(root, "agent-state clear failed: " + (proc.stderr or proc.stdout).strip())


def record_scan_attempt(root: Path) -> None:
    proc = run_cli(["agent-state", "record-scan", "--root", str(root), "--state-dir", str(state_dir(root))], root)
    if proc.returncode != 0:
        log_msg(root, "agent-state record-scan failed: " + (proc.stderr or proc.stdout).strip())


def mark_dirty(root: Path, payload: Dict[str, Any]) -> int:
    if not bool_env("GREPRULES_TRACK_EDITED_FILES", True):
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


def last_scan_recent(root: Path) -> bool:
    min_interval = int_env("GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS", DEFAULT_MIN_INTERVAL_SECONDS)
    if min_interval <= 0:
        return False
    try:
        last = int((state_dir(root) / "last-scan").read_text(encoding="utf-8").strip())
    except Exception:
        return False
    return int(time.time()) - last < min_interval


def scan_if_dirty(root: Path, payload: Dict[str, Any]) -> int:
    if hook_bool(payload, "stop_hook_active", "stopHookActive"):
        log_msg(root, "stop_hook_active=true; skipping automatic scan block")
        clear_dirty(root)
        return 0
    if not bool_env("GREPRULES_AUTO_SCAN", True):
        log_msg(root, "auto scan disabled; preserving edited file state")
        return 0
    if not (state_dir(root) / "dirty").exists():
        log_msg(root, "scan skipped; no dirty marker")
        return 0
    if last_scan_recent(root):
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
        "Run /greprules:scan-edited or /greprules:scan-working-tree when ready."
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
            str(int_env("GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES", DEFAULT_MAX_CHANGED_FILES)),
            "--too-many-message",
            too_many_message,
        ],
        root,
        timeout=900,
    )
    if proc.returncode != 0:
        emit_system_message("greprules automatic edited-file scan failed: " + (proc.stderr or proc.stdout or "unknown scan failure").strip())
        return 0
    try:
        outcome = json.loads(proc.stdout)
    except json.JSONDecodeError:
        emit_system_message("greprules automatic edited-file scan failed: could not parse agent-scan output")
        return 0
    if not isinstance(outcome, dict):
        return 0
    message = string_from_any(outcome.get("message"))
    if not message:
        return 0
    if outcome.get("status") == "scanned":
        try:
            (state_dir(root) / "last-summary.txt").write_text(message + "\n", encoding="utf-8")
        except Exception:
            pass
        emit_block(message)
    else:
        log_msg(root, "scan skipped or failed: " + message)
        emit_system_message(message)
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
    if not bool_env("GREPRULES_AUTO_SCAN", True):
        log_msg(root, "auto scan disabled; doctor skipped")
        return 0
    if not root.is_dir():
        log_msg(root, f"doctor skipped because project dir is not available: {root}")
        return 0
    proc = run_cli(["doctor", "--root", str(root), "--format", "json"], root, timeout=180)
    if proc.returncode != 0:
        log_msg(root, "doctor failed: " + (proc.stderr or proc.stdout).strip())
        emit_system_message("greprules readiness check failed: " + (proc.stderr or proc.stdout).strip())
        return 0
    try:
        report = json.loads(proc.stdout)
    except json.JSONDecodeError as exc:
        emit_system_message("greprules readiness check failed: could not parse doctor JSON: " + str(exc))
        return 0

    registry_ok = bool(nested(report, "registry", "ok"))
    lock_exists = bool(nested(report, "lock", "exists"))
    active_ok = bool(nested(report, "opengrep", "active", "ok"))
    if registry_ok and active_ok:
        log_msg(root, "doctor ok")
        return 0

    setup_guidance = ""
    if not active_ok:
        setup_guidance = " Run /greprules:configure or /greprules:doctor to choose an OpenGrep runtime."
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
        f"configured OpenGrep mode: {mode}. Recommended commands: {recommended}.{setup_guidance}{rule_pack_guidance}"
    )
    emit_system_message(message)
    return 0


def main() -> int:
    mode = sys.argv[1] if len(sys.argv) > 1 else "scan-if-dirty"
    root = project_dir()
    payload = read_hook_input()
    if mode == "mark-dirty":
        return mark_dirty(root, payload)
    if mode == "scan-if-dirty":
        return scan_if_dirty(root, payload)
    if mode == "doctor":
        return doctor_context(root)
    sys.stderr.write(f"unknown greprules Claude hook mode: {mode}\n")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
