---
name: greprules-scan-target
description: Scan one or more explicit files or directories with greprules.
---

# greprules Scan Target

Use this skill when the user asks Codex to scan a specific file, directory, or target path with greprules.

This command requires at least one target path. If the user asks for a target scan without a path, ask for the file or directory instead of guessing.

Workflow:

1. Identify the target path or paths from the user's command. If none are provided, ask for the target.
2. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
3. Run `greprules agent-status --format json`.
4. If `opengrep.active.ok` is false, use `$greprules-setup` to prepare the managed OpenGrep runtime before scanning.
5. Run `greprules agent-scan scan <path> [<path>...]` for the requested paths.
6. If the scan returns `needs_pack_selection`, inspect `selectionContext.detection`, requested targets, `selectionContext.availablePacks`, and `selectionContext.candidates`; choose explicit pack slugs that match the target paths, fetch them with `greprules fetch <slug> [<slug>...]`, then rerun the scan. Do not invent pack slugs.
7. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
8. Read the `Full result:` path reported in the scan summary.
9. Summarize findings by rule id, severity, file, line, and message.
10. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If a target does not exist, report the missing path and ask for the corrected target.
- If there are many targets, pass them as positional paths rather than switching to a full scan unless the user asks.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
