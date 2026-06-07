# greprules Codex Plugin

Codex plugin for greprules.io rule-pack scans. The plugin packages Codex skills plus lifecycle hooks that delegate deterministic rule-pack selection, fetch, and explicit target scans to the greprules Go CLI. Edited-file session state is owned by the Codex adapter.

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

If automatic scans are enabled but edited-file scans do not run, use `$greprules-doctor`. It checks whether the plugin is enabled and whether the `SessionStart`, `PostToolUse`, and `Stop` hook entries have been trusted in Codex. Missing hook trust means greprules is installed but the automatic scan flow is not active yet.

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
- `PostToolUse` tracks files Codex edited through `apply_patch`/file-edit tools under `.greprules/plugin-data/codex/sessions/<session-id>/`.
- `Stop` scans the tracked edited files for the current Codex session once when `agent.autoScan=true`. The adapter passes absolute explicit targets; edited-file scans do not use git changed-file tracking. A clean scan is allowed to finish without a follow-up prompt. If findings, warnings, errors, or rule-pack selection work need attention, it continues Codex with instructions to keep the original development result as the primary response and append greprules review as a short secondary section.

If only some hooks are trusted, `SessionStart` can warn that automatic scans are not fully active. If `SessionStart` itself is not trusted, Codex will not run the warning hook; open `/hooks` and trust all greprules entries.

Automatic scans are disabled by default. Enable the automatic Stop hook scan persistently:

```bash
greprules config set agent.autoScan true --global
```

Edited-file tracking remains enabled by default, so `$greprules-scan-edited` can still be run manually. A successful edited-file scan clears dirty state for that Codex session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan. To disable tracking as well:

```bash
greprules config set agent.trackEditedFiles false --global
```

For a one-session override, set `GREPRULES_AUTO_SCAN=true` or `GREPRULES_TRACK_EDITED_FILES=false` before starting Codex.

## CLI Resolution

Codex skills and hooks invoke the plugin-bundled `bin/greprules` wrapper. Codex does not add that wrapper to the user's shell `PATH`, so `command -v greprules` may be empty even when the plugin is installed correctly.

The bundled wrapper resolves the real CLI in this order:

1. `GREPRULES_CLI_PATH`
2. `greprules` on `PATH`, excluding the wrapper itself
3. GitHub Release bootstrap into the greprules user cache

The release bootstrap downloads the configured greprules CLI into the user cache when a local binary is not available.

Edited-file plugin scans write session-local results under `.greprules/plugin-data/codex/sessions/<session-id>/out/agent-result.json`. Normal working-tree, target, and full scans use the CLI output directory, usually `.greprules/out/agent-result.json`. `$greprules-scan-working-tree` is the git-based changed-file scan path.
