---
description: Configure, scan, or fetch greprules packs from Hermes chat
---

Use this skill when the user wants Hermes to run greprules scans, review greprules results, contribute scan feedback, propose a greprules.io rule from an independently identified vulnerability, or understand greprules plugin behavior.

Hermes slash commands and hooks resolve the greprules command through the plugin adapter. They use `GREPRULES_CLI_PATH` only as an explicit local override, otherwise the plugin-bundled `bin/greprules` wrapper and its plugin-pinned managed CLI. `greprules` on shell `PATH` is only a fallback when managed bootstrap fails. Do not treat a missing `command -v greprules` result as a Hermes plugin readiness failure.

Available slash commands:

1. `/greprules configure` checks first-run readiness, registry access, rule-pack fetch state, OpenGrep readiness, and effective agent settings.
2. `/greprules configure managed` prepares the greprules managed OpenGrep runtime.
3. `/greprules configure registry <url>`, `include-default-rules true|false`, `auto-scan true|false`, `track-edited-files true|false`, `auto-scan-min-interval <seconds>`, and `auto-scan-max-changed-files <count>` configure persistent greprules behavior.
4. `/greprules scan` chooses edited files, git changes, explicit paths, or full repository scope from the user's request.

Scan examples: `/greprules scan`, `/greprules scan changed`, `/greprules scan src/auth`, `/greprules scan full`.

When rule-pack selection needs agent input, inspect the `selectionContext` returned by `greprules agent-scan scan`. Edited-file scans pass absolute explicit targets. Choose only slugs present in `selectionContext.availablePacks`, fetch them with the explicit fetch command shown in the scan message, then rerun the scan.

The `pre_llm_call` hook can inject compact edited-file scan results before the next model turn when Hermes greprules `autoScan=true`. Automatic scan context injection is disabled by default; use `/greprules configure auto-scan true` to enable it persistently while keeping manual commands available.

Hermes edited-file state is stored under `.greprules/plugin-data/hermes/sessions/<session-or-task-id>/`. The adapter passes only explicit target files to the CLI. If multiple Hermes sessions have dirty files, do not merge them; use the current session flow or `/greprules scan <path>` for explicit files.

greprules.io login:

Use this flow when the user asks Hermes to log in to greprules, connect greprules.io, authorize community contribution, or fix a greprules login-required error. This flow only authenticates the local greprules CLI. It must not submit scan feedback, rule proposals, source code, findings, or diagnostics.

1. Run `greprules auth status`.
2. If status succeeds, report that greprules is logged in and stop.
3. If status reports missing or expired login, start the browser approval flow yourself:
   - Prefer `greprules auth login --agent`.
   - If the installed CLI does not recognize `--agent`, retry with `greprules auth login --no-browser`.
4. As soon as the command prints an approval URL or JSON event with `event=approval_required`, show the user the approval URL, the short code if present, and tell them to approve it in the browser.
5. Keep the login command running while the user approves. Do not wait silently until the command exits before showing the URL.
6. When the command reports login success, rerun `greprules auth status` and summarize the logged-in registry and expiry if shown.

Do not ask the user to paste tokens, API keys, cookies, or browser session data into chat. Do not print or inspect the stored token file. If approval expires, rerun the login command and show only the newest URL/code.

Community feedback contribution:

After a manual scan/review, if the findings include justified true-positive, false-positive, accepted-risk, fixed, not-applicable, warning, or diagnostic signal, Hermes can offer to contribute that context to greprules.io. Use this flow when the user approves that offer or explicitly asks Hermes to contribute greprules scan feedback, report true positives or false positives to greprules.io, or share scan warnings/diagnostics. This flow is never automatic and must not run from hooks without a user approval turn.

1. Read the `Full result:` path from a previous greprules scan. If no result path is clear, ask the user for the `agent-result.json` path.
2. Review findings, warnings, and errors locally. Classify only findings you can justify as `true_positive`, `false_positive`, `accepted_risk`, `not_applicable`, or `fixed`.
3. Run `greprules agent-feedback prepare --result <agent-result.json> --out <feedback-bundle.json>`.
4. Read the generated bundle and verify it contains hashed project/finding identifiers, not raw source code or raw file paths.
5. Add entries to the bundle `feedback` array only for findings still present in the prepared bundle. Do not add feedback for findings omitted by `prepare`; OpenGrep default-rule findings are not greprules.io registry rules.
6. Show a concise preview covering feedback verdicts, diagnostics, uploaded fields, and excluded fields. Uploaded fields are rule slug, rule version, finding fingerprint, verdict, short message, and diagnostic hashes. Excluded fields are source code, raw file paths, private repository URLs, and code snippets.
7. Ask for explicit natural-language approval. If the user approves only a subset, update the bundle and show the revised scope.
8. After approval, run `greprules auth status`. If login is missing or expired, run the greprules.io login flow above from chat.
9. Run `greprules agent-feedback submit --bundle <feedback-bundle.json> --consent-session <short-session-id>`.
10. If submit reports that greprules login is required, run the same chat login flow and then retry submit only if the user's contribution approval still covers the exact bundle.

False-positive feedback is context-specific precision feedback. Do not describe it as a global rule downvote or direct quality-score penalty.

Rule proposal contribution:

Use this flow only when the user explicitly asks Hermes to turn an independently identified vulnerability into a greprules.io rule proposal. This flow is never automatic and must not run from hooks.

1. Confirm the user wants to publish a public OpenGrep/Semgrep-compatible rule proposal.
2. Run `greprules agent-proposal prepare --out <rule-proposal-bundle.json>`.
3. Edit the bundle with rule YAML, title, description, license, provenance, generated metadata, vulnerable pattern, recommended fix, false-positive notes, and at least one positive and one negative public test fixture.
4. Verify the bundle contains no TODO placeholders and no private repository URLs, raw local file paths, organization secrets, or unapproved source snippets.
5. Preview uploaded fields and excluded fields for the user.
6. Ask for explicit natural-language approval. If scope changes, update the bundle and preview again.
7. After approval, run `greprules auth status`. If login is missing or expired, run the greprules.io login flow above from chat.
8. Run `greprules agent-proposal submit --bundle <rule-proposal-bundle.json> --consent-session <short-session-id>`.
9. If submit reports that greprules login is required, run the same chat login flow and then retry submit only if the user's proposal approval still covers the exact bundle.
