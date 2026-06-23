"""Hermes plugin for greprules.

The plugin keeps Hermes-specific orchestration in Python and delegates rule
fetching, OpenGrep runtime handling, scanning, and result generation to the
greprules Go CLI.
"""

from __future__ import annotations

import json
import logging
import os
import re
import shlex
import shutil
import subprocess
import threading
import time
import uuid
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Set, Tuple

logger = logging.getLogger(__name__)

_PLUGIN_DIR = Path(__file__).resolve().parent
_DEFAULT_MIN_INTERVAL_SECONDS = 45
_DEFAULT_MAX_CHANGED_FILES = 100
_DEFAULT_AGENT_SETTINGS: Dict[str, Any] = {
    "autoScan": False,
    "trackEditedFiles": True,
    "autoScanMinIntervalSeconds": _DEFAULT_MIN_INTERVAL_SECONDS,
    "autoScanMaxChangedFiles": _DEFAULT_MAX_CHANGED_FILES,
}
_RECENT_ROOTS: Dict[str, Set[str]] = {}
_LOCK = threading.Lock()
_SAFE_SESSION_RE = re.compile(r"[^A-Za-z0-9._-]+")
_IGNORED_DIRS = {
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
_SCAN_FILENAMES = {
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
_SCAN_EXTENSIONS = {
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


def _bool_env(name: str, default: bool = True) -> bool:
    value = os.environ.get(name, "").strip().lower()
    if value == "":
        return default
    return value not in {"0", "false", "no", "off"}


def _int_env(name: str, default: int) -> int:
    try:
        parsed = int(os.environ.get(name, "").strip())
    except ValueError:
        return default
    return parsed if parsed >= 0 else default


def _greprules_cmd() -> str:
    explicit = os.environ.get("GREPRULES_CLI_PATH", "").strip()
    if explicit and os.path.isfile(explicit) and os.access(explicit, os.X_OK):
        return explicit
    bundled = _PLUGIN_DIR / "bin" / ("greprules.exe" if os.name == "nt" else "greprules")
    if bundled.exists() and os.access(bundled, os.X_OK):
        return str(bundled)
    found = shutil.which("greprules")
    return found or "greprules"


def _run(args: Sequence[str], cwd: Path, timeout: int = 180) -> Tuple[int, str, str]:
    command = [_greprules_cmd(), *args]
    try:
        proc = subprocess.run(
            command,
            cwd=str(cwd),
            text=True,
            capture_output=True,
            timeout=timeout,
            check=False,
        )
    except FileNotFoundError:
        return 127, "", "greprules CLI wrapper not found; the plugin-bundled bin/greprules is missing and GREPRULES_CLI_PATH/PATH did not resolve an executable"
    except subprocess.TimeoutExpired as exc:
        out = exc.stdout if isinstance(exc.stdout, str) else ""
        err = exc.stderr if isinstance(exc.stderr, str) else ""
        return 124, out, (err + "\ngreprules command timed out").strip()
    return proc.returncode, proc.stdout, proc.stderr


def _agent_settings_path() -> Path:
    override = os.environ.get("GREPRULES_HERMES_SETTINGS_PATH", "").strip()
    if override:
        return Path(override).expanduser()
    raw_home = os.environ.get("HERMES_HOME", "").strip()
    home = Path(raw_home).expanduser() if raw_home else Path.home() / ".hermes"
    return home / "plugins" / "greprules" / "settings.json"


def _agent_settings() -> Dict[str, Any]:
    settings = dict(_DEFAULT_AGENT_SETTINGS)
    path = _agent_settings_path()
    if not path.exists():
        return settings
    try:
        loaded = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError:
        logger.debug("greprules Hermes settings returned non-JSON: %s", path)
        return settings
    except Exception as exc:
        logger.debug("greprules Hermes settings could not be read: %s", exc)
        return settings
    if not isinstance(loaded, dict):
        return settings
    for key in ("autoScan", "trackEditedFiles"):
        value = loaded.get(key)
        if isinstance(value, bool):
            settings[key] = value
    for key in ("autoScanMinIntervalSeconds", "autoScanMaxChangedFiles"):
        value = loaded.get(key)
        if isinstance(value, int) and not isinstance(value, bool) and value >= 0:
            settings[key] = value
    return settings


def _save_agent_setting(key: str, value: Any) -> str:
    settings = _agent_settings()
    settings[key] = value
    path = _agent_settings_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(settings, indent=2, sort_keys=True) + "\n", encoding="utf-8")
    return str(path)


def _agent_bool(agent: Dict[str, Any], key: str, default: bool) -> bool:
    value = agent.get(key, default)
    return value if isinstance(value, bool) else default


def _agent_int(agent: Dict[str, Any], key: str, default: int) -> int:
    value = agent.get(key, default)
    return value if isinstance(value, int) and not isinstance(value, bool) and value >= 0 else default


def _current_cwd(kwargs: Dict[str, Any]) -> Path:
    raw = kwargs.get("cwd") or os.environ.get("GREPRULES_PROJECT_DIR") or os.getcwd()
    try:
        return Path(str(raw)).expanduser().resolve()
    except Exception:
        return Path.cwd()


def _git_root(cwd: Path) -> Path:
    try:
        proc = subprocess.run(
            ["git", "-C", str(cwd), "rev-parse", "--show-toplevel"],
            text=True,
            capture_output=True,
            timeout=10,
            check=False,
        )
    except Exception:
        return cwd
    if proc.returncode == 0 and proc.stdout.strip():
        return Path(proc.stdout.strip()).resolve()
    return cwd


def _state_dir(root: Path) -> Path:
    override = os.environ.get("GREPRULES_HERMES_STATE_DIR", "").strip()
    if override:
        path = Path(override).expanduser()
    else:
        path = root / ".greprules" / "plugin-data" / "hermes"
    path.mkdir(parents=True, exist_ok=True)
    return path


def _safe_session_key(value: str) -> str:
    safe = _SAFE_SESSION_RE.sub("-", value.strip())[:120].strip(".-")
    return safe or ""


def _tracker_key(session_id: str, task_id: str = "") -> str:
    return _safe_session_key(task_id or session_id or "")


def _session_state_dir(root: Path, session_key: str) -> Optional[Path]:
    if not session_key:
        return None
    path = _state_dir(root) / "sessions" / session_key
    path.mkdir(parents=True, exist_ok=True)
    return path


def _dirty_files_path(session_dir: Path) -> Path:
    return session_dir / "dirty-files"


def _scan_targets_path(session_dir: Path) -> Path:
    return session_dir / "scan-targets.txt"


def _last_scan_path(session_dir: Path) -> Path:
    return session_dir / "last-scan"


def _last_summary_path(session_dir: Path) -> Path:
    return session_dir / "last-summary.txt"


def _output_dir(session_dir: Path, label: str) -> Path:
    return session_dir / "runs" / _run_id(label)


def _run_id(label: str) -> str:
    safe_label = _SAFE_SESSION_RE.sub("-", label.strip())[:40].strip(".-")
    parts = [time.strftime("%Y%m%dT%H%M%SZ", time.gmtime())]
    if safe_label:
        parts.append(safe_label)
    parts.append(uuid.uuid4().hex[:12])
    return "-".join(parts)


def _last_scan_recent(session_dir: Path, agent: Dict[str, Any]) -> bool:
    min_interval = _agent_int(agent, "autoScanMinIntervalSeconds", _DEFAULT_MIN_INTERVAL_SECONDS)
    if min_interval <= 0:
        return False
    try:
        last = int(_last_scan_path(session_dir).read_text().strip())
    except Exception:
        return False
    return int(time.time()) - last < min_interval


def _remember_root(root: Path, session_id: str, task_id: str = "") -> None:
    key = _tracker_key(session_id, task_id)
    if not key:
        return
    with _LOCK:
        _RECENT_ROOTS.setdefault(key, set()).add(str(root))


def _roots_for_turn(cwd: Path, session_id: str, task_id: str = "") -> List[Path]:
    roots: Set[str] = {str(cwd)}
    key = _tracker_key(session_id, task_id)
    with _LOCK:
        if key:
            roots.update(_RECENT_ROOTS.get(key, set()))
    return [Path(root) for root in sorted(roots)]


def _extract_path_candidates(tool_name: str, args: Any) -> Set[str]:
    if not isinstance(args, dict):
        return set()
    candidates: Set[str] = set()
    normalized_tool = (tool_name or "").lower()
    path_keys = {"path", "file_path", "filePath", "notebook_path", "notebookPath"}
    if normalized_tool not in {"write_file", "patch", "edit", "multi_edit", "notebook_edit"}:
        return set()
    for key in path_keys:
        value = args.get(key)
        if isinstance(value, str) and value:
            candidates.add(value)
    return candidates


def _is_scan_candidate(rel: str) -> bool:
    parts = rel.replace("\\", "/").split("/")
    if any(part in _IGNORED_DIRS for part in parts):
        return False
    name = parts[-1].lower() if parts else ""
    if name in _SCAN_FILENAMES or name.startswith("dockerfile."):
        return True
    return Path(name).suffix in _SCAN_EXTENSIONS


def _normalize_existing_path(root: Path, base_dir: Path, raw: str) -> str:
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
    return str(absolute) if rel_text and _is_scan_candidate(rel_text) else ""


def _normalize_candidates(root: Path, base_dir: Path, paths: Iterable[str]) -> List[str]:
    seen: Set[str] = set()
    files: List[str] = []
    for raw in paths:
        if not isinstance(raw, str):
            continue
        rel = _normalize_existing_path(root, base_dir, raw)
        if rel and rel not in seen:
            seen.add(rel)
            files.append(rel)
    return sorted(files)


def _read_lines(path: Path) -> List[str]:
    try:
        return [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
    except FileNotFoundError:
        return []


def _write_lines(path: Path, lines: Iterable[str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    values = sorted({line.strip() for line in lines if line.strip()})
    tmp = Path(str(path) + ".tmp")
    tmp.write_text("".join(line + "\n" for line in values), encoding="utf-8")
    tmp.replace(path)


def _append_dirty_files(session_dir: Path, files: Iterable[str]) -> List[str]:
    merged = set(_read_lines(_dirty_files_path(session_dir)))
    merged.update(line for line in files if line)
    _write_lines(_dirty_files_path(session_dir), merged)
    return sorted(merged)


def _prepare_scan_targets(root: Path, session_dir: Path) -> List[str]:
    targets = _normalize_candidates(root, root, _read_lines(_dirty_files_path(session_dir)))
    _write_lines(_scan_targets_path(session_dir), targets)
    return targets


def _clear_dirty(session_dir: Path) -> None:
    for path in (_dirty_files_path(session_dir), _scan_targets_path(session_dir)):
        try:
            path.unlink()
        except FileNotFoundError:
            pass


def _record_scan_attempt(session_dir: Path) -> None:
    try:
        _last_scan_path(session_dir).write_text(str(int(time.time())) + "\n", encoding="utf-8")
    except Exception:
        pass


def _mark_dirty_paths(paths: Iterable[str], cwd: Path, session_id: str, task_id: str = "") -> None:
    root = cwd
    path_list = [path for path in paths if path]
    if not path_list:
        return
    key = _tracker_key(session_id, task_id)
    session_dir = _session_state_dir(root, key)
    if session_dir is None:
        logger.debug("greprules dirty tracking skipped; Hermes session_id/task_id is not available")
        return
    files = _normalize_candidates(root, cwd, path_list)
    if files:
        _append_dirty_files(session_dir, files)
        _remember_root(root, session_id, task_id)


def _status_json(root: Path) -> Tuple[Optional[Dict[str, Any]], str]:
    code, out, err = _run(["agent-status", "--root", str(root), "--format", "json"], root)
    if code != 0:
        return None, (err or out or "greprules readiness check failed").strip()
    try:
        return json.loads(out), ""
    except json.JSONDecodeError as exc:
        return None, f"could not parse greprules readiness JSON: {exc}"


def _recommended(report: Dict[str, Any]) -> str:
    commands = report.get("recommendedCommands") or []
    if isinstance(commands, list) and commands:
        visible = []
        for command in commands:
            text = str(command)
            if text.startswith("greprules agent-status"):
                text = "/greprules configure"
            elif text.startswith("greprules setup-opengrep"):
                text = "/greprules configure managed"
            if text not in visible:
                visible.append(text)
        return ", ".join(visible)
    return "/greprules configure"


def _too_many_targets_message(count: int, limit: int, auto: bool) -> str:
    prefix = "automatic " if auto else ""
    return (
        f"greprules {prefix}edited-file scan skipped because {count} edited files exceed the automatic limit ({limit}). "
        "Run /greprules scan when ready."
    )


def _scan_message_with_selection_context(payload: Dict[str, Any]) -> str:
    message = str(payload.get("message") or "").strip()
    if payload.get("status") != "needs_pack_selection":
        return message
    context = payload.get("selectionContext")
    if not isinstance(context, dict):
        return message
    try:
        context_text = json.dumps(context, indent=2, sort_keys=True)
    except Exception:
        return message
    if message:
        return message + "\nselectionContext:\n" + context_text
    return "selectionContext:\n" + context_text


def _scan_dirty(root: Path, session_key: str, *, auto: bool) -> Optional[str]:
    session_dir = _session_state_dir(root, session_key)
    if session_dir is None:
        return None
    agent = _agent_settings()
    if auto and not _agent_bool(agent, "autoScan", False):
        return None
    if auto and _last_scan_recent(session_dir, agent):
        return None
    if not _read_lines(_dirty_files_path(session_dir)):
        return None
    targets = _prepare_scan_targets(root, session_dir)
    if not targets:
        _clear_dirty(session_dir)
        return None
    limit = _agent_int(agent, "autoScanMaxChangedFiles", _DEFAULT_MAX_CHANGED_FILES) if auto else 0
    if limit > 0 and len(targets) > limit:
        return _too_many_targets_message(len(targets), limit, auto)
    command = [
        "agent-scan",
        "scan",
        "--root",
        str(root),
        "--label",
        "edited-file",
        "--targets-from",
        str(_scan_targets_path(session_dir)),
        "--output-dir",
        str(_output_dir(session_dir, "edited-file")),
        "--format",
        "json",
    ]
    if auto:
        command.append("--automatic")
    code, out, err = _run(command, root, timeout=900)
    if code != 0:
        return "greprules edited-file scan failed: " + (err or out or "unknown scan failure").strip()
    try:
        payload = json.loads(out)
    except json.JSONDecodeError:
        return "greprules edited-file scan failed: could not parse agent-scan output"
    if not isinstance(payload, dict):
        return None
    message = _scan_message_with_selection_context(payload)
    if payload.get("status") == "scanned":
        if message:
            try:
                _last_summary_path(session_dir).write_text(message + "\n", encoding="utf-8")
            except Exception:
                pass
        _record_scan_attempt(session_dir)
        _clear_dirty(session_dir)
    return message or None


def _dirty_session_dirs(root: Path) -> List[Path]:
    sessions_root = _state_dir(root) / "sessions"
    if not sessions_root.is_dir():
        return []
    sessions: List[Path] = []
    for child in sessions_root.iterdir():
        if child.is_dir() and _read_lines(_dirty_files_path(child)):
            sessions.append(child)
    return sorted(sessions)


def _scan_edited(root: Path) -> Optional[str]:
    sessions = _dirty_session_dirs(root)
    if len(sessions) == 0:
        return None
    if len(sessions) > 1:
        return "multiple Hermes sessions have dirty files; start a session-scoped scan or run /greprules scan <path> for explicit files"
    return _scan_dirty(root, sessions[0].name, auto=False)


def _scan_with_args(root: Path, args: Sequence[str], label: str) -> str:
    code, out, err = _run(["agent-scan", "scan", "--root", str(root), "--label", label, *args], root, timeout=900)
    if code != 0:
        return "greprules scan failed: " + (err or out or "unknown scan failure").strip()
    return out.strip() or f"greprules {label} scan finished, but no result path was reported."


def _format_status(root: Path) -> str:
    report, error = _status_json(root)
    if report is None:
        return "greprules readiness check failed: " + error
    registry = report.get("registry") or {}
    lock = report.get("lock") or {}
    active = ((report.get("opengrep") or {}).get("active") or {})
    runtime = active.get("runtime") or {}
    lines = [
        f"status: {report.get('status', 'unknown')}",
        f"registry: {'ok' if registry.get('ok') else registry.get('error', 'not ready')}",
        f"rule packs: {'fetched' if lock.get('exists') else lock.get('message', 'not fetched yet')}",
        f"opengrep: {'ok' if active.get('ok') else active.get('error', 'not ready')}",
    ]
    if runtime:
        lines.append(
            "runtime: "
            + " ".join(
                part
                for part in [
                    str(runtime.get("mode") or "").strip(),
                    str(runtime.get("version") or "").strip(),
                    str(runtime.get("path") or "").strip(),
                ]
                if part
            )
        )
    agent = _agent_settings()
    lines.append(
        "hermes settings: autoScan={auto} trackEditedFiles={track} minIntervalSeconds={min_interval} maxChangedFiles={max_files} path={path}".format(
            auto=str(agent.get("autoScan", False)).lower(),
            track=str(agent.get("trackEditedFiles", True)).lower(),
            min_interval=agent.get("autoScanMinIntervalSeconds", _DEFAULT_MIN_INTERVAL_SECONDS),
            max_files=agent.get("autoScanMaxChangedFiles", _DEFAULT_MAX_CHANGED_FILES),
            path=_agent_settings_path(),
        )
    )
    if report.get("recommendedCommands"):
        lines.append("recommended: " + _recommended(report))
    return "\n".join(lines)


def _configure(root: Path, argv: List[str]) -> str:
    if not argv or argv[0] in {"status", "help", "-h", "--help"}:
        return (
            _format_status(root)
            + "\n\nConfigure with one of:\n"
            + "  /greprules configure managed\n"
            + "  /greprules configure registry https://api.greprules.io\n"
            + "  /greprules configure include-default-rules true|false\n"
            + "  /greprules configure auto-scan true|false\n"
            + "  /greprules configure track-edited-files true|false\n"
            + "  /greprules configure auto-scan-min-interval <seconds>\n"
            + "  /greprules configure auto-scan-max-changed-files <count>"
        )
    mode = argv[0]
    if mode == "managed":
        commands = [["setup-opengrep", "--root", str(root)]]
    elif mode in {"system", "path"}:
        return "greprules always uses its managed OpenGrep runtime. Run /greprules configure managed to prepare it."
    elif mode == "include-default-rules":
        if len(argv) < 2 or argv[1].lower() not in {"true", "false"}:
            return "usage: /greprules configure include-default-rules true|false"
        commands = [["agent-config", "set", "opengrep.includeDefaultRules", argv[1].lower(), "--global"]]
    elif mode == "registry":
        if len(argv) < 2:
            return "usage: /greprules configure registry https://api.greprules.io"
        commands = [["agent-config", "set", "registry", argv[1], "--global"]]
    elif mode == "auto-scan":
        if len(argv) < 2 or argv[1].lower() not in {"true", "false"}:
            return "usage: /greprules configure auto-scan true|false"
        path = _save_agent_setting("autoScan", argv[1].lower() == "true")
        return f"updated Hermes greprules settings: {path}\n" + _format_status(root)
    elif mode == "track-edited-files":
        if len(argv) < 2 or argv[1].lower() not in {"true", "false"}:
            return "usage: /greprules configure track-edited-files true|false"
        path = _save_agent_setting("trackEditedFiles", argv[1].lower() == "true")
        return f"updated Hermes greprules settings: {path}\n" + _format_status(root)
    elif mode == "auto-scan-min-interval":
        if len(argv) < 2 or not argv[1].isdigit():
            return "usage: /greprules configure auto-scan-min-interval <seconds>"
        path = _save_agent_setting("autoScanMinIntervalSeconds", int(argv[1]))
        return f"updated Hermes greprules settings: {path}\n" + _format_status(root)
    elif mode == "auto-scan-max-changed-files":
        if len(argv) < 2 or not argv[1].isdigit():
            return "usage: /greprules configure auto-scan-max-changed-files <count>"
        path = _save_agent_setting("autoScanMaxChangedFiles", int(argv[1]))
        return f"updated Hermes greprules settings: {path}\n" + _format_status(root)
    else:
        return "unknown configure option. Run /greprules configure for supported options."
    outputs = []
    for command in commands:
        code, out, err = _run(command, root, timeout=600)
        if code != 0:
            return "greprules configure failed: " + (err or out or "unknown configure failure").strip()
        if out.strip():
            outputs.append(out.strip())
    outputs.append(_format_status(root))
    return "\n".join(outputs)


def _help() -> str:
    return """\
/greprules — greprules.io rule-pack scanning for Hermes

Subcommands:
  configure [setting]            Inspect readiness, prepare managed OpenGrep, or change settings
  fetch <pack> [pack...]         Fetch explicit rule packs
  scan [scope|path...]           Scan edited files, git changes, explicit paths, or the full repository

Scan examples:
  /greprules scan                Scan edited files if tracked, otherwise git changes
  /greprules scan changed        Scan git working tree, staged, and untracked files
  /greprules scan src/auth       Scan explicit files or directories
  /greprules scan full           Scan the full repository
"""


def _scan_command(root: Path, cwd: Path, rest: List[str]) -> str:
    if not rest:
        return _scan_edited(root) or _scan_with_args(_git_root(cwd), ["--changed"], "working-tree")
    if rest[0] in {"edited", "edits", "latest", "session"}:
        return _scan_edited(root) or "no Hermes-edited files are tracked; edit files or run /greprules scan changed"
    scope = rest[0].lower()
    if scope in {"changed", "changes", "working-tree", "worktree", "diff", "staged", "untracked"}:
        return _scan_with_args(_git_root(cwd), ["--changed"], "working-tree")
    if scope in {"full", "all", "repo", "repository", "everything"}:
        return _scan_with_args(root, [], "full-repository")
    if scope in {"target", "path", "paths"}:
        targets = rest[1:]
        if not targets:
            return "usage: /greprules scan <path> [path...]"
        return _scan_with_args(root, targets, "target")
    return _scan_with_args(root, rest, "target")


def _handle_greprules(raw_args: str = "") -> str:
    try:
        argv = shlex.split(raw_args or "")
    except ValueError as exc:
        return f"could not parse arguments: {exc}"
    cwd = _current_cwd({})
    root = cwd
    if not argv or argv[0] in {"help", "-h", "--help"}:
        return _help()
    sub = argv[0]
    rest = argv[1:]
    if sub == "configure":
        return _configure(root, rest)
    if sub == "fetch":
        if not rest:
            return "usage: /greprules fetch <slug> [<slug>...]"
        args = ["fetch", "--root", str(root)]
        args.extend(rest)
        code, out, err = _run(args, root, timeout=600)
        return (out or err or "greprules fetch finished").strip() if code == 0 else "greprules fetch failed: " + (err or out).strip()
    if sub == "scan":
        return _scan_command(root, cwd, rest)
    return "unknown greprules subcommand.\n\n" + _help()


def _on_post_tool_call(
    tool_name: str = "",
    args: Optional[Dict[str, Any]] = None,
    session_id: str = "",
    task_id: str = "",
    **kwargs: Any,
) -> None:
    candidates = _extract_path_candidates(tool_name, args)
    if not candidates:
        return
    cwd = _current_cwd(kwargs)
    root = cwd
    agent = _agent_settings()
    if not _agent_bool(agent, "trackEditedFiles", True):
        return
    _mark_dirty_paths(candidates, cwd, session_id, task_id)


def _on_pre_llm_call(session_id: str = "", task_id: str = "", **kwargs: Any) -> Optional[Dict[str, str]]:
    contexts = []
    key = _tracker_key(session_id, task_id)
    for root in _roots_for_turn(_current_cwd(kwargs), session_id, task_id):
        message = _scan_dirty(root, key, auto=True)
        if message:
            contexts.append(message)
    if not contexts:
        return None
    return {"context": "\n\n".join(contexts)}


def register(ctx) -> None:
    ctx.register_command("greprules", _handle_greprules, "Run greprules configure, fetch, and scan commands", args_hint="<subcommand>")
    ctx.register_hook("post_tool_call", _on_post_tool_call)
    ctx.register_hook("pre_llm_call", _on_pre_llm_call)

    skills_dir = _PLUGIN_DIR / "skills"
    if skills_dir.exists():
        for child in sorted(skills_dir.iterdir()):
            skill_md = child / "SKILL.md"
            if child.is_dir() and skill_md.exists():
                ctx.register_skill(child.name, skill_md, f"greprules Hermes workflow: {child.name}")
