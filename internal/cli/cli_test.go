package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
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
	"github.com/greprules/greprules/internal/doctor"
	"github.com/greprules/greprules/internal/recommend"
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

func TestRunRecommendAgentContextUsesTargetSignals(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[project]\ndependencies = [\"fastapi\"]\n")
	writeFile(t, filepath.Join(root, "src", "main.py"), "from fastapi import FastAPI\napp = FastAPI()\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/packs" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[{"slug":"python-web-security","name":"Python Web Security","languages":["python"]}]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := runRecommend(t.Context(), []string{"--root", root, "--registry", server.URL, "--format", "json", "--agent", "--target", "src/main.py"}); err != nil {
			t.Fatal(err)
		}
	})
	var context recommend.AgentContext
	if err := json.Unmarshal(stdout.Bytes(), &context); err != nil {
		t.Fatalf("expected agent context JSON, got %q: %v", stdout.String(), err)
	}
	if context.SchemaVersion != "greprules.recommend.agent.v1" {
		t.Fatalf("unexpected schema: %#v", context)
	}
	if got := strings.Join(recommend.PackIDs(context.Candidates), ","); got != "python-web-security" {
		t.Fatalf("expected python web candidate, got %#v", context.Candidates)
	}
	if len(context.AvailablePacks) != 1 || context.AvailablePacks[0].Slug != "python-web-security" {
		t.Fatalf("expected available packs in agent context, got %#v", context.AvailablePacks)
	}
}

func TestRunFetchUsesTargetAwareRecommendation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[project]\ndependencies = [\"fastapi\"]\n")
	writeFile(t, filepath.Join(root, "src", "main.py"), "from fastapi import FastAPI\napp = FastAPI()\n")
	tarball := makePackTarball(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/packs":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[{"slug":"python-web-security","name":"Python Web Security","languages":["python"]}]}`))
		case "/api/packs/python-web-security/manifest.json":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"schema_version":1,"slug":"python-web-security","build_id":"build-1","total_rules":1,"languages":["python"],"rules":[{"slug":"example","yaml_path":"rules/example.yaml"}]}`))
		case "/api/packs/python-web-security/latest.tar.gz":
			w.Header().Set("content-type", "application/gzip")
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	if err := runFetch(t.Context(), []string{"--root", root, "--registry", server.URL, "--target", "src/main.py"}); err != nil {
		t.Fatal(err)
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packs) != 1 || lock.Packs[0].ID != "python-web-security" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
}

func TestRunScanAutoFetchesMissingLockForStandalone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), "[project]\ndependencies = [\"fastapi\"]\n")
	writeFile(t, filepath.Join(root, "src", "main.py"), "from fastapi import FastAPI\napp = FastAPI()\n")
	tarball := makePackTarball(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/packs":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[{"slug":"python-web-security","name":"Python Web Security","languages":["python"]}]}`))
		case "/api/packs/python-web-security/manifest.json":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"schema_version":1,"slug":"python-web-security","build_id":"build-1","total_rules":1,"languages":["python"],"rules":[{"slug":"example","yaml_path":"rules/example.yaml"}]}`))
		case "/api/packs/python-web-security/latest.tar.gz":
			w.Header().Set("content-type", "application/gzip")
			_, _ = w.Write(tarball)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
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
		if err := runScan(t.Context(), []string{"--root", root, "--target", "src/main.py", "--sarif=false", "--explain-selection"}); err != nil {
			t.Fatal(err)
		}
	})
	output := stdout.String()
	for _, want := range []string{"detected languages:", "python-web-security", "fetched python-web-security", "wrote .greprules/out/agent-result.json"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in scan output, got %q", want, output)
		}
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packs) != 1 || lock.Packs[0].ID != "python-web-security" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	assertFileExists(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
}

func TestRunScanNoAutoFetchKeepsMissingLockFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
printf '{"results":[]}\n'
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

	err := runScan(t.Context(), []string{"--root", root, "--full", "--sarif=false", "--no-auto-fetch"})
	if err == nil || !strings.Contains(err.Error(), "lockfile missing") {
		t.Fatalf("expected missing lockfile error, got %v", err)
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
	cfg.OpenGrep.IncludeDefaultRules = true
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

func TestRunScanUsesExplicitTargetsFromFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package main\n")
	writeFile(t, filepath.Join(root, "b.go"), "package main\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "go-security", "rules", "example.yaml"), "rules: []\n")
	targetsFrom := filepath.Join(root, "targets.txt")
	writeFile(t, targetsFrom, "b.go\na.go\nb.go\n\n")
	targetLog := filepath.Join(root, "opengrep-targets.txt")
	configLog := filepath.Join(root, "opengrep-configs.txt")
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
out=""
fmt="json"
targets=""
configs=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    scan) ;;
    --config) shift; configs="${configs}${configs:+ }$1" ;;
    --output) shift; out="$1" ;;
    --json) fmt="json" ;;
    --sarif) fmt="sarif" ;;
    --*) ;;
    *) targets="${targets}${targets:+ }$1" ;;
  esac
  shift
done
printf '%s\n' "$targets" >> "`+targetLog+`"
printf '%s\n' "$configs" >> "`+configLog+`"
if [ "$fmt" = "sarif" ]; then
  printf '{"version":"2.1.0","runs":[]}\n' > "$out"
else
  printf '{"results":[]}\n' > "$out"
fi
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.OpenGrep.Mode = "path"
	cfg.OpenGrep.Managed = false
	cfg.OpenGrep.Path = fakeOpenGrep
	cfg.OpenGrep.IncludeDefaultRules = true
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

	if err := runScan(t.Context(), []string{"--root", root, "--targets-from", targetsFrom}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".greprules", "out", "agent-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Repo struct {
			ChangedMode  bool     `json:"changedMode"`
			ChangedFiles []string `json:"changedFiles"`
		} `json:"repo"`
		Scan struct {
			Targets []string `json:"targets"`
		} `json:"scan"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Repo.ChangedMode || len(result.Repo.ChangedFiles) != 0 {
		t.Fatalf("expected explicit targets not git changed mode, got %s", string(data))
	}
	if strings.Join(result.Scan.Targets, ",") != "a.go,b.go" {
		t.Fatalf("unexpected scan targets: %s", string(data))
	}
	logData, err := os.ReadFile(targetLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "a.go b.go") {
		t.Fatalf("expected explicit targets in opengrep invocation, got %q", string(logData))
	}
	configData, err := os.ReadFile(configLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), " auto") {
		t.Fatalf("expected opengrep default rule config in invocation, got %q", string(configData))
	}
	if !strings.Contains(string(data), "\"configs\"") || !strings.Contains(string(data), "\"auto\"") {
		t.Fatalf("expected agent result to record scan configs, got %s", string(data))
	}
}

func TestRunScanUsesProvidedRootByDefaultInsideParentGitRepo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	parent := t.TempDir()
	if err := exec.Command("git", "-C", parent, "init").Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	root := filepath.Join(parent, "packages", "api")
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

	if err := runScan(t.Context(), []string{"--root", root, "--target", filepath.Join(root, "app.mjs"), "--sarif=false"}); err != nil {
		t.Fatal(err)
	}

	assertFileExists(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
	assertNoFile(t, filepath.Join(parent, ".greprules", "lock.json"))
}

func TestResolveCommandRootDiscoversGitRootOnlyForChangedMode(t *testing.T) {
	parent := t.TempDir()
	if err := exec.Command("git", "-C", parent, "init").Run(); err != nil {
		t.Skipf("git init unavailable: %v", err)
	}
	child := filepath.Join(parent, "packages", "api")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	exact, err := resolveCommandRoot(child, false)
	if err != nil {
		t.Fatal(err)
	}
	if exact != child {
		t.Fatalf("expected provided root by default, got %s", exact)
	}
	changedRoot, err := resolveCommandRoot(child, true)
	if err != nil {
		t.Fatal(err)
	}
	realChangedRoot, err := filepath.EvalSymlinks(changedRoot)
	if err != nil {
		t.Fatal(err)
	}
	realParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	if realChangedRoot != realParent {
		t.Fatalf("expected git root for changed mode, got %s", changedRoot)
	}
}

func TestRunScanPassesEachPackAsSeparateOpenGrepConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "pack-a", "rules", "a.yaml"), "rules: []\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "pack-b", "rules", "b.yaml"), "rules: []\n")
	configLog := filepath.Join(root, "opengrep-configs.txt")
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
out=""
configs=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) shift; configs="${configs}${configs:+|}$1" ;;
    --output) shift; out="$1" ;;
  esac
  shift
done
printf '%s\n' "$configs" > "`+configLog+`"
printf '{"results":[]}\n' > "$out"
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.OpenGrep.Mode = "path"
	cfg.OpenGrep.Managed = false
	cfg.OpenGrep.Path = fakeOpenGrep
	cfg.OpenGrep.IncludeDefaultRules = true
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	lock := config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      "https://example.test",
		Packs: []config.LockedPack{
			{
				ID:         "pack-a",
				Version:    "build-1",
				SHA256:     "abc",
				RulePath:   ".greprules/cache/packs/pack-a/rules",
				TotalRules: 1,
			},
			{
				ID:         "pack-b",
				Version:    "build-1",
				SHA256:     "def",
				RulePath:   ".greprules/cache/packs/pack-b/rules",
				TotalRules: 1,
			},
		},
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}

	if err := runScan(t.Context(), []string{"--root", root, "--full", "--sarif=false"}); err != nil {
		t.Fatal(err)
	}

	configData, err := os.ReadFile(configLog)
	if err != nil {
		t.Fatal(err)
	}
	configs := strings.Split(strings.TrimSpace(string(configData)), "|")
	if len(configs) != 3 {
		t.Fatalf("expected two pack configs plus auto, got %q", string(configData))
	}
	if strings.Contains(configs[0], string(os.PathListSeparator)) || strings.Contains(configs[1], string(os.PathListSeparator)) {
		t.Fatalf("expected separate --config args, got %q", string(configData))
	}
	if !strings.HasSuffix(configs[0], filepath.Join(".greprules", "cache", "packs", "pack-a", "rules")) ||
		!strings.HasSuffix(configs[1], filepath.Join(".greprules", "cache", "packs", "pack-b", "rules")) ||
		configs[2] != "auto" {
		t.Fatalf("unexpected configs: %q", string(configData))
	}
}

func TestRunScanKeepsFindingsWhenOpenGrepReturnsPartialError(t *testing.T) {
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
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output) shift; out="$1" ;;
  esac
  shift
done
printf '{"results":[{"check_id":"greprules.partial","path":"main.go","start":{"line":1,"col":1},"end":{"line":1,"col":2},"extra":{"message":"partial","severity":"WARNING","metadata":{"license":"MIT"}}}],"errors":[{"type":"Rule parse error","level":"error","path":"rules/bad.yaml","message":"bad pattern"}]}\n' > "$out"
exit 2
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

	if err := runScan(t.Context(), []string{"--root", root, "--full", "--sarif=false"}); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".greprules", "out", "agent-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result struct {
		Status   string `json:"status"`
		Findings []struct {
			RuleID string `json:"ruleId"`
		} `json:"findings"`
		Warnings []string `json:"warnings"`
		Errors   []string `json:"errors"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Status != "ok" || len(result.Findings) != 1 || result.Findings[0].RuleID != "greprules.partial" {
		t.Fatalf("expected partial findings to be preserved, got %s", string(data))
	}
	if len(result.Errors) != 0 {
		t.Fatalf("expected partial OpenGrep errors as warnings, got %s", string(data))
	}
	if !containsString(result.Warnings, "OpenGrep JSON run exited with status 2; preserving findings from generated output") ||
		!containsWarningContaining(result.Warnings, "Rule parse error") {
		t.Fatalf("expected exit and diagnostic warnings, got %s", string(data))
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
	if err := runConfigSet([]string{"--root", root, "registry", "http://127.0.0.1:8790", "--global"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigSet([]string{"--root", root, "opengrep.includeDefaultRules", "true", "--global"}); err != nil {
		t.Fatal(err)
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Config.OpenGrep.Mode != "system" ||
		resolution.Config.Registry != "http://127.0.0.1:8790" ||
		!resolution.Config.OpenGrep.IncludeDefaultRules {
		t.Fatalf("unexpected effective config: %#v", resolution.Config)
	}
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatal(err)
	}
}

func TestAgentConfigKeysUnsupported(t *testing.T) {
	root := t.TempDir()
	userConfig := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GREPRULES_USER_CONFIG", userConfig)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")

	if err := runConfigSet([]string{"--root", root, "agent.autoScan", "true", "--global"}); err == nil {
		t.Fatal("expected agent.autoScan to be unsupported")
	}
}

func TestDoctorMissingLockIsNotSetupFailureWhenScanCanFetch(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/packs" {
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"success":true,"packs":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
printf '{"results":[]}\n'
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

	report, err := doctor.Build(t.Context(), root, doctor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "ok" {
		t.Fatalf("expected missing lock to remain ok when registry/runtime are ready, got %q", report.Status)
	}
	if report.Lock.Exists {
		t.Fatal("expected missing lock")
	}
	if !strings.Contains(report.Lock.Message, "not fetched yet") {
		t.Fatalf("expected informational lock message, got %#v", report.Lock)
	}
	if containsString(report.RecommendedCommands, "greprules fetch") {
		t.Fatalf("expected fetch not to be setup recommendation, got %#v", report.RecommendedCommands)
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

func TestEnsureGreprulesGitignoreAddsEntryOnce(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not available")
	}
	root := t.TempDir()
	if err := exec.Command("git", "-C", root, "init").Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	writeFile(t, filepath.Join(root, ".gitignore"), "node_modules/\n")

	if err := ensureGreprulesGitignore(root); err != nil {
		t.Fatal(err)
	}
	if err := ensureGreprulesGitignore(root); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := gitignoreEffectiveLines(data)
	for _, entry := range greprulesGitignoreEntries {
		if !lines[entry] {
			t.Fatalf("expected %s in .gitignore, got %q", entry, string(data))
		}
		if count := strings.Count(string(data), entry); count != 1 {
			t.Fatalf("expected one %s entry, got %d in %q", entry, count, string(data))
		}
	}
	if lines[".greprules/"] || lines[".greprules/config.yaml"] || lines[".greprules/lock.json"] {
		t.Fatalf("should not ignore shared greprules files, got %q", string(data))
	}
}

func TestRunCleanupRemovesSelectedUserPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	userConfig := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GREPRULES_USER_CONFIG", userConfig)
	writeFile(t, userConfig, "{}\n")
	cacheRoot, err := greprulesPluginCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cacheRoot, "greprules", "v0.1.0", "greprules"), "binary")

	if err := runCleanup([]string{"--config", "--plugin-cache", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatalf("dry run should keep config: %v", err)
	}
	if _, err := os.Stat(cacheRoot); err != nil {
		t.Fatalf("dry run should keep cache: %v", err)
	}

	if err := runCleanup([]string{"--config", "--plugin-cache"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userConfig); !os.IsNotExist(err) {
		t.Fatalf("expected config removed, got err=%v", err)
	}
	if _, err := os.Stat(cacheRoot); !os.IsNotExist(err) {
		t.Fatalf("expected plugin cache removed, got err=%v", err)
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsWarningContaining(values []string, target string) bool {
	for _, value := range values {
		if strings.Contains(value, target) {
			return true
		}
	}
	return false
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
