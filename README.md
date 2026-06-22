# greprules

[![Release](https://badgen.net/github/release/greprules/greprules)](https://github.com/greprules/greprules/releases)
[![CI](https://github.com/greprules/greprules/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/greprules/greprules/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/greprules/greprules)](https://github.com/greprules/greprules/blob/main/LICENSE)
[![Go Report](https://goreportcard.com/badge/github.com/greprules/greprules)](https://goreportcard.com/report/github.com/greprules/greprules)

Agent plugin and CLI for fetching SAST rule packs from greprules.io and scanning local code changes with OpenGrep.

greprules is designed for local coding agents first. The Claude Code, Codex, and Hermes plugins give agents commands or skills for first-run setup, configuring OpenGrep, selecting rule packs from code context, fetching those packs, and scanning local code changes. The Go CLI is the deterministic local runtime behind those commands.

greprules is maintained in the greprules GitHub organization with support from Provally. Provally operates the hosted greprules.io registry and API used by the default configuration. Normal scans fetch rule packs from greprules.io, run OpenGrep locally, and keep standalone project locks in user state. Agent plugin scans may also write provider-specific results under local `.greprules/` paths.

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

For standalone CLI usage, the default managed runtime is installed automatically by `greprules scan` or explicitly with `greprules setup-opengrep`. Agent plugins expose runtime configuration through their `/greprules configure` or `$greprules-configure` workflows.

For agent/plugin automation only, the underlying settings command is:

```bash
greprules agent-config set opengrep.mode system --global
greprules agent-config set opengrep.mode managed --global
greprules agent-config set opengrep.mode path --global
greprules agent-config set opengrep.path /absolute/path/to/opengrep --global
```

By default, greprules scans fetched greprules.io packs only. To also include OpenGrep's default auto-selected rules:

```bash
greprules agent-config set opengrep.includeDefaultRules true --global
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
user state: projects/<project-key>/lock.json
user cache: packs/<slug>/<sha>/...
.greprules/config.yaml
.greprules/plugin-data/<provider>/sessions/<session-id>/out/agent-result.json
```

Standalone CLI scans follow OpenGrep output behavior: stdout and files are controlled by the OpenGrep arguments you pass, such as `--json`, `--sarif`, and `--output`. Standalone project locks are stored in user state keyed by the canonical project root, while rule pack artifacts are stored in user cache and reused across projects. Agent scans use the hidden `agent-scan` command and write structured results under `.greprules/out/agent-result.json` or session-local plugin output paths.

Generated local paths are ignored automatically in git repositories:

```text
.greprules/out/
.greprules/plugin-data/
.greprules/config.local.json
```

Shared files such as `.greprules/config.yaml` are not ignored automatically.

## Standalone CLI

The CLI is useful when you want greprules.io rule packs with normal OpenGrep scan behavior.

```bash
greprules scan .
```

On first run, `scan` detects the target language/framework context, selects matching greprules.io rule packs, fetches and pins them in user state, installs managed OpenGrep when needed, and then runs `opengrep scan` with the selected rule packs injected as `--config` arguments. Existing project locks are reused so pinned rule packs stay reproducible on the same machine. Supported OpenGrep scan options can be mixed in normal OpenGrep style. Put advanced OpenGrep flags after `--` so greprules does not mistake their values for pack-selection targets.

More commands:

```bash
greprules scan --changed
greprules fetch python-security
greprules setup-opengrep
greprules scan path/to/file
greprules scan . --json
greprules scan . --sarif --output result.sarif
greprules scan . --severity ERROR
greprules scan --json-output result.json src
greprules scan . --no-prepare
greprules scan . --verbose
greprules scan src -- --some-future-opengrep-flag value
greprules cleanup --plugin-cache --dry-run
```

## Advanced Agent Configuration Reference

Standalone CLI users normally do not need to edit greprules config. `scan` prepares the managed runtime, selects rule packs, fetches missing packs, and then delegates output behavior to OpenGrep. Use this reference only for agent/plugin automation or local development.

The production registry is:

```text
https://api.greprules.io
```

Agent configuration is merged in this order:

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
GREPRULES_REGISTRY=http://127.0.0.1:8790 greprules agent-status --format json
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

## Maintainers

greprules is maintained by contributors in the greprules GitHub organization with support from [Provally](https://provally.io/).

<p>
  <a href="https://provally.io/">
    <img src="docs/assets/provally-logo.png" alt="Provally" width="28" height="28">
  </a>
</p>
