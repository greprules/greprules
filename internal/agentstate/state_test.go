package agentstate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStateMarkDirtyFromFiltersAndNormalizesCandidates(t *testing.T) {
	root := t.TempDir()
	state, err := New(root, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	mkdirAll(t, filepath.Join(root, "src"))
	mkdirAll(t, filepath.Join(root, "node_modules", "pkg"))
	writeTestFile(t, filepath.Join(root, "README.md"), "# docs\n")
	writeTestFile(t, filepath.Join(root, "src", "app.mjs"), "console.log(1)\n")
	writeTestFile(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "console.log(1)\n")

	files, err := state.MarkDirtyFrom([]string{
		"app.mjs",
		"app.mjs",
		filepath.Join(root, "README.md"),
		filepath.Join(root, "node_modules", "pkg", "index.js"),
		filepath.Join(root, "missing.ts"),
	}, filepath.Join(root, "src"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(files, ","), filepath.Join("src", "app.mjs"); got != want {
		t.Fatalf("dirty files = %q, want %q", got, want)
	}
	data, err := os.ReadFile(state.DirtyFilesPath())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(data)), filepath.Join("src", "app.mjs"); got != want {
		t.Fatalf("dirty file content = %q, want %q", got, want)
	}
	assertStateFileExists(t, state.DirtyMarkerPath())
}

func TestStatePrepareScanTargetsRevalidatesDirtyFiles(t *testing.T) {
	root := t.TempDir()
	state, err := New(root, filepath.Join(root, "state"))
	if err != nil {
		t.Fatal(err)
	}
	mkdirAll(t, filepath.Join(root, "src"))
	writeTestFile(t, filepath.Join(root, "src", "keep.ts"), "console.log(1)\n")
	writeTestFile(t, filepath.Join(root, "src", "drop.md"), "# docs\n")
	if err := os.WriteFile(state.DirtyFilesPath(), []byte("src/drop.md\nsrc/keep.ts\nmissing.js\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	targets, err := state.PrepareScanTargets()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(targets, ","), filepath.Join("src", "keep.ts"); got != want {
		t.Fatalf("targets = %q, want %q", got, want)
	}
	data, err := os.ReadFile(state.ScanTargetsPath())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(data)), filepath.Join("src", "keep.ts"); got != want {
		t.Fatalf("scan targets content = %q, want %q", got, want)
	}
}

func TestSummarizeAgentResultPreservesAutomaticScanWording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-result.json")
	if err := os.WriteFile(path, []byte(`{
  "status": "ok",
  "repo": {},
  "scan": { "targets": ["src/app.ts"] },
  "warnings": [],
  "errors": [],
  "findings": []
}`), 0o644); err != nil {
		t.Fatal(err)
	}

	summary := SummarizeAgentResult(path, SummaryOptions{Automatic: true})
	for _, want := range []string{
		"greprules automatic edited-file scan completed: status=ok, findings=0, warnings=0, errors=0, targets=1.",
		"No OpenGrep findings were reported for the current automatic scan.",
		"Full result: .greprules/out/agent-result.json",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %q", want, summary)
		}
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func writeTestFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertStateFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}
