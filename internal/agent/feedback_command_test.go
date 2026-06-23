package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/greprules/greprules/internal/auth"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/rules"
)

func TestFeedbackPrepareBuildsRedactedBundle(t *testing.T) {
	root, resultPath := writeFeedbackTestResult(t)
	bundlePath := filepath.Join(root, "feedback-bundle.json")

	if err := RunFeedbackCommand(context.Background(), []string{"prepare", "--result", resultPath, "--out", bundlePath}, "vtest"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(bundlePath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Contains(text, "src/app.py") || strings.Contains(text, "unsafe query") {
		t.Fatalf("bundle leaked raw finding context: %s", text)
	}
	var bundle FeedbackBundle
	if err := json.Unmarshal(data, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Scan.GreprulesVersion != "vtest" {
		t.Fatalf("unexpected greprules version: %s", bundle.Scan.GreprulesVersion)
	}
	if !strings.HasPrefix(bundle.Scan.SubmissionHash, "sha256:") || bundle.Scan.SubmissionHash != bundle.ResultHash {
		t.Fatalf("unexpected submission hash: scan=%s result=%s", bundle.Scan.SubmissionHash, bundle.ResultHash)
	}
	if got, want := len(bundle.Findings), 1; got != want {
		t.Fatalf("findings count = %d, want %d", got, want)
	}
	finding := bundle.Findings[0]
	if finding.RuleSlug != "python-sql-injection" {
		t.Fatalf("unexpected rule slug: %s", finding.RuleSlug)
	}
	if !strings.HasPrefix(finding.PathHash, "sha256:") || !strings.HasPrefix(finding.MessageHash, "sha256:") {
		t.Fatalf("expected sha256 hashes, got path=%s message=%s", finding.PathHash, finding.MessageHash)
	}
	if got, want := len(bundle.Diagnostics), 1; got != want {
		t.Fatalf("diagnostics count = %d, want %d", got, want)
	}
	if !strings.HasPrefix(bundle.Diagnostics[0].DiagnosticFingerprint, "sha256:") {
		t.Fatalf("expected diagnostic fingerprint, got %s", bundle.Diagnostics[0].DiagnosticFingerprint)
	}
}

func TestFeedbackSubmitPostsScanDiagnosticsAndFindingFeedback(t *testing.T) {
	root, resultPath := writeFeedbackTestResult(t)
	bundle, err := BuildFeedbackBundle(resultPath, "vtest")
	if err != nil {
		t.Fatal(err)
	}
	bundle.Feedback = []PreparedFindingFeedback{{
		RuleSlug:           bundle.Findings[0].RuleSlug,
		RuleVersion:        bundle.Findings[0].RuleVersion,
		FindingFingerprint: bundle.Findings[0].FindingFingerprint,
		Verdict:            "false_positive",
		Message:            "Test-only route is not reachable in production.",
	}}
	bundlePath := filepath.Join(root, "feedback-bundle.json")
	if err := writeJSONFile(bundlePath, bundle); err != nil {
		t.Fatal(err)
	}

	var sawScan, sawDiagnostics, sawFeedback bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("authorization") != "Bearer test-token" {
			t.Fatalf("missing bearer token on %s", r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		switch r.URL.Path {
		case "/api/scans":
			sawScan = true
			var request rules.ScanCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			encoded, _ := json.Marshal(request)
			if strings.Contains(string(encoded), "src/app.py") || strings.Contains(string(encoded), "unsafe query") {
				t.Fatalf("scan request leaked raw finding context: %s", string(encoded))
			}
			if request.Consent.Mode != "explicit_user_approval" || request.Consent.RedactionPolicy != "no_source_code" {
				t.Fatalf("unexpected consent: %#v", request.Consent)
			}
			if request.Scan.SubmissionHash != bundle.ResultHash {
				t.Fatalf("unexpected submission hash: %s", request.Scan.SubmissionHash)
			}
			_, _ = w.Write([]byte(`{"success":true,"scan_id":"scan-1","findings":[{"id":"finding-1","rule_slug":"python-sql-injection","rule_version":"1.0.0","finding_fingerprint":"` + bundle.Findings[0].FindingFingerprint + `"}]}`))
		case "/api/scan-diagnostics":
			sawDiagnostics = true
			var request rules.ScanDiagnosticCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.Diagnostics[0].DiagnosticFingerprint == "" {
				t.Fatalf("missing diagnostic fingerprint: %#v", request.Diagnostics[0])
			}
			_, _ = w.Write([]byte(`{"success":true,"diagnostic_ids":["diag-1"]}`))
		case "/api/rules/python-sql-injection/findings/feedback":
			sawFeedback = true
			var request rules.FindingFeedbackCreateRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			if request.FindingID != "finding-1" || request.Verdict != "false_positive" {
				t.Fatalf("unexpected feedback request: %#v", request)
			}
			_, _ = w.Write([]byte(`{"success":true,"feedback_id":"feedback-1","finding_id":"finding-1","verdict":"false_positive"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	if err := auth.StoreToken(server.URL, "test-token", time.Now().Add(time.Hour).UTC().Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}

	err = RunFeedbackCommand(context.Background(), []string{
		"submit",
		"--bundle", bundlePath,
		"--consent-session", "session-123456",
		"--registry", server.URL,
	}, "vtest")
	if err != nil {
		t.Fatal(err)
	}
	if !sawScan || !sawDiagnostics || !sawFeedback {
		t.Fatalf("expected all API calls, saw scan=%t diagnostics=%t feedback=%t", sawScan, sawDiagnostics, sawFeedback)
	}
}

func writeFeedbackTestResult(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GREPRULES_STATE_HOME", filepath.Join(root, "state"))
	manifestPath := filepath.Join(root, ".greprules", "cache", "packs", "python-security", "manifest.json")
	writeScanServiceTestFile(t, manifestPath, `{
  "schema_version": 1,
  "slug": "python-security",
  "name": "Python Security",
  "summary": "",
  "source_type": "community",
  "generated_at": "2026-06-22T00:00:00Z",
  "build_id": "build-1",
  "total_rules": 1,
  "languages": ["python"],
  "rules": [{
    "slug": "python-sql-injection",
    "title": "Python SQL Injection",
    "rule_id": "python.sql-injection",
    "canonical_rule_ids": ["python.sql-injection"],
    "original_rule_id": "python.sql-injection",
    "rule_namespace": "global",
    "yaml_path": "rules/python-sql-injection.yaml",
    "language": "python",
    "framework": "",
    "severity": "high",
    "confidence": "medium",
    "license": "MIT",
    "cve": [],
    "cwe": ["CWE-89"],
    "tags": [],
    "source_repo": "",
    "source_commit": "",
    "version": "1.0.0"
  }]
}`)
	if err := config.SaveLock(root, config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      "https://api.greprules.io",
		Packs: []config.LockedPack{{
			ID:           "python-security",
			Version:      "build-1",
			ManifestPath: manifestPath,
			RulePath:     filepath.Join(root, ".greprules", "cache", "packs", "python-security", "rules"),
			TotalRules:   1,
			Languages:    []string{"python"},
		}},
	}); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, "agent-result.json")
	result := AgentResult{
		SchemaVersion: "greprules.agent.v1",
		Status:        "ok",
		Repo:          RepoInfo{Root: root},
		Packs:         []PackInfo{{ID: "python-security", Version: "build-1", TotalRules: 1}},
		Engine:        EngineInfo{Name: "opengrep", Version: "1.23.0", Managed: true},
		Scan:          ScanInfo{Targets: []string{"."}},
		Findings: []Finding{{
			RuleID:   "python.sql-injection",
			Path:     "src/app.py",
			Start:    Location{Line: 10, Col: 2},
			End:      Location{Line: 10, Col: 24},
			Message:  "unsafe query",
			Severity: "ERROR",
		}},
		Warnings: []string{"OpenGrep diagnostic: type=ParseError path=src/app.py message=parse warning"},
	}
	if err := WriteAgentResult(resultPath, result); err != nil {
		t.Fatal(err)
	}
	return root, resultPath
}
