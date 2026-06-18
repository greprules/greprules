---
name: greprules-setup
description: Set up greprules after installing the Codex plugin.
---

# greprules Setup

Use this skill when the user has just installed greprules, asks Codex to set up greprules, or wants greprules made ready for local scans.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; `command -v greprules` may be empty in a valid plugin install. In this workflow, `greprules ...` means the resolved wrapper command.
2. Check Codex plugin and hook trust state before interpreting automatic scan readiness:
   - Read `${CODEX_HOME:-$HOME/.codex}/config.toml`.
   - Confirm `[plugins."greprules@greprules"] enabled = true`.
   - Confirm these hook state entries have `trusted_hash` values:
     - `greprules@greprules:hooks/hooks.json:session_start:0:0`
     - `greprules@greprules:hooks/hooks.json:post_tool_use:0:0`
     - `greprules@greprules:hooks/hooks.json:stop:0:0`
   - If the plugin is enabled but any trusted hook entry is missing, tell the user that greprules is installed but automatic edited-file scans will not run yet. Ask them to open `/hooks`, trust the greprules hook entries, and start a new Codex session.
3. Run `greprules agent-status --format json` to inspect registry access, OpenGrep runtime readiness, and rule-pack fetch state.
4. If `registry.ok` and `opengrep.active.ok` are true, summarize setup readiness and stop. If `lock.exists` is false, mention only that rule packs have not been fetched yet and scan commands can select packs from target context before fetching.
5. If OpenGrep is not ready and the user has not already requested a specific runtime, set up managed OpenGrep for the simplest first-run path:
   - Run `greprules agent-config set opengrep.mode managed --global`.
   - Run `greprules setup-opengrep`.
6. If the user asked for system or manual-path OpenGrep instead, hand off to `$greprules-configure` or apply the requested setting through `greprules agent-config set ... --global`.
7. Run `greprules agent-status --format json` again and summarize registry, OpenGrep, and rule-pack state.

Do not run `greprules fetch` or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
