# greprules

Agent plugin for fetching trusted SAST rule packs from greprules.io and scanning local code changes with OpenGrep.

greprules is designed for local coding agents first. The Claude Code, Codex, and Hermes plugins give agents commands or skills for first-run setup, configuring OpenGrep, selecting rule packs from code context, fetching those packs, and scanning local code changes. The Go CLI is the deterministic local runtime behind those commands.

## Quick Start

### Claude Code

```text
/plugin marketplace add greprules/greprules
/plugin install greprules@greprules
/reload-plugins
```

```text
/greprules:setup
/greprules:configure
/greprules:scan-edited
```

### Codex

```bash
codex plugin marketplace add greprules/greprules --sparse .agents/plugins --sparse plugins/codex
```

Codex app: **Plugins** -> `greprules` -> install/enable.
Codex CLI TUI:

```text
/plugins
```

```text
$greprules-setup
$greprules-configure
$greprules-scan-edited
```

### Hermes

```bash
hermes plugins install greprules/greprules --enable
```

```text
/greprules setup
/greprules configure
/greprules scan-edited
```

Run `setup` once after installing the plugin. Use `configure` later to inspect status or change settings.

## What It Does

- Lets agents select reusable SAST rule packs from greprules.io based on target code context.
- Fetches the selected packs reproducibly through the CLI.
- Configures OpenGrep for local scans.
- Tracks files edited by local coding agents.
- Scans changed files or explicit targets before the agent finishes.
- Writes agent-readable results so the agent can review findings and suggest fixes.
- Keeps source code local; the plugin fetches rules and runs OpenGrep on your machine.

## Plugin Docs

- [Claude Code](plugins/claude-code/README.md)
- [Codex](plugins/codex/README.md)
- [Hermes](plugins/hermes/README.md)

## OpenGrep Runtime

OpenGrep does the actual scanning. greprules keeps runtime selection explicit so scans are reproducible and easy to debug.

| Mode | Use when |
| --- | --- |
| `managed` | You want greprules to install and use a managed OpenGrep binary. This is the default. |
| `system` | You already have `opengrep` on `PATH` and want to use it. |
| `path` | You want to point greprules at a specific OpenGrep executable. |

Use your agent's configure command to choose a runtime. From a shell, the same settings are available through:

```bash
greprules config set opengrep.mode system --global
greprules config set opengrep.mode managed --global
greprules config set opengrep.mode path --global
greprules config set opengrep.path /absolute/path/to/opengrep --global
```

By default, greprules scans fetched greprules.io packs only. To also include OpenGrep's default auto-selected rules:

```bash
greprules config set opengrep.includeDefaultRules true --global
```

Hook behavior is configured per agent plugin, not through the shared CLI config:

```text
~/.claude/plugins/greprules/settings.json
~/.codex/plugins/greprules/settings.json
~/.hermes/plugins/greprules/settings.json
```

Each file uses the same keys:

```json
{
  "autoScan": false,
  "trackEditedFiles": true,
  "autoScanMinIntervalSeconds": 45,
  "autoScanMaxChangedFiles": 100
}
```

## Results and Local Files

The important files are:

```text
.greprules/config.yaml
.greprules/lock.json
.greprules/out/agent-result.json
.greprules/out/scan.sarif
.greprules/plugin-data/<provider>/sessions/<session-id>/out/agent-result.json
```

Normal CLI scans write `.greprules/out/agent-result.json`. Plugin edited-file scans write session-local results under `.greprules/plugin-data/<provider>/sessions/<session-id>/out/agent-result.json`; agents should read the full result path reported in the scan summary. The agent result contains the scan summary, findings, warnings, selected OpenGrep runtime, and rule pack metadata. `.greprules/lock.json` pins fetched pack artifacts and records the selected scan runtime.

Generated local paths are ignored automatically in git repositories:

```text
.greprules/cache/
.greprules/out/
.greprules/plugin-data/
.greprules/config.local.json
```

Shared files such as `.greprules/config.yaml` and `.greprules/lock.json` are not ignored automatically.

## Standalone CLI

The CLI is useful when you want the same scan behavior outside an agent.

```bash
greprules init --mode auto
greprules setup-opengrep
greprules fetch
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
    "includeDefaultRules": false
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
GREPRULES_REGISTRY=http://127.0.0.1:8790 greprules config inspect --format json
```

## Plugin Runtime

Agent plugins ship a `bin/greprules` wrapper, not the native Go binary itself. Skills and hooks should invoke that bundled wrapper directly; `greprules` being absent from the user's shell `PATH` is not a plugin setup failure.

The wrapper resolves the real CLI in this order:

```text
GREPRULES_CLI_PATH
system PATH, excluding the plugin wrapper itself
GitHub Release bootstrap into <user-cache-dir>/greprules/plugins/<provider>/greprules/<version>/greprules
```

For plugin-specific details, see [`plugins/claude-code/README.md`](plugins/claude-code/README.md), [`plugins/codex/README.md`](plugins/codex/README.md), and [`plugins/hermes/README.md`](plugins/hermes/README.md).

## Development

```bash
make test vet build
claude plugin validate --strict /path/to/greprules
claude plugin validate --strict /path/to/greprules/plugins/claude-code
CODEX_HOME="$(mktemp -d)" codex plugin marketplace add /path/to/greprules
```

To test a local CLI build before a release:

```bash
go build -o greprules ./cmd/greprules
export GREPRULES_CLI_PATH="$PWD/greprules"
```

Cleanup is explicit:

```bash
greprules cleanup --config --plugin-cache --dry-run
greprules cleanup --config --plugin-cache
greprules cleanup --purge
greprules cleanup --repo
```
