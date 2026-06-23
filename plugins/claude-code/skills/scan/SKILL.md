---
description: Scan edits, git changes, paths, or the full repo
---

# greprules Scan

Use this skill when the user asks `/greprules:scan`, asks Claude Code to scan with greprules, describes a greprules scan target in natural language, asks to contribute scan feedback, or asks to turn an independently identified vulnerability into a greprules.io rule proposal.

Choose scan scope from the user's words:

- Edited/session changes: "edited files", "my changes in this session", "latest edits" -> run the edited-file hook scan.
- Git changes: "changed files", "working tree", "staged", "untracked", "diff" -> run a working-tree scan.
- Explicit targets: file or directory paths such as `src/auth`, `server/app.js`, `.` with a focused target intent -> scan those paths.
- Full repository: "full repo", "entire repository", "everything", "repo-wide" -> run a full scan.
- Ambiguous "scan" with no target: prefer edited-file scan when edited files are tracked; otherwise scan the git working tree. Do not default to a full repository scan unless the user asks for broad coverage.

When handling an automatic Stop hook result, read the `Full result:` path reported in the scan summary and any relevant project context needed to classify each finding, then report reasoning only. Do not edit code, add suppressions, chase zero findings, or rerun greprules unless the user explicitly asks.

Workflow:

1. Resolve the plugin root and use bundled `bin/greprules`; shell `PATH` is optional.
2. Run `greprules agent-status --format json`.
3. If `opengrep.active.ok` is false, use `/greprules:configure` to prepare the managed OpenGrep runtime before scanning.
4. Run the selected scan command:
   - Edited files: `python3 <plugin-root>/scripts/greprules-hook.py scan-edited`
   - Git changes: `greprules agent-scan scan --changed`
   - Explicit targets: `greprules agent-scan scan <path> [<path>...]`
   - Full repository: `greprules agent-scan scan`
5. If the scan returns `needs_pack_selection`, inspect `selectionContext.detection`, `selectionContext.targets`, `selectionContext.availablePacks`, and `selectionContext.candidates`; choose explicit pack slugs that match the selected scope, fetch them with `greprules fetch <slug> [<slug>...]`, then rerun the same scan. Do not invent pack slugs.
6. If OpenGrep is still not ready, stop and summarize `recommendedCommands`.
7. Read the `Full result:` path reported in the scan summary.
8. Summarize findings by rule id, severity, file, line, and message.
9. Classify findings as true positive, false positive, or needs investigation. Do not edit code, add suppressions, upload rules, submit feedback, or create rule drafts unless the user explicitly asks.
10. For manual scan/review requests, if the findings include justified true-positive, false-positive, accepted-risk, fixed, not-applicable, warning, or diagnostic signal, offer to contribute that context to greprules.io. Keep the offer short and do not submit anything until the user approves the exact scope in conversation.

Community feedback flow:

1. Use a previous `Full result:` path from the scan output. If no result path is clear, ask for the `agent-result.json` path.
2. Run `greprules agent-feedback prepare --result <agent-result.json> --out <feedback-bundle.json>`.
3. Read the generated bundle and verify it contains hashed project/finding identifiers, not raw source code or raw file paths.
4. Add entries to the bundle `feedback` array only for items the user wants to submit. Each entry must use the bundle finding's `rule_slug`, `rule_version`, and `finding_fingerprint`.
5. Preview the exact submission scope:
   - uploaded: rule slug, rule version, finding fingerprint, verdict, short message, and scan diagnostic hashes
   - not uploaded: source code, raw file paths, private repository URL, or code snippets
6. Ask for explicit natural-language approval. If the user changes scope, update the bundle and preview again.
7. After approval, run `greprules auth status`. If login is missing or expired, use `/greprules:auth-login` from chat.
8. Run `greprules agent-feedback submit --bundle <feedback-bundle.json> --consent-session <short-session-id>`.
9. Report the scan id, number of findings/diagnostics submitted, and number of feedback verdicts submitted.

Rule proposal flow:

Use this only when the user asks Claude Code to turn an independently identified vulnerability into a public greprules.io rule proposal. Do not start it automatically from hook output.

1. Confirm the user wants to publish a public OpenGrep/Semgrep-compatible rule proposal.
2. Run `greprules agent-proposal prepare --out <rule-proposal-bundle.json>`.
3. Edit the bundle with rule YAML, title, description, license, provenance, generated metadata, vulnerable pattern, recommended fix, false-positive notes, and at least one positive and one negative public test fixture.
4. Verify the bundle contains no TODO placeholders and no private repository URLs, raw local file paths, organization secrets, or unapproved source snippets.
5. Preview uploaded and excluded fields for the user.
6. Ask for explicit natural-language approval. If scope changes, update the bundle and preview again.
7. After approval, run `greprules auth status`. If login is missing or expired, use `/greprules:auth-login` from chat.
8. Run `greprules agent-proposal submit --bundle <rule-proposal-bundle.json> --consent-session <short-session-id>`.
9. Explain that accepted proposals enter validation and moderation before public trust, verification, or pack eligibility.

Community contribution rules:

- Never submit feedback or proposals from automatic hook output without a user approval turn.
- Never upload raw source code, raw file paths, private repository URLs, organization secrets, or unapproved snippets.
- False-positive feedback means "false positive in this context", not a global rule downvote.
- If submit reports that greprules login is required, run the chat login flow and retry only if the user's contribution approval still covers the exact bundle.

Fallbacks:

- If an edited-file scan has no tracked files, run a working-tree scan when in a git repo; otherwise ask for a target path.
- If a target path does not exist, report the missing path and ask for the corrected target.
- If working-tree scope is requested outside a git repo, ask whether to scan explicit paths or the full directory.
- If registry access fails, report the registry URL and the error from `agent-status --format json`.
