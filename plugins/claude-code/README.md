# greprules Claude Code Plugin

This plugin teaches Claude Code how to use the local `greprules` CLI without embedding project configuration in prompts.

The plugin provides a `bin/greprules` wrapper. The wrapper resolves the real CLI from `GREPRULES_CLI_PATH`, system `PATH`, or a managed GitHub Release download cached under the OS user cache directory at `greprules/claude-plugin/greprules/v0.1.0/`.

The plugin does not use install-time plugin configuration. OpenGrep runtime selection is stored in the greprules CLI config, so Claude Code, terminals, and CI can share the same behavior.

greprules includes OpenGrep default auto-selected rules by default. Disable them with `greprules config set opengrep.includeDefaultRules false --global` when a scan should use only greprules.io packs.

It uses structured CLI outputs:

- `greprules config inspect --format json`
- `greprules doctor --format json`
- `.greprules/out/agent-result.json`

Skills:

- `/greprules:doctor`: inspect readiness and recommend next commands
- `/greprules:configure`: choose managed, system, or manual-path OpenGrep through the CLI
- `/greprules:scan`: fetch packs if needed, scan changed files or explicit targets, and summarize findings

Automatic hooks:

- `SessionStart` runs `greprules doctor --format json` and reports setup gaps. It does not install OpenGrep or scan.
- `PostToolUse` runs after Claude Code edits files with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`, captures the edited file path from the hook input, and marks the workspace dirty. It does not scan.
- `Stop` fetches rule packs if needed, verifies OpenGrep readiness, runs one targeted scan over files Claude actually edited, and injects a compact scan summary into Claude's next model context. It works in non-git directories.
- Set `GREPRULES_AUTO_SCAN=false` before starting Claude Code to disable this hook for a session.
- Tune automatic scans with `GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS` and `GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES`.
- Hook state is written under the project `.greprules/plugin-data/claude-code` directory by default. Override with `GREPRULES_PLUGIN_STATE_DIR` only when needed.
- User config and caches are intentionally not removed by Claude Code plugin uninstall. Use `greprules cleanup --config --plugin-cache --dry-run` to inspect cleanup targets.
