---
description: Scan files that Claude Code edited in the current session with greprules
---

Use this skill when the user asks for `/greprules:scan-edited` or wants to manually scan files Claude Code edited.

This command uses the file list tracked by the plugin `PostToolUse` hook. It is the manual counterpart to the optional automatic Stop hook scan. Automatic Stop hook scans are disabled by default; `greprules config set agent.autoScan true --global` enables the Stop hook scan and block behavior while keeping this command available. A successful edited-file scan clears the tracked state. `GREPRULES_AUTO_SCAN=true` remains available as a one-session override.

Workflow:

1. Resolve the installed plugin root and use its bundled `bin/greprules` wrapper for CLI checks. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. Run the adapter manual scan mode: `python3 <plugin-root>/scripts/greprules-hook.py scan-edited`. The adapter owns session dirty state, writes absolute targets to `scan-targets.txt`, and invokes `greprules agent-scan scan --targets-from ... --output-dir ...`. Edited-file scans do not use git changed-file tracking.
5. If the adapter reports that rule-pack selection is needed, run the exact `greprules recommend --format json --agent --targets-from <targetsPath>` command shown in the message, inspect `detection`, `targets`, `availablePacks`, and `candidates`, then fetch only explicit matching slugs with the exact `greprules fetch --pack <slug>` command shown in the message. Do not invent pack slugs.
6. Rerun `python3 <plugin-root>/scripts/greprules-hook.py scan-edited` after fetching packs.
7. Read the `Full result:` path reported in the scan summary.
8. Summarize findings by rule id, severity, file, line, and message.
9. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If no edited files are tracked, say that Claude Code has no tracked edited-file targets yet and suggest `/greprules:scan-working-tree` or `/greprules:scan-target <path>`.
- If multiple Claude sessions have dirty files and no current session id is available, do not merge them; ask the user to run a session-scoped scan or use `/greprules:scan-target <path>`.
- If edited-file tracking was disabled with `agent.trackEditedFiles=false` or `GREPRULES_TRACK_EDITED_FILES=false`, explain that `/greprules:scan-edited` has no source of targets in that session.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
