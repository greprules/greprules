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

func TestClaudeHookMarkDirtyFiltersScanCandidates(t *testing.T) {
	root, state := setupClaudeHookTestEnv(t)
	writeFile(t, filepath.Join(root, "README.md"), "# docs\n")
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")

	var stdout bytes.Buffer
	if err := runClaudeHookWithIO(t.Context(), []string{"mark-dirty"}, strings.NewReader(`{"tool_name":"Edit","tool_input":{"file_path":"README.md"}}`), &stdout); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(state, "dirty")); !os.IsNotExist(err) {
		t.Fatalf("expected README edit not to create dirty marker, err=%v", err)
	}

	if err := runClaudeHookWithIO(t.Context(), []string{"mark-dirty"}, strings.NewReader(`{"tool_name":"Edit","tool_input":{"file_path":"app.mjs"}}`), &stdout); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(state, "dirty-files"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "app.mjs" {
		t.Fatalf("expected app.mjs dirty target, got %q", string(data))
	}
}

func TestClaudeHookScanFatalClearsDirtyAndRecordsAttempt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root, state := setupClaudeHookTestEnv(t)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	configureClaudeHookScanProject(t, root, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
printf 'fatal scan\n' >&2
exit 9
`)

	markClaudeHookDirty(t, "app.mjs")
	var stdout bytes.Buffer
	if err := runClaudeHookWithIO(t.Context(), []string{"scan-if-dirty"}, strings.NewReader(`{}`), &stdout); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON hook output, got %q: %v", stdout.String(), err)
	}
	if payload["continue"] != true || !strings.Contains(payload["systemMessage"].(string), "scan failed") {
		t.Fatalf("unexpected hook output: %#v", payload)
	}
	assertNoFile(t, filepath.Join(state, "dirty"))
	assertNoFile(t, filepath.Join(state, "dirty-files"))
	assertFileExists(t, filepath.Join(state, "last-scan"))
}

func TestClaudeHookScanSuccessBlocksAndClearsDirty(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root, state := setupClaudeHookTestEnv(t)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	configureClaudeHookScanProject(t, root, `#!/bin/sh
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
printf '{"results":[{"check_id":"greprules.example","path":"app.mjs","start":{"line":1,"col":1},"end":{"line":1,"col":2},"extra":{"message":"example","severity":"WARNING","metadata":{"license":"MIT"}}}]}\n' > "$out"
`)

	markClaudeHookDirty(t, "app.mjs")
	var stdout bytes.Buffer
	if err := runClaudeHookWithIO(t.Context(), []string{"scan-if-dirty"}, strings.NewReader(`{}`), &stdout); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON hook output, got %q: %v", stdout.String(), err)
	}
	if payload["decision"] != "block" || !strings.Contains(payload["reason"].(string), "OpenGrep reported 1 finding") {
		t.Fatalf("unexpected hook output: %#v", payload)
	}
	assertNoFile(t, filepath.Join(state, "dirty"))
	assertNoFile(t, filepath.Join(state, "dirty-files"))
	assertFileExists(t, filepath.Join(state, "last-scan"))
	assertFileExists(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
}

func TestClaudeHookStopHookActiveClearsDirty(t *testing.T) {
	root, state := setupClaudeHookTestEnv(t)
	writeFile(t, filepath.Join(root, "app.mjs"), "console.log(1)\n")
	markClaudeHookDirty(t, "app.mjs")

	var stdout bytes.Buffer
	if err := runClaudeHookWithIO(t.Context(), []string{"scan-if-dirty"}, strings.NewReader(`{"stop_hook_active":true}`), &stdout); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("expected no hook output, got %q", stdout.String())
	}
	assertNoFile(t, filepath.Join(state, "dirty"))
	assertNoFile(t, filepath.Join(state, "dirty-files"))
}

func TestClaudeHookTooManyTargetsClearsDirtyAndRecordsAttempt(t *testing.T) {
	root, state := setupClaudeHookTestEnv(t)
	t.Setenv("GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES", "1")
	writeFile(t, filepath.Join(root, "a.mjs"), "console.log(1)\n")
	writeFile(t, filepath.Join(root, "b.ts"), "console.log(2)\n")
	markClaudeHookDirty(t, "a.mjs")
	markClaudeHookDirty(t, "b.ts")

	var stdout bytes.Buffer
	if err := runClaudeHookWithIO(t.Context(), []string{"scan-if-dirty"}, strings.NewReader(`{}`), &stdout); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("expected JSON hook output, got %q: %v", stdout.String(), err)
	}
	if payload["continue"] != true || !strings.Contains(payload["systemMessage"].(string), "exceed the automatic limit") {
		t.Fatalf("unexpected hook output: %#v", payload)
	}
	assertNoFile(t, filepath.Join(state, "dirty"))
	assertNoFile(t, filepath.Join(state, "dirty-files"))
	assertFileExists(t, filepath.Join(state, "last-scan"))
}

func setupClaudeHookTestEnv(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	t.Setenv("GREPRULES_PLUGIN_STATE_DIR", state)
	t.Setenv("GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS", "0")
	return root, state
}

func markClaudeHookDirty(t *testing.T, path string) {
	t.Helper()
	input := `{"tool_name":"Edit","tool_input":{"file_path":` + strconvQuote(path) + `}}`
	if err := runClaudeHookWithIO(t.Context(), []string{"mark-dirty"}, strings.NewReader(input), ioDiscard{}); err != nil {
		t.Fatal(err)
	}
}

func configureClaudeHookScanProject(t *testing.T, root string, fakeOpenGrepScript string) {
	t.Helper()
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
	writeFile(t, fakeOpenGrep, fakeOpenGrepScript)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "go-security", "rules", "example.yaml"), "rules: []\n")
	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
	cfg.OpenGrep.Mode = "path"
	cfg.OpenGrep.Managed = false
	cfg.OpenGrep.Path = fakeOpenGrep
	cfg.OpenGrep.IncludeDefaultRules = false
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	lock := config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      server.URL,
		Packs: []config.LockedPack{{
			ID:         "go-security",
			Version:    "build-1",
			SHA256:     "abc",
			RulePath:   ".greprules/cache/packs/go-security/rules",
			TotalRules: 1,
		}},
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, err=%v", path, err)
	}
}

func assertFileExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func strconvQuote(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}
