# greprules Claude Code Plugin

This plugin teaches Claude Code how to use the local `greprules` CLI without embedding project configuration in prompts.

The plugin provides a `bin/greprules` wrapper. The wrapper resolves the real CLI from `GREPRULES_CLI_PATH`, system `PATH`, or a managed GitHub Release download cached under the OS user cache directory at `greprules/plugins/claude-code/greprules/<version>/`.

The plugin does not use install-time plugin configuration. OpenGrep runtime selection is stored in the greprules CLI config, so Claude Code, terminals, and CI can share the same behavior.

greprules includes OpenGrep default auto-selected rules by default. Disable them with `greprules config set opengrep.includeDefaultRules false --global` when a scan should use only greprules.io packs.

It uses structured CLI outputs:

- `greprules config inspect --format json`
- `greprules doctor --format json`
- `.greprules/out/agent-result.json`

Skills:

- `/greprules:doctor`: inspect readiness and recommend next commands
- `/greprules:configure`: choose managed, system, or manual-path OpenGrep through the CLI
- `/greprules:scan-edited`: fetch packs if needed, scan files Claude Code edited in this session, and summarize findings
- `/greprules:scan-working-tree`: fetch packs if needed, scan git working tree, staged, and untracked files, and summarize findings
- `/greprules:scan-target <path>`: fetch packs if needed, scan explicit files or directories, and summarize findings
- `/greprules:scan-full`: fetch packs if needed, scan the full repository, and summarize findings

Automatic hooks:

- `SessionStart` runs `greprules doctor --format json` and reports registry or OpenGrep setup gaps. It does not install OpenGrep or scan. Missing rule packs are not reported as setup gaps because scan commands can fetch them automatically.
- `PostToolUse` runs after Claude Code edits files with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`, captures edited file paths from the hook input, filters them to code and security-relevant config candidates, and marks the workspace dirty. It does not scan.
- `Stop` fetches rule packs if needed, verifies OpenGrep readiness, runs one targeted scan over files Claude actually edited, and blocks the stop once with a compact verification prompt. Automatic scan state is one-shot after a scan, terminal skip, or failure. It works in non-git directories.
- Hook entries execute `scripts/greprules-hook.py`. The script handles Claude hook JSON and StopBlock output, then delegates state and scan work to the provider-neutral `greprules agent-state` and `greprules agent-scan` primitives.
- Set `GREPRULES_AUTO_SCAN=false` before starting Claude Code to disable the automatic Stop hook scan and block for a session. Edited-file tracking stays enabled so `/greprules:scan-edited` can still be run manually; a successful manual edited-file scan clears the tracked state.
- Set `GREPRULES_TRACK_EDITED_FILES=false` before starting Claude Code only when you also want to disable edited-file tracking.
- Tune automatic scans with `GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS` and `GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES`.
- Hook state is written under the project `.greprules/plugin-data/agent` directory by default. Override with `GREPRULES_PLUGIN_STATE_DIR` only when needed.
- User config and caches are intentionally not removed by Claude Code plugin uninstall. Use `greprules cleanup --config --plugin-cache --dry-run` to inspect cleanup targets.
