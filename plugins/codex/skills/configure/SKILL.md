---
name: greprules-configure
description: Inspect and configure greprules registry, OpenGrep runtime, and Codex hook behavior.
---

Configure or inspect greprules for Codex, including registry, OpenGrep runtime, and hook settings.

Core rules:

- Use the plugin-bundled `bin/greprules` wrapper; shell `PATH` is optional.
- Run `greprules agent-status --format json` before decisions and after changes.
- Agent config owns registry/OpenGrep runtime. Codex settings own hook behavior. Never store hooks in `greprules agent-config set agent.*`.
- Codex settings path: `${CODEX_HOME:-$HOME/.codex}/plugins/greprules/settings.json`.
- Do not write machine-specific `opengrep.path` to shared `.greprules/config.yaml`.
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

1. When the user asks about automatic edited-file scans or overall readiness, read `${CODEX_HOME:-$HOME/.codex}/config.toml`. Confirm `[plugins."greprules@greprules"] enabled = true` and `trusted_hash` values for `greprules@greprules:hooks/hooks.json:session_start:0:0`, `greprules@greprules:hooks/hooks.json:post_tool_use:0:0`, and `greprules@greprules:hooks/hooks.json:stop:0:0`. If trust is missing, say automatic scans will not run until the user trusts the hooks with `/hooks` and starts a new Codex session.
2. Run `greprules agent-status --format json`, then read Codex settings or use defaults.
3. For status-only requests, summarize `status`, registry, active OpenGrep runtime, rule-pack state, and Codex settings. If `lock.exists=false`, say only that packs have not been fetched yet.
4. If registry access fails, report URL and error; change registry only on request.
5. For registry or `opengrep.includeDefaultRules`, use `greprules agent-config set <key> <value> --global` unless the user asked for repo-local config.
6. For hook settings, update only the Codex settings JSON, then rerun agent-status.
7. For runtime changes, choose `system` when `opengrep.system.ok`, `managed` when system OpenGrep is unavailable or plugin-managed setup is preferred, and `path` only with a provided path.
8. Apply runtime with `greprules agent-config set ... --global`; for managed mode, also run `greprules setup-opengrep`.
9. Rerun agent-status and summarize registry, OpenGrep, rule-pack state, and Codex settings.
