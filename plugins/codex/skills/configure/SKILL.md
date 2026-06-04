---
name: greprules-configure
description: Configure greprules registry and OpenGrep runtime for Codex, terminal, and CI parity.
---

# greprules Configure

Use this skill when the user asks Codex to configure greprules, choose an OpenGrep runtime, change the registry URL, or inspect effective greprules settings.

Rules:

- Prefer `greprules config set` over editing config files directly.
- Persist OpenGrep runtime selection with `--global` unless the user asks for repository-local settings.
- Do not write machine-specific `opengrep.path` to shared `.greprules/config.yaml`.
- OpenGrep default rules are included by default with `opengrep.includeDefaultRules=true`; only change this if the user asks for greprules.io packs only.

Common commands:

```bash
greprules config set registry https://api.greprules.io --global
greprules config set opengrep.mode system --global
greprules config set opengrep.mode managed --global
greprules config set opengrep.mode path --global
greprules config set opengrep.path /absolute/path/to/opengrep --global
greprules config set opengrep.includeDefaultRules false --global
greprules config inspect --format json
greprules doctor --format json
```

Workflow:

1. Run `greprules doctor --format json`.
2. If registry access fails, report the registry URL and error. Only change registry if the user asked for a different registry.
3. If OpenGrep is ready, summarize the effective runtime and stop.
4. If setup is needed, choose the smallest suitable option from the doctor report:
   - `system` when `opengrep.system.ok` is true.
   - `managed` when system OpenGrep is unavailable or the user prefers plugin-managed setup.
   - `path` only when the user provides a path.
5. Apply the selected setting with `greprules config set ... --global`.
6. For managed mode, run `greprules setup-opengrep`.
7. Run `greprules doctor --format json` again and summarize registry, OpenGrep, and rule-pack state.
