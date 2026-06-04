"""Hermes plugin for greprules.

The plugin keeps Hermes-specific orchestration in Python and delegates rule
fetching, OpenGrep runtime handling, scanning, and result generation to the
greprules Go CLI.
"""

from __future__ import annotations

import json
import logging
import os
import shlex
import shutil
import subprocess
import threading
import time
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Sequence, Set, Tuple

logger = logging.getLogger(__name__)

_PLUGIN_DIR = Path(__file__).resolve().parent
_DEFAULT_MIN_INTERVAL_SECONDS = 45
_DEFAULT_MAX_CHANGED_FILES = 100
_RECENT_ROOTS: Dict[str, Set[str]] = {}
_LOCK = threading.Lock()


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
        return 127, "", "greprules CLI not found; install it, put it on PATH, or set GREPRULES_CLI_PATH"
    except subprocess.TimeoutExpired as exc:
        out = exc.stdout if isinstance(exc.stdout, str) else ""
        err = exc.stderr if isinstance(exc.stderr, str) else ""
        return 124, out, (err + "\ngreprules command timed out").strip()
    return proc.returncode, proc.stdout, proc.stderr


def _agent_config(root: Path) -> Dict[str, Any]:
    code, out, err = _run(["config", "inspect", "--root", str(root), "--format", "json"], root)
    if code != 0:
        logger.debug("greprules config inspect failed: %s", err or out)
        return {}
    try:
        resolution = json.loads(out)
    except json.JSONDecodeError:
        logger.debug("greprules config inspect returned non-JSON: %s", out)
        return {}
    config = resolution.get("config") if isinstance(resolution, dict) else {}
    agent = config.get("agent") if isinstance(config, dict) else {}
    return agent if isinstance(agent, dict) else {}


def _agent_bool(agent: Dict[str, Any], key: str, default: bool, *env_names: str) -> bool:
    value = agent.get(key, default)
    parsed = value if isinstance(value, bool) else default
    for env_name in env_names:
        raw = os.environ.get(env_name, "").strip().lower()
        if raw:
            return raw not in {"0", "false", "no", "off"}
    return parsed


def _agent_int(agent: Dict[str, Any], key: str, default: int, *env_names: str) -> int:
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


def _dirty_marker_path(root: Path) -> Path:
    return _state_dir(root) / "dirty"


def _last_scan_path(root: Path) -> Path:
    return _state_dir(root) / "last-scan"


def _last_scan_recent(root: Path, agent: Dict[str, Any]) -> bool:
    min_interval = _agent_int(
        agent,
        "autoScanMinIntervalSeconds",
        _DEFAULT_MIN_INTERVAL_SECONDS,
        "GREPRULES_HERMES_AUTO_SCAN_MIN_INTERVAL_SECONDS",
        "GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS",
    )
    if min_interval <= 0:
        return False
    try:
        last = int(_last_scan_path(root).read_text().strip())
    except Exception:
        return False
    return int(time.time()) - last < min_interval


def _tracker_key(session_id: str, task_id: str = "") -> str:
    return task_id or session_id or "default"


def _remember_root(root: Path, session_id: str, task_id: str = "") -> None:
    key = _tracker_key(session_id, task_id)
    with _LOCK:
        _RECENT_ROOTS.setdefault(key, set()).add(str(root))


def _roots_for_turn(cwd: Path, session_id: str) -> List[Path]:
    roots: Set[str] = {str(_git_root(cwd))}
    with _LOCK:
        if session_id:
            roots.update(_RECENT_ROOTS.get(session_id, set()))
        for key, values in _RECENT_ROOTS.items():
            if key != session_id:
                roots.update(values)
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


def _mark_dirty_paths(paths: Iterable[str], cwd: Path, session_id: str, task_id: str = "") -> None:
    root = _git_root(cwd)
    path_list = [path for path in paths if path]
    if not path_list:
        return
    command = [
        "agent-state",
        "mark-dirty",
        "--root",
        str(root),
        "--state-dir",
        str(_state_dir(root)),
        "--cwd",
        str(cwd),
    ]
    for path in path_list:
        command.extend(["--path", path])
    code, out, err = _run(command, root)
    if code != 0:
        logger.debug("greprules agent-state mark-dirty failed: %s", err or out)
        return
    try:
        payload = json.loads(out)
    except json.JSONDecodeError:
        logger.debug("greprules agent-state mark-dirty returned non-JSON: %s", out)
        return
    if payload.get("marked"):
        _remember_root(root, session_id, task_id)


def _doctor_json(root: Path) -> Tuple[Optional[Dict[str, Any]], str]:
    code, out, err = _run(["doctor", "--root", str(root), "--format", "json"], root)
    if code != 0:
        return None, (err or out or "greprules doctor failed").strip()
    try:
        return json.loads(out), ""
    except json.JSONDecodeError as exc:
        return None, f"could not parse greprules doctor JSON: {exc}"


def _recommended(report: Dict[str, Any]) -> str:
    commands = report.get("recommendedCommands") or []
    if isinstance(commands, list) and commands:
        return ", ".join(str(command) for command in commands)
    return "greprules doctor --format json"


def _scan_dirty(root: Path, *, auto: bool) -> Optional[str]:
    agent = _agent_config(root)
    if auto and not _agent_bool(agent, "autoScan", False, "GREPRULES_HERMES_AUTO_SCAN", "GREPRULES_AUTO_SCAN"):
        return None
    if auto and _last_scan_recent(root, agent):
        return None
    if not _dirty_marker_path(root).exists():
        return None
    command = [
        "agent-scan",
        "edited",
        "--root",
        str(root),
        "--state-dir",
        str(_state_dir(root)),
        "--label",
        "edited-file",
    ]
    if auto:
        command.extend(
            [
                "--automatic",
                "--max-targets",
                str(
                    _agent_int(
                        agent,
                        "autoScanMaxChangedFiles",
                        _DEFAULT_MAX_CHANGED_FILES,
                        "GREPRULES_HERMES_AUTO_SCAN_MAX_CHANGED_FILES",
                        "GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES",
                    )
                ),
                "--too-many-suggestion",
                "Run /greprules scan-edited or /greprules scan-working-tree when ready.",
            ]
        )
    code, out, err = _run(command, root, timeout=900)
    if code != 0:
        return "greprules edited-file scan failed: " + (err or out or "unknown scan failure").strip()
    return out.strip() or None


def _scan_with_args(root: Path, args: Sequence[str], label: str) -> str:
    code, out, err = _run(["agent-scan", "scan", "--root", str(root), "--label", label, *args], root, timeout=900)
    if code != 0:
        return "greprules scan failed: " + (err or out or "unknown scan failure").strip()
    return out.strip() or f"greprules {label} scan finished. Full result: {root / '.greprules' / 'out' / 'agent-result.json'}"


def _format_doctor(root: Path) -> str:
    report, error = _doctor_json(root)
    if report is None:
        return "greprules doctor failed: " + error
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
    config = report.get("config") or {}
    effective = config.get("config") if isinstance(config, dict) else {}
    agent = effective.get("agent") if isinstance(effective, dict) else {}
    if isinstance(agent, dict):
        lines.append(
            "agent: autoScan={auto} trackEditedFiles={track} maxChangedFiles={max_files}".format(
                auto=str(agent.get("autoScan", False)).lower(),
                track=str(agent.get("trackEditedFiles", True)).lower(),
                max_files=agent.get("autoScanMaxChangedFiles", _DEFAULT_MAX_CHANGED_FILES),
            )
        )
    if report.get("recommendedCommands"):
        lines.append("recommended: " + _recommended(report))
    return "\n".join(lines)


def _configure(root: Path, argv: List[str]) -> str:
    if not argv or argv[0] in {"status", "help", "-h", "--help"}:
        return (
            _format_doctor(root)
            + "\n\nConfigure with one of:\n"
            + "  /greprules configure system\n"
            + "  /greprules configure managed\n"
            + "  /greprules configure path /absolute/path/to/opengrep\n"
            + "  /greprules configure registry https://api.greprules.io\n"
            + "  /greprules configure include-default-rules true|false\n"
            + "  /greprules configure auto-scan true|false\n"
            + "  /greprules configure track-edited-files true|false\n"
            + "  /greprules configure auto-scan-min-interval <seconds>\n"
            + "  /greprules configure auto-scan-max-changed-files <count>"
        )
    mode = argv[0]
    if mode == "system":
        commands = [["config", "set", "opengrep.mode", "system", "--global"]]
    elif mode == "managed":
        commands = [
            ["config", "set", "opengrep.mode", "managed", "--global"],
            ["setup-opengrep", "--root", str(root)],
        ]
    elif mode == "path":
        if len(argv) < 2:
            return "usage: /greprules configure path /absolute/path/to/opengrep"
        commands = [
            ["config", "set", "opengrep.mode", "path", "--global"],
            ["config", "set", "opengrep.path", argv[1], "--global"],
        ]
    elif mode == "include-default-rules":
        if len(argv) < 2 or argv[1].lower() not in {"true", "false"}:
            return "usage: /greprules configure include-default-rules true|false"
        commands = [["config", "set", "opengrep.includeDefaultRules", argv[1].lower(), "--global"]]
    elif mode == "registry":
        if len(argv) < 2:
            return "usage: /greprules configure registry https://api.greprules.io"
        commands = [["config", "set", "registry", argv[1], "--global"]]
    elif mode == "auto-scan":
        if len(argv) < 2 or argv[1].lower() not in {"true", "false"}:
            return "usage: /greprules configure auto-scan true|false"
        commands = [["config", "set", "agent.autoScan", argv[1].lower(), "--global"]]
    elif mode == "track-edited-files":
        if len(argv) < 2 or argv[1].lower() not in {"true", "false"}:
            return "usage: /greprules configure track-edited-files true|false"
        commands = [["config", "set", "agent.trackEditedFiles", argv[1].lower(), "--global"]]
    elif mode == "auto-scan-min-interval":
        if len(argv) < 2 or not argv[1].isdigit():
            return "usage: /greprules configure auto-scan-min-interval <seconds>"
        commands = [["config", "set", "agent.autoScanMinIntervalSeconds", argv[1], "--global"]]
    elif mode == "auto-scan-max-changed-files":
        if len(argv) < 2 or not argv[1].isdigit():
            return "usage: /greprules configure auto-scan-max-changed-files <count>"
        commands = [["config", "set", "agent.autoScanMaxChangedFiles", argv[1], "--global"]]
    else:
        return "unknown configure option. Run /greprules configure for supported options."
    outputs = []
    for command in commands:
        code, out, err = _run(command, root, timeout=600)
        if code != 0:
            return "greprules configure failed: " + (err or out or "unknown configure failure").strip()
        if out.strip():
            outputs.append(out.strip())
    outputs.append(_format_doctor(root))
    return "\n".join(outputs)


def _help() -> str:
    return """\
/greprules — greprules.io rule-pack scanning for Hermes

Subcommands:
  doctor                         Check registry, rule-pack fetch state, and OpenGrep runtime
  configure [mode]               Configure OpenGrep runtime: managed, system, path <exe>
  fetch [pack]                   Fetch recommended packs, or one named pack
  scan-edited                    Scan files Hermes edited and tracked this session
  scan-working-tree              Scan git working tree, staged, and untracked files
  scan-target <path> [...]       Scan explicit files or directories
  scan-full                      Scan the full repository

Aliases:
  /greprules-doctor
  /greprules-scan-edited
  /greprules-scan-working-tree
  /greprules-scan-target <path>
  /greprules-scan-full
"""


def _handle_greprules(raw_args: str = "") -> str:
    try:
        argv = shlex.split(raw_args or "")
    except ValueError as exc:
        return f"could not parse arguments: {exc}"
    cwd = _current_cwd({})
    root = _git_root(cwd)
    if not argv or argv[0] in {"help", "-h", "--help"}:
        return _help()
    sub = argv[0]
    rest = argv[1:]
    if sub == "doctor":
        return _format_doctor(root)
    if sub == "configure":
        return _configure(root, rest)
    if sub == "fetch":
        args = ["fetch", "--root", str(root)]
        if rest:
            args.extend(["--pack", rest[0]])
        code, out, err = _run(args, root, timeout=600)
        return (out or err or "greprules fetch finished").strip() if code == 0 else "greprules fetch failed: " + (err or out).strip()
    if sub == "scan-edited":
        return _scan_dirty(root, auto=False) or "no Hermes-edited files are tracked; edit files or run /greprules scan-working-tree"
    if sub == "scan-working-tree":
        return _scan_with_args(root, ["--changed"], "working-tree")
    if sub == "scan-full":
        return _scan_with_args(root, ["--full"], "full-repository")
    if sub == "scan-target":
        if not rest:
            return "usage: /greprules scan-target <path> [path...]"
        args: List[str] = []
        for target in rest:
            args.extend(["--target", target])
        return _scan_with_args(root, args, "target")
    return "unknown greprules subcommand.\n\n" + _help()


def _alias(subcommand: str):
    def handler(raw_args: str = "") -> str:
        return _handle_greprules(" ".join(part for part in [subcommand, raw_args] if part.strip()))

    return handler


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
    root = _git_root(cwd)
    agent = _agent_config(root)
    if not _agent_bool(agent, "trackEditedFiles", True, "GREPRULES_HERMES_TRACK_EDITED_FILES", "GREPRULES_TRACK_EDITED_FILES"):
        return
    _mark_dirty_paths(candidates, cwd, session_id, task_id)


def _on_pre_llm_call(session_id: str = "", **kwargs: Any) -> Optional[Dict[str, str]]:
    contexts = []
    for root in _roots_for_turn(_current_cwd(kwargs), session_id):
        message = _scan_dirty(root, auto=True)
        if message:
            contexts.append(message)
    if not contexts:
        return None
    return {"context": "\n\n".join(contexts)}


def register(ctx) -> None:
    ctx.register_command("greprules", _handle_greprules, "Run greprules doctor, configure, fetch, and scan commands", args_hint="<subcommand>")
    ctx.register_command("greprules-doctor", _alias("doctor"), "Check greprules readiness")
    ctx.register_command("greprules-scan-edited", _alias("scan-edited"), "Scan files Hermes edited in this session")
    ctx.register_command("greprules-scan-working-tree", _alias("scan-working-tree"), "Scan git working tree, staged, and untracked files")
    ctx.register_command("greprules-scan-target", _alias("scan-target"), "Scan explicit greprules targets", args_hint="<path>")
    ctx.register_command("greprules-scan-full", _alias("scan-full"), "Scan the full repository with greprules")
    ctx.register_hook("post_tool_call", _on_post_tool_call)
    ctx.register_hook("pre_llm_call", _on_pre_llm_call)

    skills_dir = _PLUGIN_DIR / "skills"
    if skills_dir.exists():
        for child in sorted(skills_dir.iterdir()):
            skill_md = child / "SKILL.md"
            if child.is_dir() and skill_md.exists():
                ctx.register_skill(child.name, skill_md, f"greprules Hermes workflow: {child.name}")
