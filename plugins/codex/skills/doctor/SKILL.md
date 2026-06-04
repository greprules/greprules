---
name: greprules-doctor
description: Check greprules registry access, OpenGrep runtime readiness, and fetched rule-pack state in Codex.
---

# greprules Doctor

Use this skill when the user asks Codex to check greprules readiness, diagnose greprules setup, or inspect whether OpenGrep and the registry are available.

Workflow:

1. Verify the CLI is available with `command -v greprules`.
2. Run `greprules doctor --format json`.
3. Inspect `registry.ok`, `lock.exists`, and `opengrep.active.ok`.
4. If OpenGrep is not ready, summarize the available runtime options from the doctor report:
   - Use system OpenGrep on PATH when `opengrep.system.ok` is true.
   - Use managed OpenGrep when system OpenGrep is missing or the user wants greprules-managed setup.
   - Use path mode only when the user provides an absolute OpenGrep path.
5. If the user asks you to fix setup, apply the selected setup with `greprules config set ... --global`; for managed mode also run `greprules setup-opengrep`.
6. Run `greprules doctor --format json` again and summarize readiness.

Do not run `greprules fetch` or `greprules scan` from this skill unless the user explicitly asks for that follow-up.
