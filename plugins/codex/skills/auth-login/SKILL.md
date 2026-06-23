---
name: greprules-auth-login
description: Sign in to greprules.io from Codex chat so community feedback and rule proposals can submit with a stored CLI token.
---

# greprules Auth Login

Use this skill when the user asks Codex to log in to greprules, connect greprules.io, authorize community contribution, or fix a greprules login-required error.

This skill only authenticates the local greprules CLI. It must not submit scan feedback, rule proposals, source code, findings, or diagnostics.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`.
2. Run `greprules auth status`.
3. If status succeeds, report that greprules is logged in and stop.
4. If status reports missing or expired login, start the browser approval flow yourself:
   - Prefer `greprules auth login --agent`.
   - If the installed CLI does not recognize `--agent`, retry with `greprules auth login --no-browser`.
5. As soon as the command prints an approval URL or JSON event with `event=approval_required`, show the user:
   - the approval URL
   - the short code, if present
   - that they should approve it in the browser
6. Keep the login command running while the user approves. Do not wait silently until the command exits before showing the URL.
7. When the command reports login success, rerun `greprules auth status` and summarize the logged-in registry and expiry if shown.

Rules:

- Do not ask the user to run `greprules auth login` manually unless the local command execution environment cannot run greprules at all.
- Do not ask the user to paste tokens, API keys, cookies, or browser session data into chat.
- Do not print or inspect the stored token file.
- If approval expires, rerun the login command and show only the newest URL/code.
- Community uploads still require a separate explicit approval turn in `$greprules-submit-feedback` or `$greprules-propose-rule`.
