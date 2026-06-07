#!/usr/bin/env python3
"""Claude Code hook adapter for greprules.

Claude-specific hook JSON parsing, session dirty state, and hook output
formatting live here. The Go CLI only exposes provider-neutral explicit scan
primitives.
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
PLUGIN_ROOT = Path(os.environ.get("CLAUDE_PLUGIN_ROOT", Path(__file__).resolve().parents[1])).resolve()
SAFE_SESSION_RE = re.compile(r"[^A-Za-z0-9._-]+")
IGNORED_DIRS = {
    ".cache",
    ".git",
    ".greprules",
    ".hg",
    ".next",
    ".nuxt",
    ".svn",
    ".turbo",
    ".venv",
    "build",
    "coverage",
    "dist",
    "node_modules",
    "target",
    "vendor",
    "venv",
}
SCAN_FILENAMES = {
    ".dockerignore",
    ".npmrc",
    "brewfile",
    "cargo.lock",
    "cargo.toml",
    "composer.json",
    "composer.lock",
    "containerfile",
    "dockerfile",
    "gemfile",
    "gemfile.lock",
    "go.mod",
    "go.sum",
    "jenkinsfile",
    "makefile",
    "package-lock.json",
    "package.json",
    "pipfile",
    "pipfile.lock",
    "pnpm-lock.yaml",
    "podfile",
    "poetry.lock",
    "pom.xml",
    "pyproject.toml",
    "rakefile",
    "requirements.txt",
    "settings.gradle",
    "settings.gradle.kts",
    "tsconfig.json",
    "yarn.lock",
}
SCAN_EXTENSIONS = {
    ".bash",
    ".c",
    ".cc",
    ".cfg",
    ".clj",
    ".cljs",
    ".conf",
    ".cpp",
    ".cs",
    ".cxx",
    ".dart",
    ".ex",
    ".exs",
    ".fs",
    ".go",
    ".gql",
    ".gradle",
    ".graphql",
    ".groovy",
    ".h",
    ".hcl",
    ".hh",
    ".hpp",
    ".hrl",
    ".hs",
    ".htm",
    ".html",
    ".hxx",
    ".ini",
    ".java",
    ".js",
    ".json",
    ".jsx",
    ".kt",
    ".kts",
    ".lua",
    ".m",
    ".mjs",
    ".ml",
    ".mli",
    ".mm",
    ".nim",
    ".php",
    ".phtml",
    ".pl",
    ".pm",
    ".properties",
    ".proto",
    ".ps1",
    ".py",
    ".pyw",
    ".r",
    ".rb",
    ".rego",
    ".rs",
    ".scala",
    ".sh",
    ".sol",
    ".sql",
    ".svelte",
    ".swift",
    ".tf",
    ".tfvars",
    ".toml",
    ".ts",
    ".tsx",
    ".vue",
    ".xml",
    ".yaml",
    ".yml",
    ".zig",
    ".zsh",
}


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
    path = Path(override).expanduser() if override else root / ".greprules" / "plugin-data" / "claude-code"
    path.mkdir(parents=True, exist_ok=True)
    return path


def safe_session_id(payload: Dict[str, Any]) -> str:
    for key in ("session_id", "sessionId"):
        value = payload.get(key)
        if isinstance(value, str) and value.strip():
            safe = SAFE_SESSION_RE.sub("-", value.strip())[:120].strip(".-")
            return safe or ""
    return ""


def session_state_dir(root: Path, payload: Dict[str, Any]) -> Optional[Path]:
    session = safe_session_id(payload)
    if not session:
        return None
    path = state_dir(root) / "sessions" / session
    path.mkdir(parents=True, exist_ok=True)
    return path


def dirty_files_path(session_dir: Path) -> Path:
    return session_dir / "dirty-files"


def scan_targets_path(session_dir: Path) -> Path:
    return session_dir / "scan-targets.txt"


def last_scan_path(session_dir: Path) -> Path:
    return session_dir / "last-scan"


def last_summary_path(session_dir: Path) -> Path:
    return session_dir / "last-summary.txt"


def output_dir(session_dir: Path) -> Path:
    return session_dir / "out"


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


def is_scan_candidate(rel: str) -> bool:
    parts = rel.replace("\\", "/").split("/")
    if any(part in IGNORED_DIRS for part in parts):
        return False
    name = parts[-1].lower() if parts else ""
    if name in SCAN_FILENAMES or name.startswith("dockerfile."):
        return True
    return Path(name).suffix in SCAN_EXTENSIONS


def normalize_existing_path(root: Path, base_dir: Path, raw: str) -> str:
    candidate = raw.strip()
    if not candidate:
        return ""
    path = Path(candidate).expanduser()
    if not path.is_absolute():
        path = base_dir / path
    try:
        absolute = path.resolve(strict=True)
        resolved_root = root.resolve(strict=True)
    except Exception:
        return ""
    try:
        rel = absolute.relative_to(resolved_root)
    except ValueError:
        return ""
    if absolute.is_dir():
        return ""
    rel_text = rel.as_posix()
    return str(absolute) if rel_text and is_scan_candidate(rel_text) else ""


def normalize_candidates(root: Path, base_dir: Path, paths: Iterable[str]) -> List[str]:
    seen = set()
    files: List[str] = []
    for raw in paths:
        if not isinstance(raw, str):
            continue
        rel = normalize_existing_path(root, base_dir, raw)
        if rel and rel not in seen:
            seen.add(rel)
            files.append(rel)
    return sorted(files)


def read_lines(path: Path) -> List[str]:
    try:
        return [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
    except FileNotFoundError:
        return []


def write_lines(path: Path, lines: Iterable[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    values = sorted({line.strip() for line in lines if line.strip()})
    tmp = Path(str(path) + ".tmp")
    tmp.write_text("".join(line + "\n" for line in values), encoding="utf-8")
    tmp.replace(path)


def append_dirty_files(session_dir: Path, files: Iterable[str]) -> List[str]:
    merged = set(read_lines(dirty_files_path(session_dir)))
    merged.update(line for line in files if line)
    write_lines(dirty_files_path(session_dir), merged)
    return sorted(merged)


def prepare_scan_targets(root: Path, session_dir: Path) -> List[str]:
    targets = normalize_candidates(root, root, read_lines(dirty_files_path(session_dir)))
    write_lines(scan_targets_path(session_dir), targets)
    return targets


def clear_dirty(session_dir: Path) -> None:
    for path in (dirty_files_path(session_dir), scan_targets_path(session_dir)):
        try:
            path.unlink()
        except FileNotFoundError:
            pass


def record_scan_attempt(session_dir: Path) -> None:
    try:
        last_scan_path(session_dir).write_text(str(int(time.time())) + "\n", encoding="utf-8")
    except Exception:
        pass


def mark_dirty(root: Path, payload: Dict[str, Any]) -> int:
    agent = agent_config(root)
    if not agent_bool(agent, "trackEditedFiles", True, "GREPRULES_TRACK_EDITED_FILES"):
        log_msg(root, "edited file tracking disabled")
        return 0
    if not root.is_dir():
        log_msg(root, f"dirty marker skipped because project dir is not available: {root}")
        return 0
    session_dir = session_state_dir(root, payload)
    if session_dir is None:
        log_msg(root, "dirty marker skipped; Claude session_id is not available")
        return 0
    paths = capture_edited_paths(payload)
    if not paths:
        log_msg(root, "dirty marker skipped; no path candidates captured")
        return 0

    files = normalize_candidates(root, root, paths)
    if files:
        append_dirty_files(session_dir, files)
        log_msg(root, "marked dirty: " + str(root) + " files=" + ",".join(str(item) for item in files))
    else:
        log_msg(root, "dirty marker skipped; no scan candidate files captured")
    return 0


def last_scan_recent(session_dir: Path, agent: Dict[str, Any]) -> bool:
    min_interval = agent_int(agent, "autoScanMinIntervalSeconds", DEFAULT_MIN_INTERVAL_SECONDS, "GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS")
    if min_interval <= 0:
        return False
    try:
        last = int(last_scan_path(session_dir).read_text(encoding="utf-8").strip())
    except Exception:
        return False
    return int(time.time()) - last < min_interval


def too_many_targets_message(count: int, limit: int, automatic: bool) -> str:
    prefix = "automatic " if automatic else ""
    return (
        f"greprules {prefix}edited-file scan skipped because {count} edited files exceed the automatic limit ({limit}). "
        "Run /greprules:scan-edited or /greprules:scan-working-tree when ready."
    )


def scan_session(root: Path, session_dir: Path, payload: Dict[str, Any], *, automatic: bool) -> Optional[Dict[str, Any]]:
    agent = agent_config(root)
    if not root.is_dir():
        log_msg(root, f"scan skipped because project dir is not available: {root}")
        return {"status": "skipped", "message": "greprules edited-file scan skipped because project dir is not available: " + str(root)}
    targets = prepare_scan_targets(root, session_dir)
    if not targets:
        clear_dirty(session_dir)
        log_msg(root, "scan skipped; no edited files captured")
        return {"status": "skipped"}
    limit = agent_int(agent, "autoScanMaxChangedFiles", DEFAULT_MAX_CHANGED_FILES, "GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES") if automatic else 0
    if limit > 0 and len(targets) > limit:
        message = too_many_targets_message(len(targets), limit, automatic)
        return {"status": "skipped", "message": message, "targets": targets, "targetsPath": str(scan_targets_path(session_dir))}
    proc = run_cli(
        [
            "agent-scan",
            "scan",
            "--root",
            str(root),
            "--label",
            "edited-file",
            "--targets-from",
            str(scan_targets_path(session_dir)),
            "--output-dir",
            str(output_dir(session_dir)),
            "--format",
            "json",
            *(["--automatic"] if automatic else []),
        ],
        root,
        timeout=900,
    )
    if proc.returncode != 0:
        return {"status": "failed", "message": "greprules edited-file scan failed: " + (proc.stderr or proc.stdout or "unknown scan failure").strip()}
    try:
        outcome = json.loads(proc.stdout)
    except json.JSONDecodeError:
        return {"status": "failed", "message": "greprules edited-file scan failed: could not parse agent-scan output"}
    if not isinstance(outcome, dict):
        return None
    message = string_from_any(outcome.get("message"))
    if outcome.get("status") == "scanned":
        try:
            last_summary_path(session_dir).write_text(message + "\n", encoding="utf-8")
        except Exception:
            pass
        record_scan_attempt(session_dir)
        clear_dirty(session_dir)
    return outcome


def handle_scan_outcome(root: Path, outcome: Optional[Dict[str, Any]], payload: Dict[str, Any], *, automatic: bool) -> None:
    if not isinstance(outcome, dict):
        return
    message = string_from_any(outcome.get("message"))
    if not message:
        return
    status = outcome.get("status")
    if status == "scanned":
        if automatic:
            emit_block(message)
        else:
            print(message)
    elif status == "needs_pack_selection":
        log_msg(root, "agent pack selection required: " + message)
        if automatic:
            emit_block(message)
        else:
            print(message)
    else:
        log_msg(root, "scan skipped or failed: " + message)
        if automatic:
            emit_system_message(message)
        else:
            print(message)


def dirty_session_dirs(root: Path) -> List[Path]:
    sessions_root = state_dir(root) / "sessions"
    if not sessions_root.is_dir():
        return []
    sessions: List[Path] = []
    for child in sessions_root.iterdir():
        if child.is_dir() and read_lines(dirty_files_path(child)):
            sessions.append(child)
    return sorted(sessions)


def scan_edited(root: Path, payload: Dict[str, Any]) -> int:
    session_dir = session_state_dir(root, payload)
    if session_dir is None:
        sessions = dirty_session_dirs(root)
        if len(sessions) == 0:
            print("no Claude-edited files are tracked; edit files or run /greprules:scan-working-tree")
            return 0
        if len(sessions) > 1:
            print("multiple Claude sessions have dirty files; start a session-scoped scan or run /greprules:scan-target for explicit files")
            return 0
        session_dir = sessions[0]
    outcome = scan_session(root, session_dir, payload, automatic=False)
    if isinstance(outcome, dict) and outcome.get("status") == "skipped" and not string_from_any(outcome.get("message")):
        print("no Claude-edited files are tracked; edit files or run /greprules:scan-working-tree")
        return 0
    handle_scan_outcome(root, outcome, payload, automatic=False)
    return 0


def scan_if_dirty(root: Path, payload: Dict[str, Any]) -> int:
    agent = agent_config(root)
    if hook_bool(payload, "stop_hook_active", "stopHookActive"):
        log_msg(root, "stop_hook_active=true; skipping automatic scan block")
        return 0
    if not agent_bool(agent, "autoScan", False, "GREPRULES_AUTO_SCAN"):
        log_msg(root, "auto scan disabled; preserving edited file state")
        return 0
    session_dir = session_state_dir(root, payload)
    if session_dir is None:
        log_msg(root, "scan skipped; Claude session_id is not available")
        emit_system_message("greprules automatic edited-file scan skipped because Claude session_id was not available.")
        return 0
    if not read_lines(dirty_files_path(session_dir)):
        log_msg(root, "scan skipped; no dirty files for session")
        return 0
    if last_scan_recent(session_dir, agent):
        log_msg(root, "scan skipped; min interval not reached")
        return 0
    outcome = scan_session(root, session_dir, payload, automatic=True)
    handle_scan_outcome(root, outcome, payload, automatic=True)
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
    if mode == "scan-edited":
        return scan_edited(root, payload)
    if mode == "doctor":
        return doctor_context(root)
    sys.stderr.write(f"unknown greprules Claude hook mode: {mode}\n")
    return 2


if __name__ == "__main__":
    raise SystemExit(main())
