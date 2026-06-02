# greprules

`greprules` is a Go CLI for running greprules.io rule packs locally with a managed OpenGrep runtime.

The MVP scope is intentionally narrow:

- detect repository languages and frameworks
- create `.greprules/config.yaml`
- recommend and fetch greprules.io packs
- pin pack artifacts in `.greprules/lock.json`
- install a managed OpenGrep binary
- run changed-file, explicit-target, or full scans
- write OpenGrep JSON, SARIF, and agent-readable JSON output

Out of scope for this CLI MVP:

- rule draft creation
- upload/auth/moderation flows
- Autoproof SARIF ingest
- CI SaaS integration
- AI true/false-positive decisions or automatic patches

## Commands

```bash
greprules detect --format json
greprules init --mode auto
greprules config inspect --format json
greprules config set opengrep.mode system --global
greprules recommend
greprules fetch
greprules setup-opengrep
greprules scan --changed
greprules scan --target path/to/file
greprules scan --targets-from .greprules/out/targets.txt
greprules scan --full
greprules doctor --format json
```

## OpenGrep Runtime Policy

`greprules` defaults to a managed OpenGrep runtime for reproducible community scans. A system-wide OpenGrep install is supported, but it must be selected explicitly.

Managed runtime:

```bash
greprules init --engine managed
greprules setup-opengrep --version latest
greprules scan --full
```

System `PATH` runtime:

```bash
greprules config set opengrep.mode system --global
greprules scan --engine system --full
greprules doctor --engine system --debug
```

Explicit binary path:

```bash
greprules config set opengrep.mode path --global
greprules config set opengrep.path /opt/homebrew/bin/opengrep --global
greprules scan --engine path --opengrep-path /opt/homebrew/bin/opengrep --full
```

The selected engine is recorded in `.greprules/lock.json` and `.greprules/out/agent-result.json` with its mode, source, path, version, and SHA-256.

## Agent-Friendly Configuration

Agent plugins should write structured config once, then run the CLI without carrying config details in prompts.

Configuration is merged in this order:

```text
CLI flags
environment variables
.greprules/config.local.json
.greprules/config.yaml
~/.config/greprules/config.json
defaults
```

User/global config is JSON so plugin UIs can write it directly:

```json
{
  "schemaVersion": "greprules.user.v1",
  "registry": "http://localhost:8787",
  "opengrep": {
    "mode": "system",
    "path": "/Users/l0ch/.local/bin/opengrep",
    "version": "latest"
  }
}
```

Repo-shared config remains YAML:

```yaml
schemaVersion: greprules.config.v1
mode: auto
packs:
  - go-security
opengrep:
  mode: managed
```

Machine-specific repo config is JSON and should not be committed:

```text
.greprules/config.local.json
```

For safety, `opengrep.path` from shared `.greprules/config.yaml` is ignored. Put executable paths in user/global config, repo-local config, environment variables, or CLI flags.

Recommended plugin flow:

```bash
greprules config set registry http://localhost:8787 --global
greprules config set opengrep.mode system --global
greprules doctor --format json
greprules fetch
greprules scan --changed
```

## Claude Code Plugin

The repository includes a Claude Code marketplace manifest at the repo root for installing the greprules plugin from GitHub.

```bash
claude plugin validate /Users/l0ch/provally/projects/greprules
claude plugin validate /Users/l0ch/provally/projects/greprules/plugins/claude-code
claude plugin marketplace add greprules/greprules --scope user
claude plugin install greprules@greprules --scope user
```

Then reload Claude Code and use:

```text
/greprules:doctor
/greprules:configure
/greprules:scan
```

The plugin does not require install-time configuration. When `/greprules:doctor`, `/greprules:configure`, or `/greprules:scan` finds that OpenGrep is not ready, Claude first checks for a system `opengrep` on `PATH`, then asks whether to use system OpenGrep, install managed OpenGrep, or configure a manual executable path. The selected runtime is written with `greprules config set ... --global`, so terminals and CI can use the same setting.

The plugin includes a `bin/greprules` wrapper and lifecycle hooks tuned for agent editing:

- `SessionStart`: run `doctor` and report setup gaps. It never installs OpenGrep or scans.
- `PostToolUse` for `Edit`, `MultiEdit`, `Write`, and `NotebookEdit`: capture the file path from Claude's actual edit event and mark the workspace dirty. It never scans.
- `Stop`: if the workspace is dirty, scan the files Claude actually edited and inject a compact summary into Claude's next model context. This does not require a git repository.

The wrapper resolves the real CLI in this order:

```text
GREPRULES_CLI_PATH
system PATH, excluding the plugin wrapper itself
GitHub Release bootstrap into $CLAUDE_PLUGIN_DATA/greprules/v0.1.0/greprules
```

OpenGrep runtime configuration lives in greprules config files, not Claude Code plugin settings. Use `greprules config inspect --format json` to inspect the effective configuration.

For local development before a release exists, build and copy the CLI onto `PATH`:

```bash
go build -o greprules ./cmd/greprules
mkdir -p ~/.local/bin
cp ./greprules ~/.local/bin/greprules
```

After `v0.1.0` is published, the plugin can bootstrap the matching native binary from GitHub Releases and verify it against `checksums.txt`. Override the release version only for testing:

```bash
export GREPRULES_VERSION=v0.1.0
```

To disable the automatic hook in a Claude Code session:

```bash
export GREPRULES_AUTO_SCAN=false
```

Automatic scan guardrails:

```bash
export GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS=45
export GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES=100
```

## Local Files

```text
.greprules/config.yaml
.greprules/config.local.json
.greprules/lock.json
.greprules/cache/packs/...
.greprules/out/scan.json
.greprules/out/scan.sarif
.greprules/out/agent-result.json
```

Managed OpenGrep binaries are stored under the OS user cache directory:

```text
<user-cache-dir>/greprules/opengrep/<version>/opengrep
```

## Development

```bash
go test ./...
go build -o greprules ./cmd/greprules
```
