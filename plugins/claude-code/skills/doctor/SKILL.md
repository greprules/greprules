---
description: Check greprules readiness and summarize required setup
---

Use this skill when the user wants to verify whether greprules is ready to run in the current repository.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. Parse the JSON output instead of reading config files directly.
4. Report the `status`, active OpenGrep runtime, registry status, lockfile status, warnings, and `recommendedCommands`.
5. If the CLI wrapper reports that the real CLI is missing, tell the user to put `greprules` on `PATH`, set `GREPRULES_CLI_PATH`, or place the binary at `$CLAUDE_PLUGIN_DATA/greprules`.

Do not run `greprules setup-opengrep`, `greprules fetch`, or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
