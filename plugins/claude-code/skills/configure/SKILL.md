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
- Claude Code plugin options are available as `opengrep_mode` and `opengrep_path`. A non-`auto` plugin option overrides the CLI config only inside Claude Code plugin subprocesses.
- If the user wants the same behavior in terminals or CI, write the setting with `greprules config set --global` instead of relying only on plugin options.

Common commands:

```bash
claude plugin install greprules@greprules --config opengrep_mode=system
claude plugin install greprules@greprules --config opengrep_mode=managed
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
2. If `opengrep.system.ok` is true and `opengrep.active.ok` is false, ask the user whether to use the detected system OpenGrep or install managed OpenGrep.
3. If the user chooses system OpenGrep, run `greprules config set opengrep.mode system --global`.
4. If the user chooses managed OpenGrep, run `greprules setup-opengrep`, then `greprules config set opengrep.mode managed --global`.
5. Run `greprules doctor --format json` again and summarize readiness.

If the user asks for a recommended default and no system OpenGrep is already available, prefer managed OpenGrep for reproducible community scans. If system OpenGrep is already available, ask before switching because system is faster to adopt but less reproducible across machines.
