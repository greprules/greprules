package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/greprules/greprules/internal/rules"
)

func TestProposalPrepareWritesEditableBundle(t *testing.T) {
	root := t.TempDir()
	bundlePath := filepath.Join(root, "rule-proposal-bundle.json")

	if err := RunProposalCommand(context.Background(), []string{
		"prepare",
		"--out", bundlePath,
		"--title", "Agent SQL injection rule",
		"--rule-id", "agent.sql-injection",
		"--language", "python",
	}, "vtest"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	var bundle RuleProposalBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.SchemaVersion != ruleProposalBundleSchemaVersion {
		t.Fatalf("unexpected schema: %s", bundle.SchemaVersion)
	}
	if bundle.Proposal.Metadata.SourceType != "agent_generated" || bundle.Proposal.Metadata.SourceContext != "agent_context" {
		t.Fatalf("unexpected proposal metadata: %#v", bundle.Proposal.Metadata)
	}
	if !hasRuleProposalTest(bundle.Proposal.Metadata.Tests, "positive") || !hasRuleProposalTest(bundle.Proposal.Metadata.Tests, "negative") {
		t.Fatalf("expected positive and negative tests: %#v", bundle.Proposal.Metadata.Tests)
	}
}

func TestProposalSubmitRejectsPlaceholders(t *testing.T) {
	root := t.TempDir()
	bundle := NewRuleProposalTemplate("Agent SQL injection rule", "agent.sql-injection", "python", "MIT")
	bundlePath := filepath.Join(root, "rule-proposal-bundle.json")
	if err := writeJSONFile(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	err := RunProposalCommand(context.Background(), []string{
		"submit",
		"--bundle", bundlePath,
		"--consent-session", "session-123456",
		"--api-key", "test-key",
		"--registry", "http://127.0.0.1:1",
	}, "vtest")
	if err == nil {
		t.Fatal("expected placeholder validation error")
	}
}

func TestProposalSubmitPostsAgentGeneratedRule(t *testing.T) {
	root := t.TempDir()
	bundle := completedRuleProposalBundle()
	bundlePath := filepath.Join(root, "rule-proposal-bundle.json")
	if err := writeJSONFile(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	var sawRuleUpload bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/rules" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("authorization") != "Bearer test-key" {
			t.Fatalf("missing bearer token")
		}
		sawRuleUpload = true
		var request rules.RuleUploadRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Metadata.SourceType != "agent_generated" || request.Metadata.AgentConsent == nil {
			t.Fatalf("missing agent metadata or consent: %#v", request.Metadata)
		}
		if !hasRuleProposalTest(request.Metadata.Tests, "positive") || !hasRuleProposalTest(request.Metadata.Tests, "negative") {
			t.Fatalf("missing proposal tests: %#v", request.Metadata.Tests)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"rule":{"slug":"agent-sql-injection","validation_status":"queued"},"version":{"version":"1.0.0","validation_status":"queued"},"validation":{"status":"queued"}}`))
	}))
	defer server.Close()

	err := RunProposalCommand(context.Background(), []string{
		"submit",
		"--bundle", bundlePath,
		"--consent-session", "session-123456",
		"--api-key", "test-key",
		"--registry", server.URL,
	}, "vtest")
	if err != nil {
		t.Fatal(err)
	}
	if !sawRuleUpload {
		t.Fatal("expected rule upload request")
	}
}

func TestRuleUpdateUsesAuthenticatedPut(t *testing.T) {
	var sawPut bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/rules/agent-sql-injection" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPut {
			t.Fatalf("method = %s, want PUT", r.Method)
		}
		if r.Header.Get("authorization") != "Bearer test-key" {
			t.Fatalf("missing bearer token")
		}
		sawPut = true
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"rule":{"slug":"agent-sql-injection","validation_status":"queued"},"version":{"version":"1.0.1","validation_status":"queued"},"validation":{"status":"queued"}}`))
	}))
	defer server.Close()

	client := rules.NewRegistry(server.URL)
	if _, err := client.UpdateRule(context.Background(), "test-key", "agent-sql-injection", completedRuleProposalBundle().Proposal); err != nil {
		t.Fatal(err)
	}
	if !sawPut {
		t.Fatal("expected authenticated PUT request")
	}
}

func completedRuleProposalBundle() RuleProposalBundle {
	bundle := NewRuleProposalTemplate("Agent SQL injection rule", "agent.sql-injection", "python", "MIT")
	bundle.Proposal.Description = "Detects direct string formatting in SQL execution calls."
	bundle.Proposal.YAML = `rules:
  - id: agent.sql-injection
    languages: [python]
    severity: ERROR
    message: SQL query uses formatted user input.
    pattern: cursor.execute(f"...{$USER_INPUT}...")
`
	bundle.Proposal.Metadata.Source = "Approved agent analysis of a local scan result."
	bundle.Proposal.Metadata.VulnerablePattern = "SQL execution receives formatted user input."
	bundle.Proposal.Metadata.RecommendedFix = "Use parameterized queries instead of string formatting."
	bundle.Proposal.Metadata.FalsePositiveNotes = "Safe when the formatted value is a constant controlled by the application."
	bundle.Proposal.Metadata.Tests = []rules.RuleUploadTestFixture{
		{
			Name:     "detects formatted SQL execution",
			Kind:     "positive",
			Language: "python",
			Code:     "cursor.execute(f\"SELECT * FROM users WHERE id = {user_id}\")",
			Expected: "Rule should report one finding.",
		},
		{
			Name:     "ignores parameterized SQL execution",
			Kind:     "negative",
			Language: "python",
			Code:     "cursor.execute(\"SELECT * FROM users WHERE id = ?\", (user_id,))",
			Expected: "Rule should report no findings.",
		},
	}
	return bundle
}
