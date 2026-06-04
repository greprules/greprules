---
name: greprules-scan-full
description: Scan the full repository with greprules.
---

# greprules Scan Full

Use this skill when the user asks Codex to scan the full repository or run a repository-wide greprules scan.

Full scans can be slower than edited-file or target scans. Prefer `$greprules-scan-edited`, `$greprules-scan-working-tree`, or `$greprules-scan-target <path>` when the user asks for a focused scan.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, use `$greprules-configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules recommend --format json --agent`.
5. Inspect `detection`, repository context, `availablePacks`, and `candidates`; choose explicit pack slugs that match the repository. Do not invent pack slugs.
6. Fetch the selected packs with repeated `greprules fetch --pack <slug>` arguments. If no available pack fits the repository, report that pack selection needs user input instead of running a broad fetch.
7. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
8. Run `greprules scan --full`.
9. Read `.greprules/out/agent-result.json`.
10. Summarize findings by rule id, severity, file, line, and message.
11. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If the scan is too slow or the user narrows scope, stop the full scan and use `$greprules-scan-target <path>` or `$greprules-scan-working-tree`.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
