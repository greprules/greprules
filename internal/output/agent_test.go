package output

import (
	"os"
	"path/filepath"
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
