package opengrep

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSelectBinaryAssetDarwinArm64(t *testing.T) {
	release := Release{
		TagName: "v1.22.0",
		Assets: []Asset{
			{Name: "opengrep_osx_arm64", BrowserDownloadURL: "https://example.com/bin"},
			{Name: "opengrep_osx_arm64.cert", BrowserDownloadURL: "https://example.com/cert"},
			{Name: "opengrep_osx_arm64.sig", BrowserDownloadURL: "https://example.com/sig"},
		},
	}
	bin, cert, sig, err := SelectBinaryAsset(release, "darwin", "arm64")
	if err != nil {
		t.Fatal(err)
	}
	if bin.Name != "opengrep_osx_arm64" || cert.Name == "" || sig.Name == "" {
		t.Fatalf("unexpected assets: %#v %#v %#v", bin, cert, sig)
	}
}

func TestSelectBinaryAssetLinuxAMD64(t *testing.T) {
	release := Release{
		TagName: "v1.22.0",
		Assets:  []Asset{{Name: "opengrep_manylinux_x86", BrowserDownloadURL: "https://example.com/bin"}},
	}
	bin, _, _, err := SelectBinaryAsset(release, "linux", "amd64")
	if err != nil {
		t.Fatal(err)
	}
	if bin.Name != "opengrep_manylinux_x86" {
		t.Fatalf("unexpected asset: %#v", bin)
	}
}

func TestRuntimeFromPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell script fake runtime is unix-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "opengrep")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf 'opengrep 1.2.3\\n'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtimeInfo, err := RuntimeFromPath(path, "path")
	if err != nil {
		t.Fatal(err)
	}
	if runtimeInfo.Mode != "path" || runtimeInfo.Version != "1.2.3" || runtimeInfo.Managed {
		t.Fatalf("unexpected runtime: %#v", runtimeInfo)
	}
}
