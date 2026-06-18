---
name: greprules-scan-edited
description: Scan files that Codex edited in the current session with greprules.
---

Scan files tracked by the plugin `PostToolUse` hook. This is the manual counterpart to optional Stop hook scans configured by `$greprules-configure`. A successful scan clears tracked state.

Workflow:

1. Resolve the plugin root and use bundled `bin/greprules`; shell `PATH` is optional.
2. Run `greprules agent-status --format json`.
3. If `opengrep.active.ok=false`, use `$greprules-configure` or ask one concise runtime question before scanning.
4. Run `python3 <plugin-root>/scripts/greprules-hook.py scan-edited`. The adapter owns session dirty state, writes absolute targets to `scan-targets.txt`, and invokes `greprules agent-scan scan --targets-from ... --output-dir ...`. Edited-file scans do not use git changed-file tracking.
5. If pack selection is needed, run the exact `greprules agent-scan recommend --format json --targets-from <targetsPath>` command shown, inspect `detection`, `targets`, `availablePacks`, and `candidates`, then fetch only explicit matching slugs with the shown `greprules fetch <slug>`. Do not invent pack slugs.
6. Rerun `python3 <plugin-root>/scripts/greprules-hook.py scan-edited` after fetching packs.
7. Read the `Full result:` path reported in the scan summary.
8. Summarize findings by rule id, severity, file, line, and message, then classify each as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, or create rule drafts unless the user explicitly asks.

Fallbacks:

- If no edited files are tracked, suggest `$greprules-scan-working-tree` or `$greprules-scan-target <path>`.
- If multiple Codex sessions have dirty files and no current session id is available, do not merge them; ask for a session-scoped scan or use `$greprules-scan-target <path>`.
- If edited-file tracking was disabled in Codex greprules settings, explain that this skill has no source of targets.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
