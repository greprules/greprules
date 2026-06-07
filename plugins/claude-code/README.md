# greprules Claude Code Plugin

This plugin teaches Claude Code how to use the local `greprules` CLI without embedding project configuration in prompts.

The plugin provides a `bin/greprules` wrapper, not the native Go binary itself. Claude Code skills and hooks invoke that wrapper directly, so `greprules` does not need to be installed on the user's shell `PATH`. The wrapper resolves the real CLI from `GREPRULES_CLI_PATH`, system `PATH` excluding itself, or a managed GitHub Release download cached under the OS user cache directory at `greprules/plugins/claude-code/greprules/<version>/`.

The plugin does not use install-time plugin configuration. OpenGrep runtime selection is stored in the greprules CLI config, so Claude Code, terminals, and CI can share the same behavior.

greprules scans fetched greprules.io packs only by default. Enable OpenGrep default auto-selected rules with `greprules config set opengrep.includeDefaultRules true --global` only when you explicitly want that extra ruleset.

It uses structured CLI outputs:

- `greprules config inspect --format json`
- `greprules doctor --format json`
- `.greprules/out/agent-result.json` for normal CLI scans
- `.greprules/plugin-data/claude-code/sessions/<session-id>/out/agent-result.json` for edited-file plugin scans

Skills:

- `/greprules:doctor`: inspect readiness and recommend next commands
- `/greprules:configure`: choose managed, system, or manual-path OpenGrep through the CLI
- `/greprules:scan-edited`: select rule packs from edited-file context, fetch them if needed, scan files Claude Code edited in this session, and summarize findings
- `/greprules:scan-working-tree`: select rule packs from git changed-file context, fetch them if needed, scan git working tree, staged, and untracked files, and summarize findings
- `/greprules:scan-target <path>`: select rule packs from explicit target context, fetch them if needed, scan files or directories, and summarize findings
- `/greprules:scan-full`: select rule packs from repository context, fetch them if needed, scan the full repository, and summarize findings

Automatic hooks:

- `SessionStart` runs `greprules doctor --format json` and reports registry or OpenGrep setup gaps. It does not install OpenGrep or scan. Missing rule packs are not reported as setup gaps because scan commands can fetch them automatically.
- `PostToolUse` runs after Claude Code edits files with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`, captures edited file paths from the hook input, filters them to code and security-relevant config candidates, and marks the current Claude session dirty. It does not scan.
- `Stop` attempts target-aware rule-pack selection for files Claude actually edited when `agent.autoScan=true`, fetches selected packs if needed, verifies OpenGrep readiness, runs one targeted scan, and blocks the stop once with a compact verification prompt. If deterministic pack selection is insufficient, it blocks with instructions for Claude to inspect available packs and choose explicit slugs. A successful scan clears dirty state for that Claude session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan. It works in non-git directories.
- Hook entries execute `scripts/greprules-hook.py`. The script handles Claude hook JSON, owns session-local edited-file state, and passes absolute explicit target lists to the provider-neutral `greprules agent-scan scan` primitive. Edited-file scans do not use git changed-file tracking; `/greprules:scan-working-tree` is the git-based scan path.
- Automatic Stop hook scans are disabled by default. Set `greprules config set agent.autoScan true --global` to enable the automatic Stop hook scan and block persistently. Edited-file tracking stays enabled so `/greprules:scan-edited` can still be run manually; a successful manual edited-file scan clears the tracked state.
- Set `greprules config set agent.trackEditedFiles false --global` only when you also want to disable edited-file tracking.
- Tune automatic scans with `greprules config set agent.autoScanMinIntervalSeconds 45 --global` and `greprules config set agent.autoScanMaxChangedFiles 100 --global`.
- `GREPRULES_AUTO_SCAN`, `GREPRULES_TRACK_EDITED_FILES`, `GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS`, and `GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES` remain available as one-session overrides before starting Claude Code.
- Hook state is written under the project `.greprules/plugin-data/claude-code/sessions/<session-id>/` directory by default. Override the provider state root with `GREPRULES_PLUGIN_STATE_DIR` only when needed.
- User config and caches are intentionally not removed by Claude Code plugin uninstall. Use `greprules cleanup --config --plugin-cache --dry-run` to inspect cleanup targets.
