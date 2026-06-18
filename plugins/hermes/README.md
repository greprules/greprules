# greprules Hermes Plugin

Hermes plugin for greprules.io rule-pack scans. The plugin follows Hermes' `plugin.yaml` + `__init__.py` plugin layout and delegates deterministic fetch and explicit target scans to the greprules Go CLI. Edited-file session state is owned by the Hermes adapter.

## Install

Install the plugin from the greprules repository:

```bash
hermes plugins install greprules/greprules --enable
```

The repository root contains a small Hermes shim so `hermes plugins install` can clone the monorepo directly. The implementation in this directory remains the source of the Hermes adapter. Hermes general plugins are opt-in. Project-local plugins under `.hermes/plugins/` require `HERMES_ENABLE_PROJECT_PLUGINS=true`.

## CLI Runtime

Hermes slash commands and hooks invoke the plugin-bundled `bin/greprules` wrapper by default. The wrapper is not expected to be on the user's shell `PATH`.

The Hermes adapter resolves the command in this order:

1. `GREPRULES_CLI_PATH` as an explicit local override
2. bundled `bin/greprules` wrapper
3. `greprules` on `PATH` only as a fallback

The bundled wrapper can download the configured greprules release into a user cache when a local binary is not available.

## Slash Commands

```text
/greprules setup
/greprules configure registry https://api.greprules.io
/greprules configure
/greprules configure managed
/greprules configure system
/greprules configure path /absolute/path/to/opengrep
/greprules configure include-default-rules true
/greprules configure auto-scan true
/greprules configure track-edited-files false
/greprules configure auto-scan-min-interval 45
/greprules configure auto-scan-max-changed-files 100
/greprules fetch python-security
/greprules scan-edited
/greprules scan-working-tree
/greprules scan-target src/auth
/greprules scan-full
```

Aliases:

```text
/greprules-scan-edited
/greprules-scan-working-tree
/greprules-scan-target src/auth
/greprules-scan-full
```

## Hooks

- `post_tool_call` tracks files edited through Hermes file tools under `.greprules/plugin-data/hermes/sessions/<session-or-task-id>/`.
- `pre_llm_call` scans tracked edited files for the current Hermes session before the next model turn when Hermes greprules `autoScan=true` and injects a compact result summary as context. The adapter passes absolute explicit targets; edited-file scans do not use git changed-file tracking. If rule-pack selection is ambiguous, it returns instructions to inspect available packs and fetch explicit slugs before rerunning the scan.

Edited-file plugin scans write session-local results under `.greprules/plugin-data/hermes/sessions/<session-or-task-id>/out/agent-result.json`. Agent working-tree, target, and full scans use the CLI output directory, usually `.greprules/out/agent-result.json`. `/greprules scan-working-tree` is the git-based changed-file scan path.

A successful edited-file scan clears dirty state for that Hermes session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan.

Persistent controls:

```text
/greprules configure auto-scan true
/greprules configure track-edited-files false
/greprules configure auto-scan-min-interval 45
/greprules configure auto-scan-max-changed-files 100
```
