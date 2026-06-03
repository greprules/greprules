# greprules

Agent plugin for fetching trusted SAST rule packs from greprules.io and scanning local code changes with OpenGrep.

greprules is designed for local coding agents first. The Claude Code and Hermes plugins give agents slash commands for checking setup, configuring OpenGrep, selecting rule packs from code context, fetching those packs, and scanning local code changes. The Go CLI is the deterministic local runtime behind those commands.

## Quick Start

Run these inside Claude Code:

```text
/plugin marketplace add greprules/greprules
/plugin install greprules@greprules
/reload-plugins

/greprules:doctor
/greprules:configure
/greprules:scan-edited
```

`/greprules:doctor` is the best first command. It checks registry access, OpenGrep runtime readiness, and local rule-pack fetch state. A missing `.greprules/lock.json` means rule packs have not been fetched yet; scan commands select packs from target context before fetching when the registry is reachable.

## What It Does

- Lets agents select reusable SAST rule packs from greprules.io based on target code context.
- Fetches the selected packs reproducibly through the CLI.
- Configures OpenGrep for local scans.
- Tracks files edited by Claude Code.
- Scans changed files or explicit targets before the agent finishes.
- Writes agent-readable results so Claude can review findings and suggest fixes.
- Keeps source code local; the plugin fetches rules and runs OpenGrep on your machine.

## Claude Code Slash Commands

All user-facing plugin commands are prefixed with `/greprules:`.

| Command | Use when |
| --- | --- |
| `/greprules:doctor` | You want to check whether greprules is ready in the current repo. |
| `/greprules:configure` | OpenGrep needs to be installed, selected, or switched between managed/system/path modes. |
| `/greprules:scan-edited` | You want to scan files Claude Code edited in the current session. |
| `/greprules:scan-working-tree` | You want to scan git working tree, staged, and untracked files. |
| `/greprules:scan-target <path>` | You want to scan explicit files or directories. A path is required. |
| `/greprules:scan-full` | You want to scan the full repository. |

Common examples:

```text
/greprules:doctor
/greprules:configure
/greprules:scan-edited
/greprules:scan-working-tree
/greprules:scan-target src/auth
/greprules:scan-full
```

## Typical Workflow

1. Open a repository in Claude Code.
2. Run `/greprules:doctor`.
3. If setup is needed, run `/greprules:configure`.
4. Let Claude edit code as usual.
5. Run `/greprules:scan-edited`, or let the plugin's Stop hook scan the files Claude edited.
6. Claude reads the scan result, separates likely true positives from noise, and proposes fixes.

The plugin does not require install-time configuration. Runtime choices are stored in greprules config so Claude Code, terminals, and CI can share the same behavior.

## Automatic Scan Hooks

The Claude Code plugin includes lightweight hooks for agent editing sessions:

- `SessionStart` checks registry and OpenGrep readiness. Missing rule packs are not reported as setup gaps because scan commands can select and fetch packs from target context.
- `PostToolUse` records files Claude edited with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`.
- `Stop` scans the edited files once, then asks Claude to review the result before finishing.

Set this before starting Claude Code to disable automatic scans for a session:

```bash
export GREPRULES_AUTO_SCAN=false
```

This disables the Stop hook scan and block. Edited-file tracking stays enabled, so you can still run `/greprules:scan-edited` manually; a successful manual edited-file scan clears the tracked state. To disable tracking as well:

```bash
export GREPRULES_TRACK_EDITED_FILES=false
```

## Hermes Plugin

The Hermes plugin lives in `plugins/hermes` and follows Hermes' standard `plugin.yaml` plus `__init__.py` layout. Install or copy it into `~/.hermes/plugins/greprules`, then enable it:

```bash
hermes plugins enable greprules
```

Hermes slash commands:

```text
/greprules doctor
/greprules configure managed
/greprules scan-edited
/greprules scan-working-tree
/greprules scan-target src/auth
/greprules scan-full
```

The plugin tracks edited files with `post_tool_call` and can inject compact edited-file scan results before the next model turn with `pre_llm_call`. Set `GREPRULES_HERMES_AUTO_SCAN=false` to disable automatic context injection while keeping manual commands available.

## OpenGrep Runtime

OpenGrep does the actual scanning. greprules keeps runtime selection explicit so scans are reproducible and easy to debug.

| Mode | Use when |
| --- | --- |
| `managed` | You want greprules to install and use a managed OpenGrep binary. This is the default. |
| `system` | You already have `opengrep` on `PATH` and want to use it. |
| `path` | You want to point greprules at a specific OpenGrep executable. |

Use `/greprules:configure` to choose a runtime from Claude Code. From a shell, the same settings are available through:

```bash
greprules config set opengrep.mode system --global
greprules config set opengrep.mode managed --global
greprules config set opengrep.mode path --global
greprules config set opengrep.path /absolute/path/to/opengrep --global
```

By default, greprules scans fetched greprules.io packs together with OpenGrep's default auto-selected rules. To scan only greprules.io packs:

```bash
greprules config set opengrep.includeDefaultRules false --global
```

## Results and Local Files

The important files are:

```text
.greprules/config.yaml
.greprules/lock.json
.greprules/out/agent-result.json
.greprules/out/scan.sarif
```

Claude reads `.greprules/out/agent-result.json`. It contains the scan summary, findings, warnings, selected OpenGrep runtime, and rule pack metadata. `.greprules/lock.json` pins fetched pack artifacts and records the selected scan runtime.

Generated local paths are ignored automatically in git repositories:

```text
.greprules/cache/
.greprules/out/
.greprules/plugin-data/
.greprules/config.local.json
```

Shared files such as `.greprules/config.yaml` and `.greprules/lock.json` are not ignored automatically.

## Standalone CLI

The CLI is useful when you want the same scan behavior outside Claude Code.

```bash
greprules doctor
greprules init --mode auto
greprules fetch
greprules scan-edited
greprules scan --changed
```

More commands:

```bash
greprules detect --format json
greprules config inspect --format json
greprules recommend --format json --agent --target path/to/file
greprules setup-opengrep
greprules scan --target path/to/file
greprules scan --targets-from .greprules/out/targets.txt
greprules scan --full
greprules cleanup --plugin-cache --dry-run
```

## Configuration Reference

The production registry is:

```text
https://api.greprules.io
```

Configuration is merged in this order:

```text
CLI flags
environment variables
.greprules/config.local.json
.greprules/config.yaml
~/.config/greprules/config.json
defaults
```

User/global config is JSON:

```json
{
  "schemaVersion": "greprules.user.v1",
  "registry": "https://api.greprules.io",
  "opengrep": {
    "mode": "system",
    "path": "/Users/l0ch/.local/bin/opengrep",
    "version": "latest",
    "includeDefaultRules": true
  }
}
```

Repo-shared config is YAML:

```yaml
schemaVersion: greprules.config.v1
mode: auto
packs:
  - go-security
opengrep:
  mode: managed
```

For safety, `opengrep.path` from shared `.greprules/config.yaml` is ignored. Put executable paths in user/global config, repo-local config, environment variables, or CLI flags.

For local worker development only:

```bash
GREPRULES_REGISTRY=http://127.0.0.1:8790 greprules doctor
```

## Plugin Runtime

Agent plugins ship a `bin/greprules` wrapper. It resolves the real CLI in this order:

```text
GREPRULES_CLI_PATH
system PATH, excluding the plugin wrapper itself
GitHub Release bootstrap into <user-cache-dir>/greprules/plugins/<provider>/greprules/<version>/greprules
```

For plugin-specific details, see [`plugins/claude-code/README.md`](plugins/claude-code/README.md) and [`plugins/hermes/README.md`](plugins/hermes/README.md).

## Development

```bash
make test vet build
claude plugin validate --strict /path/to/greprules
claude plugin validate --strict /path/to/greprules/plugins/claude-code
```

To test a local CLI build before a release:

```bash
go build -o greprules ./cmd/greprules
export GREPRULES_CLI_PATH="$PWD/greprules"
```

To override the plugin bootstrap release during testing:

```bash
export GREPRULES_VERSION=v0.1.5
export GREPRULES_PLUGIN_CACHE_DIR=/tmp/greprules-plugin-cache
```

Cleanup is explicit:

```bash
greprules cleanup --config --plugin-cache --dry-run
greprules cleanup --config --plugin-cache
greprules cleanup --purge
greprules cleanup --repo
```
