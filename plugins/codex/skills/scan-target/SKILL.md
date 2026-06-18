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
4. If `opengrep.active.ok` is false, use `$greprules-configure` or ask one concise question about the OpenGrep runtime choice before scanning.
5. If `lock.exists` is false and `registry.ok` is true, run `greprules agent-scan recommend --format json <path>` for each requested path.
6. Inspect `detection`, requested targets, `availablePacks`, and `candidates`; choose explicit pack slugs that match the target paths. Do not invent pack slugs.
7. Fetch the selected packs with `greprules fetch <slug> [<slug>...]`. If no available pack fits the target, report that pack selection needs user input instead of running a broad fetch.
8. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
9. Run `greprules agent-scan scan <path> [<path>...]` for the requested paths.
10. Read `.greprules/out/agent-result.json`.
11. Summarize findings by rule id, severity, file, line, and message.
12. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If a target does not exist, report the missing path and ask for the corrected target.
- If there are many targets, pass them as positional paths rather than switching to a full scan unless the user asks.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
