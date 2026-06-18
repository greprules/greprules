package agent

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestResolveScanTargetsFromExplicitInputs(t *testing.T) {
	root := t.TempDir()
	writeScanServiceTestFile(t, filepath.Join(root, "a.go"), "package main\n")
	writeScanServiceTestFile(t, filepath.Join(root, "b.go"), "package main\n")
	targetsPath := filepath.Join(root, "targets.txt")
	writeScanServiceTestFile(t, targetsPath, "\n# comment\nb.go\na.go\nb.go\n")

	selection, err := resolveScanTargets(targetOptions{Root: root, TargetsFrom: targetsPath})
	if err != nil {
		t.Fatal(err)
	}
	if !selection.ExplicitMode || selection.ChangedMode || selection.EmptyWarning != "" {
		t.Fatalf("unexpected target mode: %#v", selection)
	}
	want := []string{"a.go", "b.go"}
	if !reflect.DeepEqual(selection.Targets, want) {
		t.Fatalf("unexpected targets:\nwant %#v\n got %#v", want, selection.Targets)
	}
}

func TestResolveScanTargetsRejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.go")
	writeScanServiceTestFile(t, outside, "package main\n")

	_, err := resolveScanTargets(targetOptions{Root: root, Targets: []string{outside}})
	if err == nil {
		t.Fatal("expected outside root error")
	}
}

func writeScanServiceTestFile(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
