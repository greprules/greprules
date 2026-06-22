package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
)

func setupAgentPluginTestEnv(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	state := filepath.Join(root, "state")
	return root, state
}

func withStdout(t *testing.T, buffer *bytes.Buffer, fn func()) {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
		_ = reader.Close()
	}()
	fn()
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(buffer, reader); err != nil {
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
	configureTestManagedOpenGrep(t, fakeOpenGrep)
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "go-security", "rules", "example.yaml"), "rules: []\n")
	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
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
		Engine: testLockedEngine(fakeOpenGrep),
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}
}

func testLockedEngine(path string) *config.LockedEngine {
	return &config.LockedEngine{
		Name:    "opengrep",
		Mode:    "managed",
		Version: "9.8.7",
		Path:    path,
		Source:  "test",
		Managed: true,
	}
}

func configureTestManagedOpenGrep(t *testing.T, path string) {
	t.Helper()
	t.Setenv("GREPRULES_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	cacheRoot, err := opengrep.DefaultCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	versionDir := filepath.Join(cacheRoot, "9.8.7")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeInfo := opengrep.Runtime{
		Name:    "opengrep",
		Mode:    "managed",
		Version: "9.8.7",
		Path:    path,
		Source:  "test",
		Managed: true,
	}
	if err := opengrep.SaveRuntime(filepath.Join(versionDir, "runtime.json"), runtimeInfo); err != nil {
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
