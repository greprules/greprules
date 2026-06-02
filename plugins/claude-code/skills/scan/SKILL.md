---
description: Fetch greprules packs, run OpenGrep scan, and summarize agent-readable findings
---

Use this skill when the user asks to scan the current repository with greprules.

Primary workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, check `opengrep.system.ok` first and ask the user how to configure OpenGrep before scanning:
   - Use system OpenGrep on PATH. Recommend this when `opengrep.system.ok` is true, and include the detected path/version.
   - Install managed OpenGrep. Recommend this when no system OpenGrep is available.
   - Use a manual OpenGrep executable path. Ask for the absolute path before applying it.
4. Use AskUserQuestion when available for the runtime choice. If it is not available, ask one concise question and wait.
5. Apply the selected setup with `greprules config set ... --global`; for managed mode also run `greprules setup-opengrep`. Then rerun `greprules doctor --format json`.
6. If `lock.exists` is false and `registry.ok` is true, run `greprules fetch`.
7. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
8. Run `greprules scan --changed` by default, or `greprules scan --target <path>` when the user names specific files.
9. Read `.greprules/out/agent-result.json`.
10. Summarize findings by rule id, severity, file, line, and message.
11. Propose code fixes for true-positive-looking findings, but do not upload rules or create rule drafts.

Fallbacks:

- If there are no changed files, report that the changed-file scan found no targets and ask whether to run `greprules scan --full`.
- If the current directory is not a git repository, ask whether to run `greprules scan --full` or scan specific files with `greprules scan --target <path>`.
- If the user explicitly asks for a full scan, run `greprules scan --full`.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
- If the CLI wrapper reports that the real CLI is missing, tell the user to put `greprules` on `PATH`, set `GREPRULES_CLI_PATH`, or let the plugin bootstrap the release binary under the greprules user cache. `GREPRULES_PLUGIN_CACHE_DIR` can override that cache only for debugging.

Do not parse `.greprules/config.yaml`, `.greprules/config.local.json`, or `~/.config/greprules/config.json` manually unless the CLI command itself is failing and the user asks for debugging.
