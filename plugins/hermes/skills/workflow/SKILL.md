---
description: Use greprules from Hermes slash commands and edited-file hooks
---

Use this skill when the user wants Hermes to run greprules scans or understand greprules plugin behavior.

Available slash commands:

1. `/greprules doctor` checks registry access, rule-pack fetch state, and OpenGrep readiness.
2. `/greprules configure managed|system|path <exe>` configures the OpenGrep runtime.
3. `/greprules scan-edited` scans files tracked by the Hermes `post_tool_call` hook.
4. `/greprules scan-working-tree` scans git working tree, staged, and untracked files.
5. `/greprules scan-target <path>` scans explicit files or directories.
6. `/greprules scan-full` scans the full repository.

Aliases are also available: `/greprules-doctor`, `/greprules-scan-edited`, `/greprules-scan-working-tree`, `/greprules-scan-target <path>`, and `/greprules-scan-full`.

When rule packs are not fetched yet, use agent-assisted selection: inspect the target files and run `greprules recommend --format json --agent` with `--target`, `--changed`, or `--targets-from` matching the scan scope. Choose only slugs present in `availablePacks`, fetch them with explicit `greprules fetch --pack <slug>` arguments, then rerun the scan.

The `pre_llm_call` hook can inject compact edited-file scan results before the next model turn. Set `GREPRULES_HERMES_AUTO_SCAN=false` to disable automatic scan context injection while keeping manual commands available.
