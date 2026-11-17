package opengrep

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestSelectBinaryAssetLinuxReleases(t *testing.T) {
	platforms := []struct {
		arch   string
		modern string
		legacy string
	}{
		{"amd64", "opengrep_linux_x86", "opengrep_manylinux_x86"},
		{"arm64", "opengrep_linux_aarch64", "opengrep_manylinux_aarch64"},
	}
	for _, platform := range platforms {
		tests := []struct {
			name     string
			tag      string
			binaries []string
			want     string
		}{
			{"v2", "v2.0.0", []string{"opengrep_linux_x86", "opengrep_linux_aarch64"}, platform.modern},
			{"v1", "v1.30.0", []string{"opengrep_manylinux_x86", "opengrep_manylinux_aarch64"}, platform.legacy},
			{"both_legacy_first", "v2.0.0", []string{platform.legacy, platform.modern}, platform.modern},
			{"both_modern_first", "v2.0.0", []string{platform.modern, platform.legacy}, platform.modern},
		}
		for _, tt := range tests {
			t.Run(platform.arch+"/"+tt.name, func(t *testing.T) {
				release := Release{TagName: tt.tag}
				for _, name := range tt.binaries {
					for _, suffix := range []string{".sig", ".cert", ""} {
						assetName := name + suffix
						release.Assets = append(release.Assets, Asset{
							Name:               assetName,
							BrowserDownloadURL: "https://example.com/" + assetName,
							Size:               123,
						})
					}
				}
				bin, cert, sig, err := SelectBinaryAsset(release, "linux", platform.arch)
				if err != nil {
					t.Fatal(err)
				}
				for suffix, got := range map[string]Asset{"": bin, ".cert": cert, ".sig": sig} {
					want := Asset{
						Name:               tt.want + suffix,
						BrowserDownloadURL: "https://example.com/" + tt.want + suffix,
						Size:               123,
					}
					if got != want {
						t.Errorf("asset %q: want %#v, got %#v", suffix, want, got)
					}
				}
			})
		}
	}
}

func TestSelectBinaryAssetDoesNotMixLinuxSidecars(t *testing.T) {
	tests := []struct {
		name   string
		assets []string
		want   string
	}{
		{
			name: "modern_binary_without_sidecars",
			assets: []string{
				"opengrep_manylinux_x86", "opengrep_manylinux_x86.cert", "opengrep_manylinux_x86.sig",
				"opengrep_linux_x86",
			},
			want: "opengrep_linux_x86",
		},
		{
			name: "legacy_binary_with_orphaned_modern_sidecars",
			assets: []string{
				"opengrep_linux_x86.cert", "opengrep_linux_x86.sig",
				"opengrep_manylinux_x86",
			},
			want: "opengrep_manylinux_x86",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := Release{TagName: "v2.0.0"}
			for _, name := range tt.assets {
				release.Assets = append(release.Assets, Asset{Name: name})
			}
			bin, cert, sig, err := SelectBinaryAsset(release, "linux", "amd64")
			if err != nil {
				t.Fatal(err)
			}
			if bin.Name != tt.want || cert != (Asset{}) || sig != (Asset{}) {
				t.Fatalf("unexpected assets: %#v %#v %#v", bin, cert, sig)
			}
		})
	}
}

func TestSelectBinaryAssetMissingLinuxBinary(t *testing.T) {
	release := Release{
		TagName: "v2.0.0",
		Assets: []Asset{
			{Name: "opengrep_linux_x86.cert"},
			{Name: "opengrep_linux_x86.sig"},
			{Name: "opengrep_linux_aarch64"},
		},
	}
	bin, cert, sig, err := SelectBinaryAsset(release, "linux", "amd64")
	if err == nil {
		t.Fatal("expected an error when the platform binary is missing")
	}
	for _, value := range []string{"v2.0.0", "linux/amd64", "opengrep_linux_x86", "opengrep_manylinux_x86"} {
		if !strings.Contains(err.Error(), value) {
			t.Errorf("error %q does not contain %q", err, value)
		}
	}
	if bin != (Asset{}) || cert != (Asset{}) || sig != (Asset{}) {
		t.Fatalf("expected empty assets on error: %#v %#v %#v", bin, cert, sig)
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
