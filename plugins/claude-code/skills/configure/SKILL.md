---
description: Inspect and configure greprules registry, OpenGrep runtime, and Claude Code hook behavior
---

Use this skill when the user asks to configure greprules for Claude Code or local agent use, inspect current greprules status, or change existing greprules settings.

Core rules:

- Use the plugin-bundled `bin/greprules` wrapper; shell `PATH` is optional.
- Run `greprules agent-status --format json` before decisions and after changes.
- Agent config owns registry/OpenGrep runtime. Claude Code settings own hook behavior. Never store hooks in `greprules agent-config set agent.*`.
- Claude Code settings path: `~/.claude/plugins/greprules/settings.json` or `${CLAUDE_CONFIG_DIR}/plugins/greprules/settings.json`.
- Do not configure OpenGrep through Claude Code plugin options or write machine `opengrep.path` to shared `.greprules/config.yaml`.
- OpenGrep default rules and Stop hook scans are opt-in only.
- Treat a missing lockfile as rule-pack fetch state, not OpenGrep setup failure.

Settings JSON:

```json
{
  "autoScan": false,
  "trackEditedFiles": true,
  "autoScanMinIntervalSeconds": 45,
  "autoScanMaxChangedFiles": 100
}
```

Workflow:

1. Run `greprules agent-status --format json`, then read Claude Code settings or use defaults.
2. For status-only requests, summarize `status`, registry, active OpenGrep runtime, rule-pack state, and Claude Code settings. If `lock.exists=false`, say only that packs have not been fetched yet.
3. For registry or `opengrep.includeDefaultRules`, use `greprules agent-config set <key> <value> --global` unless the user asked for repo-local config.
4. For hook settings, update only the Claude Code settings JSON, then rerun agent-status.
5. For runtime changes, inspect `opengrep.system.ok`, `opengrep.system.runtime.path`, and `opengrep.system.runtime.version`. If no runtime was chosen, ask for `system`, `managed`, or `path`; recommend `system` when available, otherwise `managed`. `path` needs an absolute path.
6. Apply runtime:
   - `system`: `greprules agent-config set opengrep.mode system --global`
   - `managed`: `greprules agent-config set opengrep.mode managed --global`, then `greprules setup-opengrep`
   - `path`: verify that `<path>` is an executable file, set `opengrep.mode=path` and `opengrep.path=<path>` globally, then rerun `greprules agent-status --format json`.
7. Rerun agent-status and summarize registry/OpenGrep readiness plus Claude Code settings. Do not call missing packs a setup failure.
