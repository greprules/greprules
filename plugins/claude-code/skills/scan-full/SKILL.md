---
description: Scan the full repository with greprules
---

Use this skill when the user asks for `/greprules:scan-full` or explicitly wants a repository-wide greprules scan.

Full scans can be slower than edited-file or working-tree scans. Run them when the user asks for broad coverage, not as the default follow-up to a small edit.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` or ask one concise question about the OpenGrep runtime choice before scanning.
4. If `lock.exists` is false and `registry.ok` is true, run `greprules fetch`.
5. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
6. Run `greprules scan --full`.
7. Read `.greprules/out/agent-result.json`.
8. Summarize findings by rule id, severity, file, line, and message.
9. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If the scan is too slow or the user narrows scope, stop the full scan and use `/greprules:scan-target <path>` or `/greprules:scan-working-tree`.
- If registry access fails, report the registry URL and the error from `doctor --format json`.
