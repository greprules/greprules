package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/greprules/greprules/internal/config"
)

func TestRunAgentScanDirectScansTargetsFromIntoOutputDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root, state := setupAgentPluginTestEnv(t)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	configureAgentScanProject(t, root, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) shift; out="$1" ;;
  esac
  shift
done
printf '{"results":[]}\n' > "$out"
`)
	targetsPath := filepath.Join(state, "scan-targets.txt")
	outputDir := filepath.Join(state, "out")
	writeFile(t, targetsPath, "app.mjs\n")

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{
			"scan",
			"--root", root,
			"--label", "edited-file",
			"--targets-from", targetsPath,
			"--output-dir", outputDir,
			"--sarif=false",
		}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"greprules edited-file scan completed: status=ok, findings=0, warnings=0, errors=0, targets=1.",
		"No OpenGrep findings were reported for this scan.",
		"Full result: " + filepath.Join(outputDir, "agent-result.json"),
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("agent-scan output missing %q: %q", want, stdout.String())
		}
	}
	assertFileExists(t, filepath.Join(outputDir, "agent-result.json"))
}

func TestRunAgentScanDirectJSONOutputPreservesAutomaticWording(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root, state := setupAgentPluginTestEnv(t)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	configureAgentScanProject(t, root, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) shift; out="$1" ;;
  esac
  shift
done
printf '{"results":[]}\n' > "$out"
`)
	targetsPath := filepath.Join(state, "scan-targets.txt")
	outputDir := filepath.Join(state, "out")
	writeFile(t, targetsPath, "app.mjs\n")

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{
			"scan",
			"--root", root,
			"--label", "edited-file",
			"--automatic",
			"--format", "json",
			"--targets-from", targetsPath,
			"--output-dir", outputDir,
			"--sarif=false",
		}); err != nil {
			t.Fatal(err)
		}
	})
	var outcome agentScanOutcome
	if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatalf("expected JSON outcome, got %q: %v", stdout.String(), err)
	}
	if outcome.Status != "scanned" {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
	for _, want := range []string{
		"greprules automatic edited-file scan completed",
		"No OpenGrep findings were reported for the current automatic scan.",
		filepath.Join(outputDir, "agent-result.json"),
	} {
		if !strings.Contains(outcome.Message, want) {
			t.Fatalf("message missing %q: %q", want, outcome.Message)
		}
	}
}

func TestRunAgentScanDirectRequestsPackSelectionWithTargetsFrom(t *testing.T) {
	root, state := setupAgentPluginTestEnv(t)
	writeFile(t, filepath.Join(root, "query.sql"), "select * from users\n")
	targetsPath := filepath.Join(state, "scan-targets.txt")
	writeFile(t, targetsPath, "query.sql\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/packs" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
exit 0
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
	cfg.OpenGrep.Mode = "path"
	cfg.OpenGrep.Managed = false
	cfg.OpenGrep.Path = fakeOpenGrep
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{
			"scan",
			"--root", root,
			"--label", "edited-file",
			"--automatic",
			"--format", "json",
			"--targets-from", targetsPath,
			"--output-dir", filepath.Join(state, "out"),
		}); err != nil {
			t.Fatal(err)
		}
	})
	var outcome agentScanOutcome
	if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatalf("expected JSON outcome, got %q: %v", stdout.String(), err)
	}
	if outcome.Status != "needs_pack_selection" {
		t.Fatalf("expected needs_pack_selection, got %#v", outcome)
	}
	for _, want := range []string{"greprules recommend", "--agent", "--targets-from", targetsPath, "greprules fetch", "--pack <slug>", "Do not invent pack slugs"} {
		if !strings.Contains(outcome.Message, want) {
			t.Fatalf("message missing %q: %q", want, outcome.Message)
		}
	}
	if strings.Contains(outcome.Message, "--exact-root") {
		t.Fatalf("pack selection guidance should not mention --exact-root: %q", outcome.Message)
	}
	if strings.Contains(outcome.Message, "--output-dir") {
		t.Fatalf("recommend guidance should not include scan-only output-dir: %q", outcome.Message)
	}
}

func TestRunAgentScanDirectUsesProvidedRootOutsideWorkingTreeMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	parent := t.TempDir()
	if err := exec.Command("git", "-C", parent, "init").Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	root := filepath.Join(parent, "packages", "api")
	state := filepath.Join(root, "state")
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test/api\n")
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	configureAgentScanProject(t, root, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
out=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) shift; out="$1" ;;
  esac
  shift
done
printf '{"results":[]}\n' > "$out"
`)
	targetsPath := filepath.Join(state, "scan-targets.txt")
	outputDir := filepath.Join(state, "out")
	writeFile(t, targetsPath, filepath.Join(root, "app.mjs")+"\n")

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{
			"scan",
			"--root", root,
			"--label", "edited-file",
			"--targets-from", targetsPath,
			"--output-dir", outputDir,
			"--sarif=false",
		}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout.String(), "targets=1") {
		t.Fatalf("expected scan of subproject target from provided root, got %q", stdout.String())
	}
	assertFileExists(t, filepath.Join(outputDir, "agent-result.json"))
	assertNoFile(t, filepath.Join(parent, ".greprules", "lock.json"))
}

func TestRunAgentScanEditedCommandRemoved(t *testing.T) {
	err := runAgentScan(t.Context(), []string{"edited"})
	if err == nil || !strings.Contains(err.Error(), "unknown agent-scan command: edited") {
		t.Fatalf("expected edited subcommand to be removed, got %v", err)
	}
}
