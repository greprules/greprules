package opengrep

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	latestReleaseURL = "https://api.github.com/repos/opengrep/opengrep/releases/latest"
	releaseByTagURL  = "https://api.github.com/repos/opengrep/opengrep/releases/tags/"
)

type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func FetchRelease(ctx context.Context, version string, client *http.Client) (Release, error) {
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	endpoint := latestReleaseURL
	if version != "" && version != "latest" {
		endpoint = releaseByTagURL + strings.TrimPrefix(version, "v")
		if !strings.HasSuffix(endpoint, strings.TrimPrefix(version, "v")) {
			endpoint = releaseByTagURL + version
		}
		endpoint = releaseByTagURL + ensureV(version)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Release{}, fmt.Errorf("fetch OpenGrep release %s failed: %s", version, resp.Status)
	}
	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return Release{}, err
	}
	return release, nil
}

func SelectBinaryAsset(release Release, goos, goarch string) (Asset, Asset, Asset, error) {
	binaryName, err := assetName(goos, goarch)
	if err != nil {
		return Asset{}, Asset{}, Asset{}, err
	}
	var binary, cert, sig Asset
	for _, asset := range release.Assets {
		switch asset.Name {
		case binaryName:
			binary = asset
		case binaryName + ".cert":
			cert = asset
		case binaryName + ".sig":
			sig = asset
		}
	}
	if binary.Name == "" {
		return Asset{}, Asset{}, Asset{}, fmt.Errorf("OpenGrep release %s has no asset for %s/%s (%s)", release.TagName, goos, goarch, binaryName)
	}
	return binary, cert, sig, nil
}

func assetName(goos, goarch string) (string, error) {
	switch goos {
	case "darwin":
		if goarch == "arm64" {
			return "opengrep_osx_arm64", nil
		}
		if goarch == "amd64" {
			return "opengrep_osx_x86", nil
		}
	case "linux":
		if goarch == "arm64" {
			return "opengrep_manylinux_aarch64", nil
		}
		if goarch == "amd64" {
			return "opengrep_manylinux_x86", nil
		}
	case "windows":
		if goarch == "amd64" || goarch == "386" {
			return "opengrep_windows_x86.exe", nil
		}
	}
	return "", fmt.Errorf("unsupported platform %s/%s", goos, goarch)
}

func CurrentAssetName() (string, error) {
	return assetName(runtime.GOOS, runtime.GOARCH)
}

func ensureV(version string) string {
	if strings.HasPrefix(version, "v") {
		return version
	}
	return "v" + version
}
