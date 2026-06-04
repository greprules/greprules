# greprules Hermes Plugin

Hermes plugin for greprules.io rule-pack scans. The plugin follows Hermes' `plugin.yaml` + `__init__.py` plugin layout and delegates deterministic fetch, scan, and state operations to the greprules Go CLI.

## Install

Install the plugin from the greprules repository:

```bash
hermes plugins install greprules/greprules --enable
```

The repository root contains a small Hermes shim so `hermes plugins install` can clone the monorepo directly. The implementation in this directory remains the source of the Hermes adapter. Hermes general plugins are opt-in. Project-local plugins under `.hermes/plugins/` require `HERMES_ENABLE_PROJECT_PLUGINS=true`.

## CLI Runtime

The plugin resolves the greprules CLI in this order:

1. `GREPRULES_CLI_PATH`
2. bundled `bin/greprules` wrapper
3. `greprules` on `PATH`

The bundled wrapper can download the configured greprules release into a user cache when a local binary is not available.

## Slash Commands

```text
/greprules doctor
/greprules configure registry https://api.greprules.io
/greprules configure managed
/greprules configure system
/greprules configure path /absolute/path/to/opengrep
/greprules configure include-default-rules true
/greprules configure auto-scan true
/greprules configure track-edited-files false
/greprules configure auto-scan-min-interval 45
/greprules configure auto-scan-max-changed-files 100
/greprules fetch
/greprules scan-edited
/greprules scan-working-tree
/greprules scan-target src/auth
/greprules scan-full
```

Aliases:

```text
/greprules-doctor
/greprules-scan-edited
/greprules-scan-working-tree
/greprules-scan-target src/auth
/greprules-scan-full
```

## Hooks

- `post_tool_call` tracks files edited through Hermes file tools.
- `pre_llm_call` scans tracked edited files before the next model turn when `agent.autoScan=true` and injects a compact result summary as context. If rule-pack selection is ambiguous, it returns instructions to inspect available packs and fetch explicit slugs before rerunning the scan.

Persistent controls:

```bash
greprules config set agent.autoScan true --global
greprules config set agent.trackEditedFiles false --global
greprules config set agent.autoScanMinIntervalSeconds 45 --global
greprules config set agent.autoScanMaxChangedFiles 100 --global
```

One-session overrides:

```bash
export GREPRULES_HERMES_AUTO_SCAN=true
export GREPRULES_HERMES_TRACK_EDITED_FILES=false
export GREPRULES_HERMES_AUTO_SCAN_MIN_INTERVAL_SECONDS=45
export GREPRULES_HERMES_AUTO_SCAN_MAX_CHANGED_FILES=100
```
