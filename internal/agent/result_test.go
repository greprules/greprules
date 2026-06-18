package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindingsFromOpenGrepJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.json")
	content := `{
  "results": [
    {
      "check_id": "greprules.example",
      "path": "main.go",
      "start": {"line": 10, "col": 2},
      "end": {"line": 10, "col": 9},
      "extra": {
        "message": "example finding",
        "severity": "WARNING",
        "metadata": {"license": "MIT"}
      }
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	findings, err := FindingsFromOpenGrepJSON(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].RuleID != "greprules.example" || findings[0].Start.Line != 10 {
		t.Fatalf("unexpected findings: %#v", findings)
	}
}

func TestSummarizeAgentResultPreservesAutomaticScanWordingAndPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-result.json")
	content := `{
  "schemaVersion": "greprules.agentResult.v1",
  "status": "ok",
  "repo": {"root": "/tmp/project", "changedMode": false},
  "packs": [],
  "engine": {"name": "opengrep"},
  "scan": {"targets": ["main.go"]},
  "findings": []
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	summary := SummarizeAgentResult(path, SummaryOptions{Automatic: true, Label: "edited-file"})
	for _, want := range []string{
		"greprules automatic edited-file scan completed: status=ok, findings=0, warnings=0, errors=0, targets=1.",
		"No OpenGrep findings were reported for the current automatic scan.",
		"Full result: " + path,
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %q", want, summary)
		}
	}
}
