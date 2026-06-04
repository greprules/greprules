---
name: greprules-doctor
description: Check greprules registry access, OpenGrep runtime readiness, and fetched rule-pack state in Codex.
---

# greprules Doctor

Use this skill when the user asks Codex to check greprules readiness, diagnose greprules setup, or inspect whether OpenGrep and the registry are available.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`; `command -v greprules` may be empty in a valid plugin install. In this workflow, `greprules ...` means the resolved wrapper command.
2. Check Codex plugin and hook trust state before interpreting scan readiness:
   - Read `${CODEX_HOME:-$HOME/.codex}/config.toml`.
   - Confirm `[plugins."greprules@greprules"] enabled = true`.
   - Confirm these hook state entries have `trusted_hash` values:
     - `greprules@greprules:hooks/hooks.json:session_start:0:0`
     - `greprules@greprules:hooks/hooks.json:post_tool_use:0:0`
     - `greprules@greprules:hooks/hooks.json:stop:0:0`
   - If the plugin is enabled but any trusted hook entry is missing, tell the user that greprules is installed but automatic edited-file scans will not run yet. Ask them to open `/hooks`, trust the greprules hook entries, and start a new Codex session.
3. Run `greprules doctor --format json`.
4. Inspect `registry.ok`, `lock.exists`, and `opengrep.active.ok`.
5. If OpenGrep is not ready, summarize the available runtime options from the doctor report:
   - Use system OpenGrep on PATH when `opengrep.system.ok` is true.
   - Use managed OpenGrep when system OpenGrep is missing or the user wants greprules-managed setup.
   - Use path mode only when the user provides an absolute OpenGrep path.
6. If the user asks you to fix setup, apply the selected setup with `greprules config set ... --global`; for managed mode also run `greprules setup-opengrep`.
7. Run `greprules doctor --format json` again and summarize readiness.

Do not run `greprules fetch` or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
