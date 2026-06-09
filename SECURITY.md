# Security Policy

greprules is security tooling, so vulnerability reports need to be handled
privately and with enough detail to reproduce safely.

## Supported versions

Security fixes are handled on the latest released CLI and plugin versions.
Users should update to the latest GitHub release before reporting an issue that
may already be fixed.

## Reporting a vulnerability

Do not open a public issue for vulnerabilities, exploitable behavior, leaked
secrets, or private exploit details.

Preferred reporting path:

Use GitHub's private vulnerability reporting for this repository if it is
available. If private reporting is unavailable, open a public issue only for
non-sensitive security hardening requests and avoid exploit details, secrets, or
private vulnerability information.

Include:

- Affected component: CLI, Claude Code plugin, Codex plugin, Hermes plugin,
  release packaging, or registry interaction.
- Affected version or commit.
- Reproduction steps and expected impact.
- Logs, command output, or proof-of-concept details, with secrets removed.
- Whether the issue is already public.

## Scope

In scope:

- Remote code execution, command injection, path traversal, or unsafe file
  writes in the CLI or plugins.
- Unsafe handling of registry URLs, release downloads, checksums, rule packs, or
  scan output.
- Vulnerabilities in plugin hooks that could expose private source code,
  secrets, or local files.

Out of scope:

- False positives or false negatives in public SAST rules unless they create a
  security issue in greprules itself.
- Reports that require access to private user repositories without permission.
- Denial-of-service reports based only on intentionally huge local inputs unless
  they affect normal plugin or CLI operation.

## Response

Maintainers will triage reports, ask for clarification when needed, and publish
fixes through normal releases. Public disclosure timing should be coordinated
with maintainers when a report has real exploit impact.
