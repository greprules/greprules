---
description: Scan files that Claude Code edited in the current session with greprules
---

Use this skill when the user asks for `/greprules:scan-edited` or wants to manually scan files Claude Code edited.

This command uses the file list tracked by the plugin `PostToolUse` hook. It is the manual counterpart to the optional automatic Stop hook scan. Automatic Stop hook scans are disabled by default; `greprules config set agent.autoScan true --global` enables the Stop hook scan and block behavior while keeping this command available. A successful edited-file scan clears the tracked state. `GREPRULES_AUTO_SCAN=true` remains available as a one-session override.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules agent-state prepare-targets`, then run `greprules recommend --format json --agent --targets-from <targetsPath>` using the returned `targetsPath`.
5. Inspect `detection`, `targets`, `availablePacks`, and `candidates`; choose explicit pack slugs that match the edited files. Do not invent pack slugs.
6. Fetch the selected packs with repeated `greprules fetch --pack <slug>` arguments. If no available pack fits the edited files, report that pack selection needs user input instead of running a broad fetch.
7. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
8. Run `greprules scan-edited`.
9. Read `.greprules/out/agent-result.json`.
10. Summarize findings by rule id, severity, file, line, and message.
11. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If no edited files are tracked, say that Claude Code has no tracked edited-file targets yet and suggest `/greprules:scan-working-tree` or `/greprules:scan-target <path>`.
- If edited-file tracking was disabled with `agent.trackEditedFiles=false` or `GREPRULES_TRACK_EDITED_FILES=false`, explain that `/greprules:scan-edited` has no source of targets in that session.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
