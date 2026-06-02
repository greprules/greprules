---
description: Configure greprules registry and OpenGrep runtime through the CLI
---

Use this skill when the user asks to configure greprules for Claude Code or local agent use.

Rules:

- Prefer `greprules config set` over editing config files directly.
- Use global config for machine/user preferences such as registry URL, OpenGrep mode, and OpenGrep executable path.
- Use repo-local config only for settings that are specific to this checkout and should not be committed.
- Do not write `opengrep.path` to shared `.greprules/config.yaml`.
- After changing config, run `greprules config inspect --format json` and summarize the effective config.

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

If the user asks for a recommended default, prefer managed OpenGrep for reproducible community scans. Use system OpenGrep only when the user explicitly wants to use their existing installation.

