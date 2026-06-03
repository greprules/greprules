---
description: Scan git working tree, staged, and untracked files with greprules
---

Use this skill when the user asks for `/greprules:scan-working-tree` or wants to scan files changed in the current working tree.

This command means `greprules scan --changed`: files changed against `HEAD`, staged files, and untracked files. It does not mean "last commit".

When handling an automatic Stop hook result, read `.greprules/out/agent-result.json` and any relevant project context needed to classify each finding, then report reasoning only. Do not edit code, add suppressions, chase zero findings, or rerun greprules unless the user explicitly asks.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules fetch`.
5. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
6. Run `greprules scan --changed`.
7. Read `.greprules/out/agent-result.json`.
8. Summarize findings by rule id, severity, file, line, and message.
9. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If there are no working-tree targets, report that no changed files were found and suggest `/greprules:scan-full` for a repository-wide scan.
- If the current directory is not a git repository, ask whether to run `/greprules:scan-full` or `/greprules:scan-target <path>`.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
