---
name: greprules-configure
description: Check readiness, prepare OpenGrep, or change settings.
---

Use this skill when the user has just installed greprules, asks Codex to set up or configure greprules, asks for current greprules status, or wants to change registry, default-rule, managed OpenGrep, or Codex hook settings.

Core rules:

- Use the plugin-bundled `bin/greprules` wrapper; shell `PATH` is optional.
- Run `greprules agent-status --format json` before decisions and after changes.
- Agent config owns shared registry and rule-scan options. Codex settings own hook behavior. Never store hooks in `greprules agent-config set agent.*`.
- Codex settings path: `${CODEX_HOME:-$HOME/.codex}/plugins/greprules/settings.json`.
- greprules always uses its managed OpenGrep runtime. Do not configure `opengrep.mode` or `opengrep.path`.
- OpenGrep default rules and Stop hook scans are opt-in only.
- Treat a missing lockfile as rule-pack fetch state, not OpenGrep readiness failure.
- Treat a missing Codex settings file as normal default-state, not a warning or setup problem.
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

1. When the user asks about automatic edited-file scans or overall readiness, read `${CODEX_HOME:-$HOME/.codex}/config.toml`. Confirm `[plugins."greprules@greprules"] enabled = true` and `trusted_hash` values for `greprules@greprules:hooks/hooks.json:session_start:0:0`, `greprules@greprules:hooks/hooks.json:post_tool_use:0:0`, and `greprules@greprules:hooks/hooks.json:stop:0:0`. If trust is missing, say automatic scans will not run until the user trusts the hooks with `/hooks` and starts a new Codex session.
2. Run `greprules agent-status --format json`, then read Codex settings or use defaults.
3. For status-only requests, answer in this order:
   - Say no changes were made.
   - Start with a concise summary: whether manual `greprules scan` is ready, and whether Codex Stop hook automatic scans are enabled.
   - List readiness state: registry, managed OpenGrep runtime, Codex plugin enabled state, hook trust state, workspace trust state, and rule-pack state.
   - List effective Codex settings with a `source` value of `defaults` when the settings file is absent, or `settings.json` when it is present.
   - List only relevant next actions the user can take, such as enabling Stop hook auto scan, enabling OpenGrep default rules, or running a scan.
   Do not lead with settings file paths. If the settings file is absent, write "Codex settings: using defaults" or `source: defaults`; do not call it missing, warn about it, or say it must be created unless the user wants to change settings.
   If `lock.exists=false`, say packs have not been fetched for this workspace yet and will be fetched automatically when scanning if the registry is reachable.
4. If registry access fails, report URL and error; change registry only on request.
5. For registry or `opengrep.includeDefaultRules`, use `greprules agent-config set <key> <value> --global` unless the user asked for repo-local config.
6. For hook settings, update only the Codex settings JSON, then rerun agent-status.
7. If OpenGrep is not ready, run `greprules setup-opengrep`.
8. Rerun agent-status and summarize registry, OpenGrep, rule-pack state, and Codex settings.

Preferred status-only response shape:

```text
greprules status checked. No changes were made.

Summary:
- Manual scan: ready
- Codex Stop hook auto scan: disabled by default

State:
- registry: ok, https://api.greprules.io
- OpenGrep: managed, <version>
- Codex plugin: enabled
- hooks: trusted
- workspace: trusted
- rule packs: not fetched for this workspace yet

Codex settings:
- source: defaults
- autoScan: false
- trackEditedFiles: true
- autoScanMinIntervalSeconds: 45
- autoScanMaxChangedFiles: 100

Available actions:
- Enable automatic Stop hook scan: set autoScan=true
- Include OpenGrep default rules: set opengrep.includeDefaultRules=true
- Run a manual scan: greprules scan .
```
