---
description: Set up greprules after installing the Claude Code plugin
---

Use this skill when the user has just installed greprules, asks to set up greprules, or wants Claude Code to make greprules ready for local scans.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules agent-status --format json` to inspect registry access, OpenGrep runtime readiness, and rule-pack fetch state.
3. Parse the JSON output instead of reading config files directly.
4. If `registry.ok` and `opengrep.active.ok` are true, summarize setup readiness and stop. If `lock.exists` is false, mention only that rule packs have not been fetched yet and scan commands can select packs from target context before fetching.
5. If OpenGrep is not ready, run `greprules setup-opengrep` to prepare the greprules managed OpenGrep runtime. greprules always uses this managed runtime and does not require `opengrep` on shell `PATH`.
6. Run `greprules agent-status --format json` again and summarize registry, OpenGrep, and rule-pack state.
7. If the CLI wrapper reports that the real CLI is missing, tell the user to allow the bundled wrapper to bootstrap the release binary under the greprules user cache, set `GREPRULES_CLI_PATH`, or install `greprules` on `PATH` as a fallback. `GREPRULES_PLUGIN_CACHE_DIR` can override that cache only for debugging.

Do not run `greprules fetch` or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
