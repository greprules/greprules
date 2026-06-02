# greprules Claude Code Plugin

This plugin teaches Claude Code how to use the local `greprules` CLI without embedding project configuration in prompts.

The plugin provides a `bin/greprules` wrapper. The wrapper resolves the real CLI from `GREPRULES_CLI_PATH`, `$CLAUDE_PLUGIN_DATA/greprules`, system `PATH`, or a managed GitHub Release download cached under `$CLAUDE_PLUGIN_DATA/greprules/v0.1.0/`. It uses structured CLI outputs:

- `greprules config inspect --format json`
- `greprules doctor --format json`
- `.greprules/out/agent-result.json`

Skills:

- `/greprules:doctor`: inspect readiness and recommend next commands
- `/greprules:configure`: set global/local greprules config through the CLI
- `/greprules:scan`: fetch packs if needed, scan changed files, and summarize findings

Automatic hooks:

- `SessionStart` runs `greprules doctor --format json` and reports setup gaps. It does not scan.
- `PostToolUse` runs after Claude Code edits files with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit` and only marks the workspace dirty. It does not scan.
- `Stop` fetches rule packs if needed, verifies OpenGrep readiness, runs one `greprules scan --changed`, and injects a compact scan summary into Claude's next model context.
- Set `GREPRULES_AUTO_SCAN=false` before starting Claude Code to disable this hook for a session.
- Tune automatic scans with `GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS` and `GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES`.
