package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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

func TestRunFetchWritesLockAndCache(t *testing.T) {
	root := t.TempDir()
	tarball := makePackTarball(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/packs/go-security/manifest.json":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"schema_version":1,"slug":"go-security","build_id":"build-1","total_rules":1,"languages":["go"],"rules":[{"slug":"example","yaml_path":"rules/example.yaml"}]}`))
		case "/api/packs/go-security/latest.tar.gz":
			w.Header().Set("content-type", "application/gzip")
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := runFetch(t.Context(), []string{"--root", root, "--registry", server.URL, "--pack", "go-security"}); err != nil {
		t.Fatal(err)
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packs) != 1 || lock.Packs[0].ID != "go-security" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	if _, err := os.Stat(filepath.Join(root, lock.Packs[0].RulePath, "example.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestRunScanWritesAgentResultWithFakeOpenGrep(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "go-security", "rules", "example.yaml"), "rules: []\n")
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
out=""
fmt="json"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) shift; out="$1" ;;
    --sarif) fmt="sarif" ;;
    --json) fmt="json" ;;
  esac
  shift
done
if [ "$fmt" = "sarif" ]; then
  printf '{"version":"2.1.0","runs":[]}\n' > "$out"
else
  printf '{"results":[{"check_id":"greprules.example","path":"main.go","start":{"line":1,"col":1},"end":{"line":1,"col":2},"extra":{"message":"example","severity":"WARNING","metadata":{"license":"MIT"}}}]}\n' > "$out"
fi
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.OpenGrep.Mode = "path"
	cfg.OpenGrep.Managed = false
	cfg.OpenGrep.Path = fakeOpenGrep
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	lock := config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      "https://example.test",
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
	if err := runScan(t.Context(), []string{"--root", root, "--full"}); err != nil {
		t.Fatal(err)
	}
	agentPath := filepath.Join(root, ".greprules", "out", "agent-result.json")
	data, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status   string `json:"status"`
		Findings []struct {
			RuleID string `json:"ruleId"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || len(result.Findings) != 1 || result.Findings[0].RuleID != "greprules.example" {
		t.Fatalf("unexpected agent result: %s", string(data))
	}
	updatedLock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if updatedLock.Engine == nil || updatedLock.Engine.Mode != "path" || updatedLock.Engine.Version != "9.8.7" {
		t.Fatalf("expected path engine in lockfile, got %#v", updatedLock.Engine)
	}
}

func TestRunInitWritesSystemEngineConfig(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	if err := runInit([]string{"--root", root, "--engine", "system"}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenGrep.Mode != "system" || cfg.OpenGrep.Managed {
		t.Fatalf("expected system engine config, got %#v", cfg.OpenGrep)
	}
}

func TestConfigSetGlobalAndInspect(t *testing.T) {
	root := t.TempDir()
	userConfig := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GREPRULES_USER_CONFIG", userConfig)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")

	if err := runConfigSet([]string{"--root", root, "--global", "opengrep.mode", "system"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigSet([]string{"--root", root, "registry", "http://localhost:8787", "--global"}); err != nil {
		t.Fatal(err)
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Config.OpenGrep.Mode != "system" || resolution.Config.Registry != "http://localhost:8787" {
		t.Fatalf("unexpected effective config: %#v", resolution.Config)
	}
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatal(err)
	}
}

func TestRepoConfigOpenGrepPathIsIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	if err := runConfigSet([]string{"--root", root, "--repo", "opengrep.path", "/tmp/opengrep"}); err == nil {
		t.Fatal("expected repo opengrep.path to be rejected")
	}
	if err := os.MkdirAll(filepath.Join(root, ".greprules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".greprules", "config.yaml"), []byte("opengrep:\n  mode: path\n  path: /tmp/opengrep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Config.OpenGrep.Path != "" {
		t.Fatalf("expected shared opengrep.path to be ignored, got %#v", resolution.Config.OpenGrep)
	}
	if len(resolution.Warnings) == 0 {
		t.Fatal("expected warning for ignored shared opengrep.path")
	}
}

func makePackTarball(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	files := map[string]string{
		"manifest.json":      `{"schema_version":1}`,
		"rules/example.yaml": "rules: []\n",
	}
	for name, content := range files {
		if strings.Contains(name, "..") {
			t.Fatal("bad test fixture")
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
