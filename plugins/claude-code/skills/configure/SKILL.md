---
description: Check readiness, prepare OpenGrep, or change settings
---

Use this skill when the user has just installed greprules, asks Claude Code to set up or configure greprules, asks for current greprules status, or wants to change registry, default-rule, managed OpenGrep, or Claude Code hook settings.

Core rules:

- Use the plugin-bundled `bin/greprules` wrapper; shell `PATH` is optional.
- Run `greprules agent-status --format json` before decisions and after changes.
- Agent config owns shared registry and rule-scan options. Claude Code settings own hook behavior. Never store hooks in `greprules agent-config set agent.*`.
- Claude Code settings path: `~/.claude/plugins/greprules/settings.json` or `${CLAUDE_CONFIG_DIR}/plugins/greprules/settings.json`.
- greprules always uses its managed OpenGrep runtime. Do not configure `opengrep.mode` or `opengrep.path`.
- OpenGrep default rules and Stop hook scans are opt-in only.
- Treat a missing lockfile as rule-pack fetch state, not OpenGrep readiness failure.
- Treat a missing Claude Code settings file as normal default-state, not a warning or setup problem.
- First-run readiness is handled here. There is no separate public setup skill.

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
2. For status-only requests, summarize readiness first, then effective Claude Code settings. Use `source: defaults` when the settings file is absent. Do not call an absent settings file a warning or setup problem. If `lock.exists=false`, say packs have not been fetched for this workspace yet and will be fetched automatically when scanning if the registry is reachable.
3. For registry or `opengrep.includeDefaultRules`, use `greprules agent-config set <key> <value> --global` unless the user asked for repo-local config.
4. For hook settings, update only the Claude Code settings JSON, then rerun agent-status.
5. If OpenGrep is not ready, run `greprules setup-opengrep`.
6. Rerun agent-status and summarize registry/OpenGrep readiness plus Claude Code settings. Do not call missing packs a readiness failure.
