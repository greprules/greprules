---
name: greprules-scan-working-tree
description: Scan git working tree, staged, and untracked files with greprules.
---

# greprules Scan Working Tree

Use this skill when the user asks Codex to scan changed files, scan the current working tree, or run a greprules scan over git changes.

This command means `greprules agent-scan scan --changed`: files changed against `HEAD`, staged files, and untracked files. It does not mean "last commit".

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules agent-status --format json`.
3. If `opengrep.active.ok` is false, use `$greprules-setup` to prepare the managed OpenGrep runtime before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules agent-scan recommend --format json --changed`.
5. Inspect `detection`, `targets`, `availablePacks`, and `candidates`; choose explicit pack slugs that match the changed files. Do not invent pack slugs.
6. Fetch the selected packs with `greprules fetch <slug> [<slug>...]`. If no available pack fits the changed files, report that pack selection needs user input instead of running a broad fetch.
7. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
8. Run `greprules agent-scan scan --changed`.
9. Read `.greprules/out/agent-result.json`.
10. Summarize findings by rule id, severity, file, line, and message.
11. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If there are no working-tree targets, report that no changed files were found and suggest `$greprules-scan-full` for a repository-wide scan.
- If the current directory is not a git repository, ask whether to run `$greprules-scan-full` or `$greprules-scan-target <path>`.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
