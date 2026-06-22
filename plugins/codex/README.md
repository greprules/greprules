# greprules Codex Plugin

Codex plugin for greprules.io rule-pack scans. The plugin packages Codex skills plus lifecycle hooks that delegate deterministic rule-pack selection, fetch, and explicit target scans to the greprules Go CLI. Edited-file session state is owned by the Codex adapter.

## Install

Install the greprules marketplace from this repository:

```bash
codex plugin marketplace add greprules/greprules --sparse .agents/plugins --sparse plugins/codex
```

Then install or enable the greprules plugin from Codex's plugin directory. In the Codex desktop app, open **Plugins** from the app UI. In the Codex CLI TUI, start `codex` and run `/plugins`.

For local development from a checkout:

```bash
codex plugin marketplace add /absolute/path/to/greprules
```

Codex requires hook review before non-managed command hooks run. Review and trust the greprules hook entries if Codex prompts for hook review. In the Codex CLI TUI, use `/hooks`.

If automatic scans are enabled but edited-file scans do not run, use `$greprules-configure`. It checks whether the plugin is enabled and whether the `SessionStart`, `PostToolUse`, and `Stop` hook entries have been trusted in Codex. Missing hook trust means greprules is installed but the automatic scan flow is not active yet.

## Skills

Use `$` in the Codex composer to invoke a skill explicitly:

```text
$greprules-setup
$greprules-configure
$greprules-scan-edited
$greprules-scan-working-tree
$greprules-scan-target src/auth
$greprules-scan-full
$greprules-submit-feedback
$greprules-propose-rule
```

The same skills can also be selected implicitly when the user asks Codex to set up greprules, configure greprules, or run a greprules scan.

`$greprules-submit-feedback` is an explicit community contribution flow. It reviews a previous `agent-result.json`, prepares a redacted feedback bundle, previews the exact uploaded and excluded fields, and submits to greprules.io only after the user approves in conversation. It requires authenticated greprules.io access through `GREPRULES_API_KEY`.

`$greprules-propose-rule` is an explicit rule proposal flow. It prepares an agent-generated rule proposal bundle, requires license/provenance/generated metadata plus positive and negative public tests, previews uploaded and excluded fields, and submits only after user approval. It requires authenticated greprules.io access through `GREPRULES_API_KEY`.

## Automatic Hooks

- `SessionStart` checks registry and OpenGrep readiness. Missing rule packs are not treated as setup failures because scan commands can select and fetch packs from target context.
- `PostToolUse` tracks files Codex edited through `apply_patch`/file-edit tools under `.greprules/plugin-data/codex/sessions/<session-id>/`.
- `Stop` scans the tracked edited files for the current Codex session once when Codex greprules `autoScan=true`. The adapter passes absolute explicit targets; edited-file scans do not use git changed-file tracking. A clean scan is allowed to finish without a follow-up prompt. If findings, warnings, errors, or rule-pack selection work need attention, it continues Codex with instructions to keep the original development result as the primary response and append greprules review as a short secondary section.

If only some hooks are trusted, `SessionStart` can warn that automatic scans are not fully active. If `SessionStart` itself is not trusted, Codex will not run the warning hook; review hook trust in Codex and trust all greprules entries.

Automatic scans are disabled by default. Use `$greprules-configure` to update Codex greprules settings under `${CODEX_HOME:-~/.codex}/plugins/greprules/settings.json`:

```json
{
  "autoScan": true,
  "trackEditedFiles": true,
  "autoScanMinIntervalSeconds": 45,
  "autoScanMaxChangedFiles": 100
}
```

Edited-file tracking remains enabled by default, so `$greprules-scan-edited` can still be run manually. A successful edited-file scan clears dirty state for that Codex session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan.

## CLI Resolution

Codex skills and hooks invoke the plugin-bundled `bin/greprules` wrapper. Codex does not add that wrapper to the user's shell `PATH`, so `command -v greprules` may be empty even when the plugin is installed correctly.

The bundled wrapper resolves the real CLI in this order:

1. `GREPRULES_CLI_PATH`
2. `greprules` on `PATH`, excluding the wrapper itself
3. GitHub Release bootstrap into the greprules user cache

The release bootstrap downloads the configured greprules CLI into the user cache when a local binary is not available.

Plugin agent scans write results under `.greprules/plugin-data/codex/sessions/<session-id>/runs/<run-id>/agent-result.json`. Each scan run gets its own directory, so full, target, working-tree, and edited-file scans do not overwrite each other. Read the `Full result:` path printed by the scan summary. `$greprules-scan-working-tree` is the git-based changed-file scan path.
