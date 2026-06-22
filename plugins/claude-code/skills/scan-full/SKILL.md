---
description: Scan the full repository with greprules
---

Use this skill when the user asks for `/greprules:scan-full` or explicitly wants a repository-wide greprules scan.

Full scans can be slower than edited-file or working-tree scans. Run them when the user asks for broad coverage, not as the default follow-up to a small edit.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules agent-status --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:setup` to prepare the managed OpenGrep runtime before scanning.
4. Run `greprules agent-scan scan`.
5. If the scan returns `needs_pack_selection`, inspect `selectionContext.detection`, repository context, `selectionContext.availablePacks`, and `selectionContext.candidates`; choose explicit pack slugs that match the full repository, fetch them with `greprules fetch <slug> [<slug>...]`, then rerun `greprules agent-scan scan`. Do not invent pack slugs.
6. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
7. Read the `Full result:` path reported in the scan summary.
8. Summarize findings by rule id, severity, file, line, and message.
9. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If the scan is too slow or the user narrows scope, stop the full scan and use `/greprules:scan-target <path>` or `/greprules:scan-working-tree`.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
