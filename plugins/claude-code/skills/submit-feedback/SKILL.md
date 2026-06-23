---
description: Review a greprules scan result and submit contextual feedback after explicit user approval
---

Use this skill when the user asks `/greprules:submit-feedback`, asks Claude Code to report greprules scan feedback, or wants to contribute true-positive, false-positive, warning, or diagnostic feedback to greprules.io.

This is an explicit contribution workflow, not an automatic scan workflow. Never submit anything from hooks or prior scan output unless the user approves the exact scope in conversation.

Prerequisites:

- A previous greprules `agent-result.json` path. Prefer the `Full result:` path printed by `/greprules:scan-edited`, `/greprules:scan-working-tree`, `/greprules:scan-target`, or `/greprules:scan-full`.
- Browser-approved greprules.io CLI login. If it is missing or expired, run the `/greprules:auth-login` flow from chat instead of asking the user to run a terminal command.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require the wrapper to be on shell `PATH`.
2. Read the requested `agent-result.json`. If no path is clear, ask the user for the result path before continuing.
3. Review findings, warnings, and errors locally. Classify only items with clear reasoning as `true_positive`, `false_positive`, `accepted_risk`, `not_applicable`, or `fixed`.
4. Run `greprules agent-feedback prepare --result <agent-result.json> --out <feedback-bundle.json>`.
5. Read the generated bundle and verify it contains hashed project/finding identifiers rather than raw source code or raw file paths.
6. Add entries to the bundle `feedback` array only for the findings the user wants to submit. Use the existing `rule_slug`, `rule_version`, and `finding_fingerprint` values from the bundle.
7. Show a concise contribution preview:
   - feedback verdicts to submit
   - diagnostics to submit
   - uploaded fields: rule slug, rule version, finding fingerprint, verdict, short message, diagnostic hashes
   - excluded fields: source code, raw file paths, private repository URL, code snippets
8. Ask for explicit approval. If the user approves only a subset, update the bundle and preview the revised scope.
9. After approval, run `greprules auth status`. If login is missing or expired, run `greprules auth login --agent`, immediately show the approval URL/code to the user, wait for approval, and rerun `greprules auth status`. If the installed CLI does not recognize `--agent`, retry with `greprules auth login --no-browser`.
10. Run `greprules agent-feedback submit --bundle <feedback-bundle.json> --consent-session <short-session-id>`.
11. Report the scan id, diagnostics count, and feedback verdict count.

Rules:

- No automatic contribution upload.
- No raw source code, raw file paths, private repository URLs, or code snippets.
- If submit reports that greprules login is required, run the same chat login flow and then retry submit only if the user's contribution approval still covers the exact bundle.
- Treat false positives as context-specific precision feedback, not a global rule rating penalty.
- Do not create or upload rule proposals in this skill.
