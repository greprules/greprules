package opengrep

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildScanArgsStructured(t *testing.T) {
	args, err := BuildScanArgs(ScanArgsOptions{
		Configs:    []string{"rules-a", "rules-b"},
		Format:     "json",
		OutputPath: ".greprules/out/scan.json",
		Targets:    []string{"src"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"scan",
		"--config", "rules-a",
		"--config", "rules-b",
		"--json",
		"--output", ".greprules/out/scan.json",
		"src",
	}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\nwant %#v\n got %#v", want, args)
	}
}

func TestBuildScanArgsRaw(t *testing.T) {
	args, err := BuildScanArgs(ScanArgsOptions{
		Configs: []string{"rules"},
		Args:    []string{"--sarif", "--output", "scan.sarif", "."},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"scan", "--config", "rules", "--sarif", "--output", "scan.sarif", "."}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("unexpected args:\nwant %#v\n got %#v", want, args)
	}
}

func TestBuildScanArgsRejectsInvalidConfigAndFormat(t *testing.T) {
	if _, err := BuildScanArgs(ScanArgsOptions{Configs: []string{""}}); err == nil || !strings.Contains(err.Error(), "config is empty") {
		t.Fatalf("expected empty config error, got %v", err)
	}
	if _, err := BuildScanArgs(ScanArgsOptions{Format: "xml"}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported format error, got %v", err)
	}
}
