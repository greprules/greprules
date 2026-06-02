---
description: Configure greprules registry and OpenGrep runtime through the CLI
---

Use this skill when the user asks to configure greprules for Claude Code or local agent use.

Rules:

- Prefer `greprules config set` over editing config files directly.
- Use global config for machine/user preferences such as registry URL, OpenGrep mode, and OpenGrep executable path.
- Use repo-local config only for settings that are specific to this checkout and should not be committed.
- Do not write `opengrep.path` to shared `.greprules/config.yaml`.
- After changing config, run `greprules config inspect --format json` or `greprules doctor --format json` and summarize the effective config.
- Claude Code plugin configuration exposes `install_opengrep` and `opengrep_path`; do not ask users to type `managed`, `system`, or `path` into plugin configuration.
- Treat `opengrep_path` as highest priority, `install_opengrep=true` as managed OpenGrep, and `install_opengrep=false` as system OpenGrep.
- Expose OpenGrep runtime choices as automatic install, system PATH, or advanced path; do not present `auto` as a user choice.
- If the user wants terminal or CI parity, write the setting with `greprules config set --global`.

Common commands:

```bash
greprules config set registry http://localhost:8787 --global
greprules config set opengrep.mode system --global
greprules config set opengrep.mode managed --global
greprules config set opengrep.mode path --global
greprules config set opengrep.path /absolute/path/to/opengrep --global
greprules config inspect --format json
greprules doctor --format json
```

Runtime selection workflow:

1. Run `greprules doctor --format json`.
2. If the user configured `opengrep_path`, validate that executable and use path mode.
3. If the user chooses system OpenGrep, run `greprules config set opengrep.mode system --global`.
4. If the user chooses managed OpenGrep, run `greprules config set opengrep.mode managed --global`, then `greprules setup-opengrep`.
5. If the user chooses a custom path, verify it with `greprules doctor --engine path --opengrep-path <path> --format json`, then write `opengrep.mode=path` and `opengrep.path=<path>` to global config.
6. Run `greprules doctor --format json` again and summarize readiness.

If the user asks for a recommended default and no system OpenGrep is already available, prefer managed OpenGrep for reproducible community scans. If system OpenGrep is already available, ask before switching because system is faster to adopt but less reproducible across machines.
