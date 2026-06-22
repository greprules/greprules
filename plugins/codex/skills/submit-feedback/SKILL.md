---
name: greprules-submit-feedback
description: Review a greprules scan result and, only after explicit user approval, submit contextual feedback to greprules.io.
---

# greprules Submit Feedback

Use this skill when the user asks Codex to contribute scan feedback, report true positives or false positives to greprules.io, or share scan warnings/diagnostics with the greprules community.

This skill is never automatic. Do not run it from hooks, and do not submit anything unless the user explicitly approves the exact contribution scope in conversation.

Prerequisites:

- A previous greprules scan result path, usually the `Full result:` path printed by `$greprules-scan-edited`, `$greprules-scan-working-tree`, `$greprules-scan-target`, or `$greprules-scan-full`.
- An authenticated greprules.io API key available as `GREPRULES_API_KEY` or provided by the user for this action.

Workflow:

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper. Do not require `greprules` to be installed on shell `PATH`.
2. Read the requested `agent-result.json`. If the user did not provide a path, use the most recent `Full result:` path from the current conversation. If there is no clear path, ask for it.
3. Review findings, warnings, and errors locally. Classify only items you can justify as `true_positive`, `false_positive`, `accepted_risk`, `not_applicable`, or `fixed`.
4. Run `greprules agent-feedback prepare --result <agent-result.json> --out <feedback-bundle.json>`.
5. Read the generated feedback bundle. It must contain hashed project/finding identifiers, not raw source code or raw file paths.
6. Edit the bundle's `feedback` array only for items the user wants to submit. Each entry must use the bundle finding's `rule_slug`, `rule_version`, and `finding_fingerprint`.
7. Before submitting, show a concise preview:
   - items to submit
   - uploaded information: rule slug, rule version, finding fingerprint, verdict, short message, scan diagnostic hashes
   - not uploaded: source code, raw file paths, private repository URL, code snippets
8. Ask for explicit approval. Accept natural-language approvals such as "submit these", "only submit the false positives", or "exclude that item". If the user changes the scope, update the bundle and show the revised scope before submitting.
9. After approval, run `greprules agent-feedback submit --bundle <feedback-bundle.json> --consent-session <short-session-id>`.
10. Report the scan id, number of findings/diagnostics submitted, and number of feedback verdicts submitted.

Rules:

- Never submit feedback from automatic hook output without a user approval turn.
- Never upload raw source code, raw file paths, private repository URLs, or code snippets.
- If `GREPRULES_API_KEY` is missing, stop and tell the user that authenticated greprules.io contribution requires a login/API key.
- False-positive feedback means "false positive in this context", not a global rule downvote.
- Do not create rule proposals in this skill; use `$greprules-propose-rule` for agent-generated rule proposals.
