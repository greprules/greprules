package rules

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/greprules/greprules/internal/config"
)

func TestEnsurePrintsCachedPackSummaryByDefault(t *testing.T) {
	root := setupCachedPackProject(t)
	var out bytes.Buffer

	result, err := Ensure(t.Context(), Request{Root: root}, EnsurePolicy{AutoFetch: true}, EnsureIO{Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !result.LockReady {
		t.Fatal("expected cached lock to be ready")
	}
	output := out.String()
	if !strings.Contains(output, "using cached rule packs: pack-a, pack-b") {
		t.Fatalf("expected cached pack summary, got %q", output)
	}
	for _, unexpected := range []string{"rulePath=", "manifest=", "sha256="} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("expected default output to omit %s details, got %q", unexpected, output)
		}
	}
}

func TestEnsurePrintsCachedPackDetailsWhenVerbose(t *testing.T) {
	root := setupCachedPackProject(t)
	var out bytes.Buffer

	result, err := Ensure(t.Context(), Request{Root: root}, EnsurePolicy{AutoFetch: true, Verbose: true}, EnsureIO{Stdout: &out})
	if err != nil {
		t.Fatal(err)
	}
	if !result.LockReady {
		t.Fatal("expected cached lock to be ready")
	}
	output := out.String()
	for _, want := range []string{
		"using cached rule packs:",
		"pack-a version=build-a rules=2 rulePath=.greprules/cache/packs/pack-a/rules manifest=.greprules/cache/packs/pack-a/manifest.json sha256=sha-a",
		"pack-b version=build-b rules=3 rulePath=.greprules/cache/packs/pack-b/rules manifest=.greprules/cache/packs/pack-b/manifest.json sha256=sha-b",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("expected %q in verbose output, got %q", want, output)
		}
	}
}

func setupCachedPackProject(t *testing.T) string {
	t.Helper()
	t.Setenv("GREPRULES_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	t.Setenv("GREPRULES_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("GREPRULES_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))

	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "pack-a", "rules", "a.yaml"), "rules: []\n")
	writeFile(t, filepath.Join(root, ".greprules", "cache", "packs", "pack-b", "rules", "b.yaml"), "rules: []\n")
	lock := config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      "https://example.test",
		Packs: []config.LockedPack{
			{
				ID:           "pack-a",
				Version:      "build-a",
				SHA256:       "sha-a",
				ManifestPath: ".greprules/cache/packs/pack-a/manifest.json",
				RulePath:     ".greprules/cache/packs/pack-a/rules",
				TotalRules:   2,
			},
			{
				ID:           "pack-b",
				Version:      "build-b",
				SHA256:       "sha-b",
				ManifestPath: ".greprules/cache/packs/pack-b/manifest.json",
				RulePath:     ".greprules/cache/packs/pack-b/rules",
				TotalRules:   3,
			},
		},
	}
	if err := config.SaveLock(root, lock); err != nil {
		t.Fatal(err)
	}
	return root
}
