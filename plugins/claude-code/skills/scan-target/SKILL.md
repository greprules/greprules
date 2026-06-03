---
description: Scan one or more explicit targets with greprules
---

Use this skill when the user asks for `/greprules:scan-target <path>` or names specific files or directories to scan.

This command requires at least one target path. If the user runs `/greprules:scan-target` without a path, ask for the file or directory instead of guessing.

Workflow:

1. Identify the target path or paths from the user's command. If none are provided, ask for the target.
2. Verify the CLI is available with `command -v greprules`.
3. Run `greprules doctor --format json`.
4. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
5. If `lock.exists` is false and `registry.ok` is true, run `greprules fetch`.
6. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
7. Run `greprules scan --target <path>` for each requested path.
8. Read `.greprules/out/agent-result.json`.
9. Summarize findings by rule id, severity, file, line, and message.
10. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If a target does not exist, report the missing path and ask for the corrected target.
- If there are many targets, pass repeated `--target` flags rather than switching to a full scan unless the user asks.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
