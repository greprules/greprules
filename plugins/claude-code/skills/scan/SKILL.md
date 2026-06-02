---
description: Fetch greprules packs, run OpenGrep scan, and summarize agent-readable findings
---

Use this skill when the user asks to scan the current repository with greprules.

Primary workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. If `lock.exists` is false and `registry.ok` is true, run `greprules fetch`.
4. If `opengrep.active.ok` is false, stop and summarize `recommendedCommands`.
5. Run `greprules scan --changed`.
6. Read `.greprules/out/agent-result.json`.
7. Summarize findings by rule id, severity, file, line, and message.
8. Propose code fixes for true-positive-looking findings, but do not upload rules or create rule drafts.

Fallbacks:

- If there are no changed files, report that the changed-file scan found no targets and ask whether to run `greprules scan --full`.
- If the user explicitly asks for a full scan, run `greprules scan --full`.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
- If OpenGrep is not ready, summarize whether plugin configuration is trying automatic install, system PATH, or a custom executable path.
- If the CLI wrapper reports that the real CLI is missing, tell the user to put `greprules` on `PATH`, set `GREPRULES_CLI_PATH`, or let the plugin bootstrap the release binary under `$CLAUDE_PLUGIN_DATA/greprules/v0.1.0/`.

Do not parse `.greprules/config.yaml`, `.greprules/config.local.json`, or `~/.config/greprules/config.json` manually unless the CLI command itself is failing and the user asks for debugging.
