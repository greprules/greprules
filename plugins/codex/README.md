# greprules Codex Plugin

Codex plugin for greprules.io rule-pack scans. The plugin packages Codex skills plus lifecycle hooks that delegate deterministic rule-pack selection, fetch, scan, and edited-file state to the greprules Go CLI.

## Install

Install the greprules marketplace from this repository:

```bash
codex plugin marketplace add greprules/greprules --sparse .agents/plugins --sparse plugins/codex
```

Then open `/plugins` in Codex, choose the greprules marketplace, and install or enable the greprules plugin.

For local development from a checkout:

```bash
codex plugin marketplace add /absolute/path/to/greprules
```

Codex requires hook review before non-managed command hooks run. Open `/hooks` after enabling the plugin and trust the greprules hook entries.

## Skills

Use `$` in the Codex composer to invoke a skill explicitly:

```text
$greprules-doctor
$greprules-configure
$greprules-scan-edited
$greprules-scan-working-tree
$greprules-scan-target src/auth
$greprules-scan-full
```

The same skills can also be selected implicitly when the user asks Codex to configure greprules or run a greprules scan.

## Automatic Hooks

- `SessionStart` checks registry and OpenGrep readiness. Missing rule packs are not treated as setup failures because scan commands can select and fetch packs from target context.
- `PostToolUse` tracks files Codex edited through `apply_patch`/file-edit tools.
- `Stop` scans the tracked edited files once, then continues Codex with a compact prompt to review `.greprules/out/agent-result.json`.

Set this before starting Codex to disable the automatic Stop hook scan for a session:

```bash
export GREPRULES_AUTO_SCAN=false
```

Edited-file tracking remains enabled, so `$greprules-scan-edited` can still be run manually. To disable tracking as well:

```bash
export GREPRULES_TRACK_EDITED_FILES=false
```

## CLI Resolution

The plugin resolves the greprules CLI in this order:

1. `GREPRULES_CLI_PATH`
2. bundled `bin/greprules` wrapper
3. `greprules` on `PATH`

The bundled wrapper can download the configured greprules release into the user cache when a local binary is not available.
