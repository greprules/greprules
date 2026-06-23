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
$greprules-configure
$greprules-scan
$greprules-auth-login
```

The same skills can also be selected implicitly when the user asks Codex to check greprules readiness, configure greprules, log in to greprules.io, or run a greprules scan.

`$greprules-auth-login` signs the local greprules CLI in to greprules.io from the Codex conversation by showing the browser approval URL/code and waiting for approval. It does not upload feedback, proposals, source, or scan data.

Community contribution is conversation-driven instead of exposed as separate skill commands. After `$greprules-scan` reviews findings, Codex can offer to submit contextual true-positive, false-positive, warning, or diagnostic feedback. If Codex independently identifies a vulnerability that should become a reusable rule, the user can ask for a greprules.io rule proposal in normal chat. In both cases Codex prepares a redacted bundle, previews uploaded and excluded fields, runs `$greprules-auth-login` if needed, and submits only after the user approves the exact scope in conversation.

## Automatic Hooks

- `SessionStart` checks registry and OpenGrep readiness. Missing rule packs are not treated as readiness failures because scan commands can select and fetch packs from target context.
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

Edited-file tracking remains enabled by default, so `$greprules-scan` can still run a manual edited-file scan. A successful edited-file scan clears dirty state for that Codex session; readiness failures, pack-selection gaps, and too-many-target skips keep the dirty state for a later scan.

## CLI Resolution

Codex skills and hooks invoke the plugin-bundled `bin/greprules` wrapper. Codex does not add that wrapper to the user's shell `PATH`, so `command -v greprules` may be empty even when the plugin is installed correctly.

The bundled wrapper resolves the real CLI in this order:

1. `GREPRULES_CLI_PATH`
2. GitHub Release bootstrap into the greprules user cache
3. `greprules` on `PATH`, excluding the wrapper itself, only if managed bootstrap fails

The release bootstrap downloads the configured greprules CLI into the user cache so plugin behavior follows the plugin-pinned CLI version. `PATH` is a fallback, not the default runtime.

Plugin agent scans write results under `.greprules/plugin-data/codex/sessions/<session-id>/runs/<run-id>/agent-result.json`. Each scan run gets its own directory, so full, target, working-tree, and edited-file scans do not overwrite each other. Read the `Full result:` path printed by the scan summary. `$greprules-scan` chooses edited-file, git changed-file, explicit target, or full-repository scope from the user's request.
