package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/greprules/greprules/internal/config"
)

func TestRunAgentScanEditedScansTrackedStateAndPrintsSummary(t *testing.T) {
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

	markAgentDirty(t, root, state, "app.mjs")
	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{"edited", "--root", root, "--state-dir", state, "--label", "edited-file", "--sarif=false"}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"greprules edited-file scan completed: status=ok, findings=0, warnings=0, errors=0, targets=1.",
		"No OpenGrep findings were reported for this scan.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("agent-scan output missing %q: %q", want, stdout.String())
		}
	}
	assertNoFile(t, filepath.Join(state, "dirty"))
	assertNoFile(t, filepath.Join(state, "dirty-files"))
	assertFileExists(t, filepath.Join(state, "last-scan"))
}

func TestRunAgentScanEditedTooManyTargetsClearsState(t *testing.T) {
	root, state := setupAgentPluginTestEnv(t)
	writeFile(t, filepath.Join(root, "a.mjs"), "console.log(1)\n")
	writeFile(t, filepath.Join(root, "b.ts"), "console.log(2)\n")
	markAgentDirty(t, root, state, "a.mjs")
	markAgentDirty(t, root, state, "b.ts")

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{
			"edited",
			"--root", root,
			"--state-dir", state,
			"--automatic",
			"--max-targets", "1",
			"--too-many-suggestion", "Run /greprules scan-edited when ready.",
		}); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{
		"greprules automatic edited-file scan skipped because 2 edited files exceed the automatic limit (1).",
		"Run /greprules scan-edited when ready.",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("agent-scan output missing %q: %q", want, stdout.String())
		}
	}
	assertNoFile(t, filepath.Join(state, "dirty"))
	assertNoFile(t, filepath.Join(state, "dirty-files"))
	assertFileExists(t, filepath.Join(state, "last-scan"))
}

func TestRunAgentScanEditedJSONOutput(t *testing.T) {
	root, state := setupAgentPluginTestEnv(t)
	writeFile(t, filepath.Join(root, "a.mjs"), "console.log(1)\n")
	writeFile(t, filepath.Join(root, "b.ts"), "console.log(2)\n")
	markAgentDirty(t, root, state, "a.mjs")
	markAgentDirty(t, root, state, "b.ts")

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runAgentScan(t.Context(), []string{
			"edited",
			"--root", root,
			"--state-dir", state,
			"--automatic",
			"--format", "json",
			"--max-targets", "1",
			"--too-many-message", "too many: {count}/{limit}",
		}); err != nil {
			t.Fatal(err)
		}
	})
	var outcome agentScanOutcome
	if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatalf("expected JSON outcome, got %q: %v", stdout.String(), err)
	}
	if outcome.Status != "skipped" || outcome.Message != "too many: 2/1" {
		t.Fatalf("unexpected outcome: %#v", outcome)
	}
	if got := strings.Join(outcome.Targets, ","); got != "a.mjs,b.ts" {
		t.Fatalf("unexpected targets: %#v", outcome.Targets)
	}
}

func TestRunScanEditedKeepsTrackedStateWhenReadinessFails(t *testing.T) {
	root, state := setupAgentPluginTestEnv(t)
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/packs" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
	cfg.OpenGrep.Mode = "path"
	cfg.OpenGrep.Managed = false
	cfg.OpenGrep.Path = filepath.Join(root, "missing-opengrep")
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	markAgentDirty(t, root, state, "app.mjs")
	err := runScanEdited(t.Context(), []string{"--root", root, "--state-dir", state})
	if err == nil || !strings.Contains(err.Error(), "scan skipped") {
		t.Fatalf("expected handled readiness/fetch error, got %v", err)
	}
	assertFileExists(t, filepath.Join(state, "dirty"))
	assertFileExists(t, filepath.Join(state, "dirty-files"))
	if _, err := os.Stat(filepath.Join(state, "last-scan")); !os.IsNotExist(err) {
		t.Fatalf("expected last-scan not to be recorded, err=%v", err)
	}
}
