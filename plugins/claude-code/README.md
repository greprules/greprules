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

- `/greprules:configure`: inspect first-run readiness, prepare managed OpenGrep if needed, and configure registry, default rules, or Claude Code hook settings
- `/greprules:scan`: select rule packs from the requested scope, fetch them if needed, scan edited files, git changes, explicit paths, or the full repository, summarize findings, and optionally prepare community feedback after explicit user approval
- `/greprules:auth-login`: sign the local greprules CLI in to greprules.io from the Claude Code conversation by showing the browser approval URL/code and waiting for approval

Automatic hooks:

- `SessionStart` reports registry or OpenGrep readiness gaps. It does not install OpenGrep or scan. Missing rule packs are not reported as readiness gaps because scan commands can fetch them automatically.
- `PostToolUse` runs after Claude Code edits files with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`, captures edited file paths from the hook input, filters them to code and security-relevant config candidates, and marks the current Claude session dirty. It does not scan.
- `Stop` attempts target-aware rule-pack selection for files Claude actually edited when Claude Code greprules `autoScan=true`, fetches selected packs if needed, verifies OpenGrep readiness, runs one targeted scan, and blocks the stop once with a compact verification prompt. If deterministic pack selection is insufficient, it blocks with instructions for Claude to inspect available packs and choose explicit slugs. A successful scan clears dirty state for that Claude session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan. It works in non-git directories.
- Hook entries execute `scripts/greprules-hook.py`. The script handles Claude hook JSON, owns session-local edited-file state, and passes absolute explicit target lists to the provider-neutral `greprules agent-scan scan` primitive. Edited-file scans do not use git changed-file tracking; `/greprules:scan` can choose the git changed-file scan path when the user asks for working-tree or diff scope.
- Automatic Stop hook scans are disabled by default. Use `/greprules:configure` to update Claude Code greprules settings under `~/.claude/plugins/greprules/settings.json`.
- Edited-file tracking stays enabled so `/greprules:scan` can still run a manual edited-file scan; a successful manual edited-file scan clears the tracked state.
- Hook state is written under the project `.greprules/plugin-data/claude-code/sessions/<session-id>/` directory by default. Override the provider state root with `GREPRULES_PLUGIN_STATE_DIR` only when needed.
- User config and caches are intentionally not removed by Claude Code plugin uninstall. Use `greprules cleanup --config --plugin-cache --dry-run` to inspect cleanup targets.

Community contribution is conversation-driven instead of exposed as separate slash commands. After `/greprules:scan` reviews findings, Claude Code can offer to submit contextual true-positive, false-positive, warning, or diagnostic feedback. If Claude Code independently identifies a vulnerability that should become a reusable rule, the user can ask for a greprules.io rule proposal in normal chat. In both cases Claude Code prepares a redacted bundle, previews uploaded and excluded fields, runs `/greprules:auth-login` if needed, and submits only after the user approves the exact scope in conversation. Contributions are never triggered automatically by hooks.
