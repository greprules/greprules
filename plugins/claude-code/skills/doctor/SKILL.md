---
description: Check greprules readiness and summarize required setup
---

Use this skill when the user wants to verify whether greprules is ready to run in the current repository.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. Parse the JSON output instead of reading config files directly.
4. Report the `status`, active OpenGrep runtime, registry status, lockfile status, warnings, and `recommendedCommands`.
5. If OpenGrep is not ready, check `opengrep.system.ok` first and ask the user how to configure OpenGrep:
   - Use system OpenGrep on PATH. Recommend this when `opengrep.system.ok` is true, and include the detected path/version.
   - Install managed OpenGrep. Recommend this when no system OpenGrep is available.
   - Use a manual OpenGrep executable path. Ask for the absolute path before applying it.
6. Use AskUserQuestion when available for the runtime choice. If it is not available, ask one concise question and wait.
7. Apply the selected setup with `greprules config set ... --global`; for managed mode also run `greprules setup-opengrep`.
8. Run `greprules doctor --format json` again and summarize readiness.
9. If the CLI wrapper reports that the real CLI is missing, tell the user to put `greprules` on `PATH`, set `GREPRULES_CLI_PATH`, or let the plugin bootstrap the release binary under the greprules user cache. `GREPRULES_PLUGIN_CACHE_DIR` can override that cache only for debugging.

Do not run `greprules fetch` or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
