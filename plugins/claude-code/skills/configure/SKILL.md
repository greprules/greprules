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
- Treat a missing lockfile as rule-pack fetch state, not incomplete OpenGrep configuration. Scan commands can run `greprules fetch` automatically when the registry is reachable.
- Do not configure OpenGrep through Claude Code plugin options; this plugin intentionally has no install-time userConfig.
- Persist OpenGrep runtime selection with `greprules config set ... --global` so Claude Code, terminals, and CI share the same setting.
- Expose OpenGrep runtime choices as managed install, system PATH, or manual executable path.
- OpenGrep default rules are included by default with `opengrep.includeDefaultRules=true`; only change this if the user asks for greprules.io packs only.
- If the user wants terminal or CI parity, write the setting with `greprules config set --global`.

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

For local worker development only, override the registry explicitly:

```bash
GREPRULES_REGISTRY=http://127.0.0.1:8790 greprules doctor --format json
```

Runtime selection workflow:

1. Run `greprules doctor --format json`.
2. If `opengrep.active.ok` is true and the user did not ask to change runtime, summarize runtime readiness and stop. If `lock.exists` is false, mention only that rule packs have not been fetched yet and can be fetched on first scan.
3. Check `opengrep.system.ok`, `opengrep.system.runtime.path`, and `opengrep.system.runtime.version` first.
4. If the user has not already chosen a runtime, use AskUserQuestion when available; otherwise ask one concise question. Offer these choices:
   - Use system OpenGrep on PATH. Recommend this when `opengrep.system.ok` is true.
   - Install managed OpenGrep. Recommend this when no system OpenGrep is available or the user wants reproducible scans.
   - Use a manual OpenGrep executable path. Ask for the absolute path before applying it.
5. If the user chooses system OpenGrep, run `greprules config set opengrep.mode system --global`.
6. If the user chooses managed OpenGrep, run `greprules config set opengrep.mode managed --global`, then `greprules setup-opengrep`.
7. If the user chooses a manual path, verify it with `greprules doctor --engine path --opengrep-path <path> --format json`, then write `opengrep.mode=path` and `opengrep.path=<path>` to global config.
8. Run `greprules doctor --format json` again and summarize registry/OpenGrep readiness. Do not call missing rule packs a setup failure.

If the user asks for a recommended default and no system OpenGrep is already available, prefer managed OpenGrep for reproducible community scans. If system OpenGrep is already available, ask before switching because system is faster to adopt but less reproducible across machines.
