package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAgentStateMarkDirtyAndPrepareTargets(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	writeFile(t, filepath.Join(root, "src", "app.ts"), "console.log(1)\n")
	writeFile(t, filepath.Join(root, "README.md"), "# docs\n")

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentState([]string{
			"mark-dirty",
			"--root", root,
			"--state-dir", stateDir,
			"--cwd", filepath.Join(root, "src"),
			"--path", "app.ts",
			"--path", filepath.Join(root, "README.md"),
		}); err != nil {
			t.Fatal(err)
		}
	})
	var marked map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &marked); err != nil {
		t.Fatalf("mark-dirty returned non-JSON %q: %v", stdout.String(), err)
	}
	if marked["marked"] != true {
		t.Fatalf("expected mark-dirty to mark state, got %#v", marked)
	}
	files, ok := marked["files"].([]any)
	if !ok || len(files) != 1 || files[0] != filepath.Join("src", "app.ts") {
		t.Fatalf("unexpected marked files: %#v", marked["files"])
	}

	stdout.Reset()
	withStdout(t, &stdout, func() {
		if err := runAgentState([]string{"prepare-targets", "--root", root, "--state-dir", stateDir}); err != nil {
			t.Fatal(err)
		}
	})
	var prepared map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &prepared); err != nil {
		t.Fatalf("prepare-targets returned non-JSON %q: %v", stdout.String(), err)
	}
	targets, ok := prepared["targets"].([]any)
	if !ok || len(targets) != 1 || targets[0] != filepath.Join("src", "app.ts") {
		t.Fatalf("unexpected targets: %#v", prepared["targets"])
	}
	targetsPath, ok := prepared["targetsPath"].(string)
	if !ok || targetsPath == "" {
		t.Fatalf("missing targetsPath: %#v", prepared)
	}
	data, err := os.ReadFile(targetsPath)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSpace(string(data)), filepath.Join("src", "app.ts"); got != want {
		t.Fatalf("targets file = %q, want %q", got, want)
	}
}

func TestRunAgentStateSummarizeUsesProviderNeutralManualWording(t *testing.T) {
	root := t.TempDir()
	resultPath := filepath.Join(root, ".greprules", "out", "agent-result.json")
	writeFile(t, resultPath, `{
  "status": "ok",
  "repo": {},
  "scan": { "targets": ["src/app.ts"] },
  "warnings": [],
  "errors": [],
  "findings": []
}`)

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentState([]string{"summarize", "--root", root, "--label", "target"}); err != nil {
			t.Fatal(err)
		}
	})
	got := stdout.String()
	for _, want := range []string{
		"greprules target scan completed: status=ok, findings=0, warnings=0, errors=0, targets=1.",
		"No OpenGrep findings were reported for this scan.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("summary missing %q: %q", want, got)
		}
	}
}

func withStdout(t *testing.T, writer *bytes.Buffer, fn func()) {
	t.Helper()
	original := os.Stdout
	readPipe, writePipe, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writePipe
	defer func() {
		os.Stdout = original
		_ = writePipe.Close()
		_ = readPipe.Close()
	}()
	fn()
	if err := writePipe.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = original
	if _, err := writer.ReadFrom(readPipe); err != nil {
		t.Fatal(err)
	}
	if err := readPipe.Close(); err != nil {
		t.Fatal(err)
	}
}
