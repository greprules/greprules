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

	"github.com/greprules/greprules/internal/agent"
	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/doctor"
	"github.com/greprules/greprules/internal/standalone"
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
	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	if err := standalone.RunFetch(t.Context(), []string{"--root", root, "go-security"}); err != nil {
		t.Fatal(err)
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packs) != 1 || lock.Packs[0].ID != "go-security" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	rulePath := lock.Packs[0].RulePath
	if !filepath.IsAbs(rulePath) {
		rulePath = filepath.Join(root, rulePath)
	}
	if _, err := os.Stat(filepath.Join(rulePath, "example.yaml")); err != nil {
		t.Fatal(err)
	}
	assertNoFile(t, filepath.Join(root, ".greprules", "lock.json"))
	assertNoFile(t, filepath.Join(root, ".greprules", "cache"))
}

func TestRunFetchRequiresExplicitPack(t *testing.T) {
	root := t.TempDir()
	err := standalone.RunFetch(t.Context(), []string{"--root", root})
	if err == nil || !strings.Contains(err.Error(), "usage: greprules fetch <PACK>") {
		t.Fatalf("expected explicit pack usage error, got %v", err)
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
			_, _ = w.Write([]byte(`{"success":true,"packs":[{"slug":"python-web-security","name":"Python Web Security","languages":["python"],"selection":{"kind":"framework","languages":["python"],"frameworks":["fastapi"],"source_types":[],"tags":[]}}]}`))
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
	argsLog := filepath.Join(root, "opengrep-args.txt")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
printf '%s\n' "$@" > "`+argsLog+`"
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := standalone.RunScanWithOptions(t.Context(), []string{"--root", root, "--verbose", "src/main.py", "--json", "--severity", "ERROR"}, standalone.ScanOptions{
		Stdout:       &stdout,
		Stderr:       &stderr,
		AutoPrepare:  true,
		OpenGrepPath: fakeOpenGrep,
	}); err != nil {
		t.Fatal(err)
	}
	output := stderr.String()
	for _, want := range []string{"detected languages:", "python-web-security", "fetched python-web-security"} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in scan output, got %q", want, output)
		}
	}
	if strings.Contains(stdout.String(), "fetched") || strings.Contains(stdout.String(), "detected languages") {
		t.Fatalf("expected greprules preparation logs on stderr, got stdout %q", stdout.String())
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packs) != 1 || lock.Packs[0].ID != "python-web-security" {
		t.Fatalf("unexpected lock: %#v", lock)
	}
	assertNoFile(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
	argsData, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scan\n", "--config\n", "src/main.py\n", "--json\n", "--severity\n", "ERROR\n"} {
		if !strings.Contains(string(argsData), want) {
			t.Fatalf("expected OpenGrep arg %q in %q", want, string(argsData))
		}
	}
}

func TestRunScanRefetchesMissingLockedArtifacts(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"success":true,"packs":[{"slug":"python-web-security","name":"Python Web Security","languages":["python"],"selection":{"kind":"framework","languages":["python"],"frameworks":["fastapi"],"source_types":[],"tags":[]}}]}`))
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
exit 0
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Registry = server.URL
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	staleRulePath := filepath.Join(root, "missing-cache", "rules")
	if err := config.SaveLock(root, config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      server.URL,
		Packs: []config.LockedPack{{
			ID:         "python-web-security",
			Version:    "old",
			SHA256:     "stale",
			RulePath:   staleRulePath,
			TotalRules: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	var stderr bytes.Buffer
	if err := standalone.RunScanWithOptions(t.Context(), []string{"--root", root, "src/main.py"}, standalone.ScanOptions{
		Stderr:       &stderr,
		AutoPrepare:  true,
		OpenGrepPath: fakeOpenGrep,
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr.String(), "fetched python-web-security") {
		t.Fatalf("expected stale lock to be refreshed, got stderr %q", stderr.String())
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Packs) != 1 || lock.Packs[0].RulePath == staleRulePath {
		t.Fatalf("expected refreshed lock, got %#v", lock)
	}
	if _, err := os.Stat(filepath.Join(lock.Packs[0].RulePath, "example.yaml")); err != nil {
		t.Fatal(err)
	}
}

func TestRunScanNoPrepareRejectsMissingLockedArtifacts(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main.go"), "package main\n")
	if err := config.SaveLock(root, config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      "https://example.test",
		Packs: []config.LockedPack{{
			ID:         "go-security",
			Version:    "old",
			SHA256:     "stale",
			RulePath:   filepath.Join(root, "missing-cache", "rules"),
			TotalRules: 1,
		}},
	}); err != nil {
		t.Fatal(err)
	}

	err := standalone.RunScan(t.Context(), []string{"--root", root, "--no-prepare"})
	if err == nil || !strings.Contains(err.Error(), "locked rule pack artifacts are missing") {
		t.Fatalf("expected missing artifact error, got %v", err)
	}
}

func TestRunScanNoPrepareKeepsMissingLockFailure(t *testing.T) {
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
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}

	err := standalone.RunScan(t.Context(), []string{"--root", root, "--no-prepare"})
	if err == nil || !strings.Contains(err.Error(), "lockfile missing") {
		t.Fatalf("expected missing lockfile error, got %v", err)
	}
}

func TestStandaloneScanParsesKnownOpenGrepValueFlagsForTargets(t *testing.T) {
	request, _, err := standalone.ParseScanRequest([]string{"--json-output", "result.json", "--severity", "ERROR", "src"}, standalone.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(request.OpenGrepArgs, " ") != "--json-output result.json --severity ERROR src" {
		t.Fatalf("unexpected OpenGrep args: %#v", request.OpenGrepArgs)
	}
	if strings.Join(request.RulePacks.Targets, " ") != "src" {
		t.Fatalf("expected only src as selection target, got %#v", request.RulePacks.Targets)
	}
}

func TestStandaloneScanRejectsUnknownOpenGrepFlagsBeforeSeparator(t *testing.T) {
	_, _, err := standalone.ParseScanRequest([]string{"--future-flag", "src"}, standalone.ScanOptions{})
	if err == nil {
		t.Fatal("expected unsupported OpenGrep flag error")
	}
	if !strings.Contains(err.Error(), "unsupported OpenGrep flag before --: --future-flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStandaloneScanSupportsRawOpenGrepPassthroughAfterSeparator(t *testing.T) {
	request, policy, err := standalone.ParseScanRequest([]string{"--verbose", "src", "--", "--future-flag", "value"}, standalone.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !policy.Verbose {
		t.Fatal("expected greprules verbose policy")
	}
	if strings.Join(request.RulePacks.Targets, " ") != "src" {
		t.Fatalf("unexpected targets: %#v", request.RulePacks.Targets)
	}
	if strings.Join(request.OpenGrepArgs, " ") != "src --future-flag value" {
		t.Fatalf("unexpected OpenGrep args: %#v", request.OpenGrepArgs)
	}
}

func TestStandaloneInitCommandRemoved(t *testing.T) {
	if code := Execute([]string{"init"}, "test"); code == 0 {
		t.Fatal("expected init to be removed from standalone CLI")
	}
}

func TestStandaloneDoctorCommandRemoved(t *testing.T) {
	if code := Execute([]string{"doctor"}, "test"); code == 0 {
		t.Fatal("expected doctor to be removed from standalone CLI")
	}
}

func TestRunScanPassthroughUsesLockedPackConfigsAndUserArgs(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "go-security", "rules", "example.yaml"), "rules: []\n")
	argsLog := filepath.Join(root, "opengrep-args.txt")
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
printf '%s\n' "$@" > "`+argsLog+`"
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
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
		Engine: testLockedEngine(fakeOpenGrep),
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}
	if err := standalone.RunScan(t.Context(), []string{"--root", root, ".", "--sarif", "--output", "result.sarif"}); err != nil {
		t.Fatal(err)
	}
	argsData, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"scan\n", "--config\n", "auto\n", ".\n", "--sarif\n", "--output\n", "result.sarif\n"} {
		if !strings.Contains(string(argsData), want) {
			t.Fatalf("expected OpenGrep arg %q in %q", want, string(argsData))
		}
	}
	assertNoFile(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
	updatedLock, err := config.LoadLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if updatedLock.Engine == nil || updatedLock.Engine.Mode != "managed" || updatedLock.Engine.Version != "9.8.7" {
		t.Fatalf("expected managed engine in lockfile, got %#v", updatedLock.Engine)
	}
}

func TestRunScanUsesExplicitPositionalTargets(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.go"), "package main\n")
	writeFile(t, filepath.Join(root, "b.go"), "package main\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "go-security", "rules", "example.yaml"), "rules: []\n")
	targetLog := filepath.Join(root, "opengrep-targets.txt")
	configLog := filepath.Join(root, "opengrep-configs.txt")
	fakeOpenGrep := filepath.Join(root, "fake-opengrep")
	writeFile(t, fakeOpenGrep, `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf 'opengrep 9.8.7\n'
  exit 0
fi
targets=""
configs=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    scan) ;;
    --config) shift; configs="${configs}${configs:+ }$1" ;;
    --*) ;;
    *) targets="${targets}${targets:+ }$1" ;;
  esac
  shift
done
printf '%s\n' "$targets" >> "`+targetLog+`"
printf '%s\n' "$configs" >> "`+configLog+`"
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
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
		Engine: testLockedEngine(fakeOpenGrep),
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}

	if err := standalone.RunScan(t.Context(), []string{"--root", root, "b.go", "a.go", "b.go"}); err != nil {
		t.Fatal(err)
	}

	logData, err := os.ReadFile(targetLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logData), "b.go a.go b.go") {
		t.Fatalf("expected explicit targets in opengrep invocation, got %q", string(logData))
	}
	configData, err := os.ReadFile(configLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configData), " auto") {
		t.Fatalf("expected opengrep default rule config in invocation, got %q", string(configData))
	}
	assertNoFile(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
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
exit 0
`)

	if err := standalone.RunScan(t.Context(), []string{"--root", root, filepath.Join(root, "app.mjs")}); err != nil {
		t.Fatal(err)
	}

	assertNoFile(t, filepath.Join(root, ".greprules", "out", "agent-result.json"))
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
	exact, err := cmdutil.ResolveCommandRoot(child, false)
	if err != nil {
		t.Fatal(err)
	}
	if exact != child {
		t.Fatalf("expected provided root by default, got %s", exact)
	}
	changedRoot, err := cmdutil.ResolveCommandRoot(child, true)
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
configs=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --config) shift; configs="${configs}${configs:+|}$1" ;;
  esac
  shift
done
printf '%s\n' "$configs" > "`+configLog+`"
`)
	if err := os.Chmod(fakeOpenGrep, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
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
		Engine: testLockedEngine(fakeOpenGrep),
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}

	if err := standalone.RunScan(t.Context(), []string{"--root", root}); err != nil {
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

func TestAgentScanKeepsFindingsWhenOpenGrepReturnsPartialError(t *testing.T) {
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
		Engine: testLockedEngine(fakeOpenGrep),
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}
	configureTestManagedOpenGrep(t, fakeOpenGrep)

	var stdout bytes.Buffer
	withStdout(t, &stdout, func() {
		if err := agent.RunScanCommand(t.Context(), []string{"scan", "--root", root, "--format", "json", "--no-sarif"}); err != nil {
			t.Fatal(err)
		}
	})

	var outcome struct {
		ResultPath string `json:"resultPath"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &outcome); err != nil {
		t.Fatalf("expected JSON outcome, got %q: %v", stdout.String(), err)
	}
	if outcome.ResultPath == "" {
		t.Fatalf("expected JSON outcome to report resultPath, got %q", stdout.String())
	}
	data, err := os.ReadFile(outcome.ResultPath)
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

func TestAgentConfigSetGlobalAndInspect(t *testing.T) {
	root := t.TempDir()
	userConfig := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("GREPRULES_USER_CONFIG", userConfig)
	writeFile(t, filepath.Join(root, "go.mod"), "module example.test\n")

	if err := agent.RunConfigSet([]string{"--root", root, "--global", "opengrep.mode", "system"}); err == nil {
		t.Fatal("expected opengrep.mode to be unsupported")
	}
	if err := agent.RunConfigSet([]string{"--root", root, "registry", "http://127.0.0.1:8790", "--global"}); err != nil {
		t.Fatal(err)
	}
	if err := agent.RunConfigSet([]string{"--root", root, "opengrep.includeDefaultRules", "true", "--global"}); err != nil {
		t.Fatal(err)
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolution.Config.OpenGrep.Mode != "managed" ||
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

	if err := agent.RunConfigSet([]string{"--root", root, "agent.autoScan", "true", "--global"}); err == nil {
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
	if err := config.SaveLocalConfig(root, cfg); err != nil {
		t.Fatal(err)
	}
	configureTestManagedOpenGrep(t, fakeOpenGrep)

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
	if err := agent.RunConfigSet([]string{"--root", root, "--repo", "opengrep.path", "/tmp/opengrep"}); err == nil {
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
	if resolution.Config.OpenGrep.Mode != "managed" {
		t.Fatalf("expected managed OpenGrep mode, got %#v", resolution.Config.OpenGrep)
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

	if err := cmdutil.EnsureGreprulesGitignore(root); err != nil {
		t.Fatal(err)
	}
	if err := cmdutil.EnsureGreprulesGitignore(root); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	lines := cmdutil.GitignoreEffectiveLines(data)
	for _, entry := range cmdutil.GreprulesGitignoreEntries {
		if !lines[entry] {
			t.Fatalf("expected %s in .gitignore, got %q", entry, string(data))
		}
		if count := strings.Count(string(data), entry); count != 1 {
			t.Fatalf("expected one %s entry, got %d in %q", entry, count, string(data))
		}
	}
	if lines[".greprules/"] || lines[".greprules/config.yaml"] {
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
	cacheRoot, err := standalone.PluginCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(cacheRoot, "greprules", "v0.1.0", "greprules"), "binary")

	if err := standalone.RunCleanup([]string{"--config", "--plugin-cache", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(userConfig); err != nil {
		t.Fatalf("dry run should keep config: %v", err)
	}
	if _, err := os.Stat(cacheRoot); err != nil {
		t.Fatalf("dry run should keep cache: %v", err)
	}

	if err := standalone.RunCleanup([]string{"--config", "--plugin-cache"}); err != nil {
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
