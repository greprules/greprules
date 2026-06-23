# greprules Claude Code Plugin

This plugin teaches Claude Code how to use the local `greprules` CLI without embedding project configuration in prompts.

The plugin provides a `bin/greprules` wrapper, not the native Go binary itself. Claude Code skills and hooks invoke that wrapper directly, so `greprules` does not need to be installed on the user's shell `PATH`. The wrapper resolves the real CLI from `GREPRULES_CLI_PATH`, then the plugin-pinned managed GitHub Release cached under the OS user cache directory at `greprules/plugins/claude-code/greprules/<version>/`, and uses system `PATH` only as a fallback if managed bootstrap fails.

The plugin does not use install-time plugin configuration. greprules always uses its managed OpenGrep runtime, shared by standalone CLI and plugin scans.

greprules scans fetched greprules.io packs only by default. Enable OpenGrep default auto-selected rules with `greprules agent-config set opengrep.includeDefaultRules true --global` only when you explicitly want that extra ruleset.

It uses structured CLI outputs:

- `greprules agent-status --format json`
- `.greprules/plugin-data/claude-code/sessions/<session-id>/runs/<run-id>/agent-result.json` for plugin agent scans
- `Full result:` in the scan summary reports the exact result file to read

Skills:

- `/greprules:setup`: set up greprules after installing the plugin
- `/greprules:configure`: inspect status and configure registry, default rules, or Claude Code hook settings
- `/greprules:scan-edited`: select rule packs from edited-file context, fetch them if needed, scan files Claude Code edited in this session, and summarize findings
- `/greprules:scan-working-tree`: select rule packs from git changed-file context, fetch them if needed, scan git working tree, staged, and untracked files, and summarize findings
- `/greprules:scan-target <path>`: select rule packs from explicit target context, fetch them if needed, scan files or directories, and summarize findings
- `/greprules:scan-full`: select rule packs from repository context, fetch them if needed, scan the full repository, and summarize findings
- `/greprules:submit-feedback`: review a previous `agent-result.json`, prepare a redacted feedback bundle, preview uploaded and excluded fields, and submit contextual feedback only after explicit user approval
- `/greprules:propose-rule`: prepare an agent-generated rule proposal bundle with provenance and public tests, preview uploaded and excluded fields, and submit only after explicit user approval

Automatic hooks:

- `SessionStart` reports registry or OpenGrep setup gaps. It does not install OpenGrep or scan. Missing rule packs are not reported as setup gaps because scan commands can fetch them automatically.
- `PostToolUse` runs after Claude Code edits files with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`, captures edited file paths from the hook input, filters them to code and security-relevant config candidates, and marks the current Claude session dirty. It does not scan.
- `Stop` attempts target-aware rule-pack selection for files Claude actually edited when Claude Code greprules `autoScan=true`, fetches selected packs if needed, verifies OpenGrep readiness, runs one targeted scan, and blocks the stop once with a compact verification prompt. If deterministic pack selection is insufficient, it blocks with instructions for Claude to inspect available packs and choose explicit slugs. A successful scan clears dirty state for that Claude session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan. It works in non-git directories.
- Hook entries execute `scripts/greprules-hook.py`. The script handles Claude hook JSON, owns session-local edited-file state, and passes absolute explicit target lists to the provider-neutral `greprules agent-scan scan` primitive. Edited-file scans do not use git changed-file tracking; `/greprules:scan-working-tree` is the git-based scan path.
- Automatic Stop hook scans are disabled by default. Use `/greprules:configure` to update Claude Code greprules settings under `~/.claude/plugins/greprules/settings.json`.
- Edited-file tracking stays enabled so `/greprules:scan-edited` can still be run manually; a successful manual edited-file scan clears the tracked state.
- Hook state is written under the project `.greprules/plugin-data/claude-code/sessions/<session-id>/` directory by default. Override the provider state root with `GREPRULES_PLUGIN_STATE_DIR` only when needed.
- User config and caches are intentionally not removed by Claude Code plugin uninstall. Use `greprules cleanup --config --plugin-cache --dry-run` to inspect cleanup targets.

Community feedback and rule proposal submission require browser-approved greprules.io CLI login from `greprules auth login`. They are never triggered automatically by hooks.
