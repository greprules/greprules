package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/greprules/greprules/internal/agentstate"
	"github.com/greprules/greprules/internal/config"
)

func setupAgentPluginTestEnv(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	t.Setenv("GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS", "0")
	return root, state
}

func markAgentDirty(t *testing.T, root string, stateDir string, path string) {
	t.Helper()
	state, err := agentstate.New(root, stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := state.MarkDirty([]string{path}); err != nil {
		t.Fatal(err)
	}
}

func configureAgentScanProject(t *testing.T, root string, fakeOpenGrepScript string) {
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
