---
description: Scan the full repository with greprules
---

Use this skill when the user asks for `/greprules:scan-full` or explicitly wants a repository-wide greprules scan.

Full scans can be slower than edited-file or working-tree scans. Run them when the user asks for broad coverage, not as the default follow-up to a small edit.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules agent-status --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules agent-scan recommend --format json`.
5. Inspect `detection`, repository context, `availablePacks`, and `candidates`; choose explicit pack slugs that match the full repository. Do not invent pack slugs.
6. Fetch the selected packs with `greprules fetch <slug> [<slug>...]`. If no available pack fits the repository, report that pack selection needs user input instead of running a broad fetch.
7. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
8. Run `greprules agent-scan scan`.
9. Read `.greprules/out/agent-result.json`.
10. Summarize findings by rule id, severity, file, line, and message.
11. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If the scan is too slow or the user narrows scope, stop the full scan and use `/greprules:scan-target <path>` or `/greprules:scan-working-tree`.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
