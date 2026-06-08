---
description: Use greprules from Hermes slash commands and edited-file hooks
---

Use this skill when the user wants Hermes to run greprules scans or understand greprules plugin behavior.

Hermes slash commands and hooks resolve the greprules command through the plugin adapter. They use `GREPRULES_CLI_PATH` only as an explicit local override, otherwise the plugin-bundled `bin/greprules` wrapper, and only then `greprules` on shell `PATH` as a fallback. Do not treat a missing `command -v greprules` result as a Hermes plugin setup failure.

Available slash commands:

1. `/greprules setup` sets up greprules after installation.
2. `/greprules configure` checks registry access, rule-pack fetch state, OpenGrep readiness, and effective agent settings.
3. `/greprules configure managed|system|path <exe>` configures the OpenGrep runtime.
4. `/greprules configure registry <url>`, `include-default-rules true|false`, `auto-scan true|false`, `track-edited-files true|false`, `auto-scan-min-interval <seconds>`, and `auto-scan-max-changed-files <count>` configure persistent greprules behavior.
5. `/greprules scan-edited` scans files tracked by the Hermes `post_tool_call` hook for a single dirty session.
6. `/greprules scan-working-tree` scans git working tree, staged, and untracked files.
7. `/greprules scan-target <path>` scans explicit files or directories.
8. `/greprules scan-full` scans the full repository.

Aliases are also available: `/greprules-scan-edited`, `/greprules-scan-working-tree`, `/greprules-scan-target <path>`, and `/greprules-scan-full`.

When rule packs are not fetched yet, use agent-assisted selection: inspect the target files and run `greprules recommend --format json --agent` with `--target`, `--changed`, or `--targets-from` matching the scan scope through the resolved plugin command. Edited-file scans pass absolute target files; git changed-file selection is reserved for `/greprules scan-working-tree`. Choose only slugs present in `availablePacks`, fetch them with the explicit fetch command shown in the scan message, then rerun the scan.

The `pre_llm_call` hook can inject compact edited-file scan results before the next model turn when `agent.autoScan=true`. Automatic scan context injection is disabled by default; use `/greprules configure auto-scan true` to enable it persistently while keeping manual commands available. `GREPRULES_HERMES_AUTO_SCAN=true` remains available as a one-session override.

Hermes edited-file state is stored under `.greprules/plugin-data/hermes/sessions/<session-or-task-id>/`. The adapter passes only explicit target files to the CLI. If multiple Hermes sessions have dirty files, do not merge them; use the current session flow or `/greprules scan-target <path>` for explicit files.
