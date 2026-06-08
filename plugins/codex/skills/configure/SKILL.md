---
name: greprules-configure
description: Inspect and configure greprules registry, OpenGrep runtime, and agent behavior for Codex, terminal, and CI parity.
---

# greprules Configure

Use this skill when the user asks Codex to configure greprules, choose an OpenGrep runtime, change the registry URL, inspect current greprules status, or inspect effective greprules settings.

Rules:

- Prefer `greprules config set` over editing config files directly.
- When running from the Codex plugin, execute the plugin-bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this file, `greprules ...` means the resolved wrapper command.
- Use `greprules doctor --format json` as the current-state diagnostic before deciding what to change.
- Persist OpenGrep runtime selection with `--global` unless the user asks for repository-local settings.
- Do not write machine-specific `opengrep.path` to shared `.greprules/config.yaml`.
- OpenGrep default rules are disabled by default with `opengrep.includeDefaultRules=false`; only enable them if the user explicitly asks to include OpenGrep's default auto-selected rules.
- Automatic Stop hook scans are disabled by default with `agent.autoScan=false`; enable them only when the user asks for automatic scan-on-edit behavior.
- `opengrep.includeDefaultRules` and `agent.autoScan` both accept `true|false`; use `true` to opt in and `false` to opt out again.
- Persist repeated agent behavior preferences with `agent.*` config keys. Use environment variables only for one-session overrides.

Common commands:

```bash
greprules config set registry https://api.greprules.io --global
greprules config set opengrep.mode system --global
greprules config set opengrep.mode managed --global
greprules config set opengrep.mode path --global
greprules config set opengrep.path /absolute/path/to/opengrep --global
greprules config set opengrep.includeDefaultRules true --global
greprules config set agent.autoScan true --global
greprules config set agent.trackEditedFiles false --global
greprules config set agent.autoScanMinIntervalSeconds 45 --global
greprules config set agent.autoScanMaxChangedFiles 100 --global
greprules doctor --format json
greprules config inspect --format json
```

Workflow:

1. Check Codex plugin and hook trust state when the user asks about automatic edited-file scans or overall readiness:
   - Read `${CODEX_HOME:-$HOME/.codex}/config.toml`.
   - Confirm `[plugins."greprules@greprules"] enabled = true`.
   - Confirm these hook state entries have `trusted_hash` values:
     - `greprules@greprules:hooks/hooks.json:session_start:0:0`
     - `greprules@greprules:hooks/hooks.json:post_tool_use:0:0`
     - `greprules@greprules:hooks/hooks.json:stop:0:0`
   - If the plugin is enabled but any trusted hook entry is missing, tell the user that greprules is installed but automatic edited-file scans will not run yet. Ask them to open `/hooks`, trust the greprules hook entries, and start a new Codex session.
2. Run `greprules doctor --format json`.
3. If the user asked only for current status or settings, summarize `status`, registry readiness, active OpenGrep runtime, rule-pack fetch state, and effective `agent.*` settings, then stop.
4. If registry access fails, report the registry URL and error. Only change registry if the user asked for a different registry.
5. If the user asked to change a non-runtime setting, apply the smallest `greprules config set ... --global` change, then rerun `greprules doctor --format json` and summarize the updated state.
6. If setup or a runtime change is needed, choose the smallest suitable option from the doctor report:
   - `system` when `opengrep.system.ok` is true.
   - `managed` when system OpenGrep is unavailable or the user prefers plugin-managed setup.
   - `path` only when the user provides a path.
7. Apply the selected setting with `greprules config set ... --global`.
8. For managed mode, run `greprules setup-opengrep`.
9. Run `greprules doctor --format json` again and summarize registry, OpenGrep, rule-pack state, and effective agent settings.
