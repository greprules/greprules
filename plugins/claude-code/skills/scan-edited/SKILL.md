---
description: Scan files that Claude Code edited in the current session with greprules
---

Use this skill when the user asks for `/greprules:scan-edited` or wants to manually scan files Claude Code edited.

This command uses the file list tracked by the plugin `PostToolUse` hook. It is the manual counterpart to the automatic Stop hook scan. `GREPRULES_AUTO_SCAN=false` disables the Stop hook scan and block behavior, but edited-file tracking remains enabled so this command can still be used. A successful edited-file scan clears the tracked state.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules fetch`.
5. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
6. Run `greprules scan-edited`.
7. Read `.greprules/out/agent-result.json`.
8. Summarize findings by rule id, severity, file, line, and message.
9. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If no edited files are tracked, say that Claude Code has no tracked edited-file targets yet and suggest `/greprules:scan-working-tree` or `/greprules:scan-target <path>`.
- If edited-file tracking was disabled with `GREPRULES_TRACK_EDITED_FILES=false`, explain that `/greprules:scan-edited` has no source of targets in that session.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
