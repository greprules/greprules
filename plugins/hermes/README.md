# greprules Hermes Plugin

Hermes plugin for greprules.io rule-pack scans. The plugin follows Hermes' `plugin.yaml` + `__init__.py` plugin layout and delegates scanning to the greprules Go CLI.

## Install

Copy or install this directory as a Hermes plugin, then enable it:

```bash
mkdir -p ~/.hermes/plugins
cp -R plugins/hermes ~/.hermes/plugins/greprules
hermes plugins enable greprules
```

Hermes general plugins are opt-in. Project-local plugins under `.hermes/plugins/` require `HERMES_ENABLE_PROJECT_PLUGINS=true`.

## CLI Runtime

The plugin resolves the greprules CLI in this order:

1. `GREPRULES_CLI_PATH`
2. bundled `bin/greprules` wrapper
3. `greprules` on `PATH`

The bundled wrapper can download the configured greprules release into a user cache when a local binary is not available.

## Slash Commands

```text
/greprules doctor
/greprules configure managed
/greprules configure system
/greprules configure path /absolute/path/to/opengrep
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
- `pre_llm_call` scans tracked edited files before the next model turn and injects a compact result summary as context.

Environment controls:

```bash
export GREPRULES_HERMES_AUTO_SCAN=false
export GREPRULES_HERMES_TRACK_EDITED_FILES=false
export GREPRULES_HERMES_AUTO_SCAN_MIN_INTERVAL_SECONDS=45
export GREPRULES_HERMES_AUTO_SCAN_MAX_CHANGED_FILES=100
```
