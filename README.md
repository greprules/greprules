# greprules

Agent plugin for fetching trusted SAST rule packs from greprules.io and scanning local code changes with OpenGrep.

greprules is designed for local coding agents first. The Claude Code plugin gives Claude slash commands for checking setup, configuring OpenGrep, fetching rule packs, and scanning the files it edits. The Go CLI is the local runtime behind those commands.

## Quick Start

Run these inside Claude Code:

```text
/plugin marketplace add greprules/greprules
/plugin install greprules@greprules
/reload-plugins

/greprules:doctor
/greprules:configure
/greprules:scan
```

`/greprules:doctor` is the best first command. It checks the registry, OpenGrep runtime, local lockfile, and any setup steps Claude should handle before scanning.

## What It Does

- Fetches reusable SAST rule packs from greprules.io.
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
| `/greprules:scan` | You want Claude to fetch rule packs if needed and scan changed files or named targets. |

Common examples:

```text
/greprules:doctor
/greprules:configure
/greprules:scan
/greprules:scan src/auth
```

## Typical Workflow

1. Open a repository in Claude Code.
2. Run `/greprules:doctor`.
3. If setup is needed, run `/greprules:configure`.
4. Let Claude edit code as usual.
5. Run `/greprules:scan`, or let the plugin's Stop hook scan the files Claude edited.
6. Claude reads the scan result, separates likely true positives from noise, and proposes fixes.

The plugin does not require install-time configuration. Runtime choices are stored in greprules config so Claude Code, terminals, and CI can share the same behavior.

## Automatic Scan Hooks

The Claude Code plugin includes lightweight hooks for agent editing sessions:

- `SessionStart` checks readiness and reports setup gaps.
- `PostToolUse` records files Claude edited with `Edit`, `MultiEdit`, `Write`, or `NotebookEdit`.
- `Stop` scans the edited files once, then asks Claude to review the result before finishing.

Set this before starting Claude Code to disable automatic scans for a session:

```bash
export GREPRULES_AUTO_SCAN=false
```

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
greprules scan --changed
```

More commands:

```bash
greprules detect --format json
greprules config inspect --format json
greprules recommend
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

The Claude Code plugin ships a `bin/greprules` wrapper. It resolves the real CLI in this order:

```text
GREPRULES_CLI_PATH
system PATH, excluding the plugin wrapper itself
GitHub Release bootstrap into <user-cache-dir>/greprules/claude-plugin/greprules/v0.1.2/greprules
```

For plugin-specific details, see [`plugins/claude-code/README.md`](plugins/claude-code/README.md).

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
export GREPRULES_VERSION=v0.1.2
export GREPRULES_PLUGIN_CACHE_DIR=/tmp/greprules-plugin-cache
```

Cleanup is explicit:

```bash
greprules cleanup --config --plugin-cache --dry-run
greprules cleanup --config --plugin-cache
greprules cleanup --purge
greprules cleanup --repo
```
