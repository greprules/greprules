package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpenGrepConfigPathsIncludesLockedPacksAndDefaultRules(t *testing.T) {
	root := t.TempDir()
	lock := Lock{
		Packs: []LockedPack{
			{RulePath: ".greprules/cache/packs/a/rules"},
			{RulePath: ".greprules/cache/packs/b/rules"},
		},
	}
	configs := OpenGrepConfigPaths(root, lock, true)
	want := []string{
		filepath.Join(root, ".greprules", "cache", "packs", "a", "rules"),
		filepath.Join(root, ".greprules", "cache", "packs", "b", "rules"),
		"auto",
	}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("unexpected configs:\nwant %#v\n got %#v", want, configs)
	}
}

func TestOpenGrepConfigPathsKeepsAbsoluteRulePath(t *testing.T) {
	root := t.TempDir()
	absolute := filepath.Join(t.TempDir(), "rules")
	lock := Lock{Packs: []LockedPack{{RulePath: absolute}}}

	configs := OpenGrepConfigPaths(root, lock, false)
	want := []string{absolute}
	if !reflect.DeepEqual(configs, want) {
		t.Fatalf("unexpected configs:\nwant %#v\n got %#v", want, configs)
	}
}

func TestLockPathUsesUserProjectState(t *testing.T) {
	root := t.TempDir()
	stateHome := t.TempDir()
	t.Setenv("GREPRULES_STATE_HOME", stateHome)

	lockPath, err := LockPath(root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(lockPath, filepath.Join(stateHome, "greprules", "projects")+string(os.PathSeparator)) {
		t.Fatalf("expected lock path under user project state, got %s", lockPath)
	}
	if filepath.Dir(lockPath) == filepath.Join(root, ".greprules") {
		t.Fatalf("expected lock path outside repo .greprules, got %s", lockPath)
	}
}

func TestLoadLockIgnoresLegacyRepoLock(t *testing.T) {
	root := t.TempDir()
	t.Setenv("GREPRULES_STATE_HOME", t.TempDir())
	legacyPath := filepath.Join(root, ".greprules", "lock.json")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"schemaVersion":"greprules.lock.v1","registry":"https://example.test","packs":[{"id":"legacy","version":"1","sha256":"abc","rulePath":"rules","downloadedAt":""}],"generatedAt":"now"}`)
	if err := os.WriteFile(legacyPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadLock(root); !os.IsNotExist(err) {
		t.Fatalf("expected missing user-state lock despite legacy repo lock, got %v", err)
	}
}

func TestRulePackCacheRootUsesUserCacheByDefault(t *testing.T) {
	root := t.TempDir()
	cacheHome := t.TempDir()
	t.Setenv("GREPRULES_CACHE_HOME", cacheHome)

	cacheRoot, err := RulePackCacheRoot(root, "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cacheHome, "greprules", "packs")
	if cacheRoot != want {
		t.Fatalf("unexpected cache root: want %s got %s", want, cacheRoot)
	}
}
