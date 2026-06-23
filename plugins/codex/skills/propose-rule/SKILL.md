---
name: greprules-propose-rule
description: Prepare and submit an agent-generated OpenGrep rule proposal to greprules.io after explicit user approval.
---

# greprules Propose Rule

Use this skill when the user asks Codex to turn an independently identified vulnerability into a greprules.io rule proposal.

## Requirements

- Browser-approved greprules.io CLI login from `greprules auth login`.
- A user-approved vulnerability explanation and a proposed OpenGrep/Semgrep-compatible YAML rule.
- At least one positive and one negative public test fixture.

## Workflow

1. Resolve the greprules command from the installed plugin root and use its bundled `bin/greprules` wrapper.
2. Run `greprules agent-proposal prepare --out <rule-proposal-bundle.json>`.
3. Edit the generated bundle with the proposed rule YAML, title, description, license, provenance, generated metadata, vulnerable pattern, recommended fix, false-positive notes, and tests.
4. Verify the bundle contains no TODO placeholders.
5. Preview the submission for the user:
   - uploaded information: rule YAML, license, provenance, generated_by/generated_at, public test fixtures, source context, consent metadata
   - not uploaded: private repository URLs, raw local file paths, organization secrets, unrelated source code
6. Ask for explicit natural-language approval. If the user changes scope, update the bundle and preview again.
7. After approval, run `greprules agent-proposal submit --bundle <rule-proposal-bundle.json> --consent-session <short-session-id>`.
8. If submit reports that greprules login is required, stop and tell the user to run `greprules auth login` so the browser can approve contribution access.

## Guardrails

- Never submit a proposal automatically from hooks.
- Never submit without positive and negative public test fixtures.
- Do not upload proprietary rule packs, private repository URLs, local paths, secrets, or snippets the user has not approved for public release.
- Explain that accepted proposals enter validation and moderation before public trust, verification, or pack eligibility.
