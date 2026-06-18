---
description: Set up greprules after installing the Claude Code plugin
---

Use this skill when the user has just installed greprules, asks to set up greprules, or wants Claude Code to make greprules ready for local scans.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; in this workflow, `greprules ...` means the resolved wrapper command.
2. Run `greprules agent-status --format json` to inspect registry access, OpenGrep runtime readiness, and rule-pack fetch state.
3. Parse the JSON output instead of reading config files directly.
4. If `registry.ok` and `opengrep.active.ok` are true, summarize setup readiness and stop. If `lock.exists` is false, mention only that rule packs have not been fetched yet and scan commands can select packs from target context before fetching.
5. If OpenGrep is not ready and the user has not already requested a specific runtime, set up managed OpenGrep for the simplest first-run path:
   - Run `greprules agent-config set opengrep.mode managed --global`.
   - Run `greprules setup-opengrep`.
6. If the user asked for system or manual-path OpenGrep instead, hand off to `/greprules:configure` or apply the requested setting through `greprules agent-config set ... --global`.
7. Run `greprules agent-status --format json` again and summarize registry, OpenGrep, and rule-pack state.
8. If the CLI wrapper reports that the real CLI is missing, tell the user to set `GREPRULES_CLI_PATH`, install `greprules` on `PATH`, or allow the bundled wrapper to bootstrap the release binary under the greprules user cache. `GREPRULES_PLUGIN_CACHE_DIR` can override that cache only for debugging.

Do not run `greprules fetch` or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
