# Contributing to greprules

Thanks for improving greprules. This repository contains the Go CLI plus agent
plugins for Claude Code, Codex, and Hermes.

## Before you start

- Search existing issues and pull requests before opening a new one.
- For bug reports, include the affected agent, greprules version, OpenGrep mode,
  operating system, command output, and minimal reproduction steps.
- For security vulnerabilities, do not open a public issue. Follow
  [SECURITY.md](SECURITY.md).

## Development setup

Required tools:

- Go version from `go.mod`
- Python 3 for plugin hook syntax checks
- OpenGrep only when testing local scans end to end

Useful commands:

```bash
go test ./...
go vet ./...
go build ./cmd/greprules
python3 -m py_compile __init__.py plugins/hermes/__init__.py plugins/codex/scripts/greprules-hook.py plugins/claude-code/scripts/greprules-hook.py
```

The same checks run in CI.

## Pull requests

Keep pull requests focused. A good PR usually includes:

- A short description of the user-facing change.
- The affected surface: CLI, Claude Code plugin, Codex plugin, Hermes plugin,
  release packaging, or docs.
- Tests or validation commands that match the risk of the change.
- Notes about compatibility, migration, or release impact when relevant.

## Plugin changes

Agent plugins should preserve these behaviors:

- Skills and hooks invoke the plugin-bundled `bin/greprules` wrapper.
- Missing shell `PATH` entries are not treated as plugin setup failures.
- Hook-level settings such as `autoScan` and `trackEditedFiles` stay in the
  agent plugin settings, not shared CLI config.
- Edited-file scans use explicit tracked files for the current agent session.

When changing a shared behavior, check all supported plugins.

## Rule and scan behavior

greprules fetches rule packs from the registry and runs scans locally through
OpenGrep. Do not upload local source code or scan results unless a user has
explicitly chosen a workflow that does so.

When changing scan behavior, prefer deterministic output and clear JSON fields
that agents can consume reliably.
