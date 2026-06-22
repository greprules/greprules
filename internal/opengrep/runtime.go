package opengrep

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/utils"
)

type Runtime struct {
	Name            string `json:"name"`
	Mode            string `json:"mode"`
	Version         string `json:"version"`
	Path            string `json:"path"`
	Source          string `json:"source"`
	SHA256          string `json:"sha256"`
	Managed         bool   `json:"managed"`
	SignaturePath   string `json:"signaturePath,omitempty"`
	CertificatePath string `json:"certificatePath,omitempty"`
	DownloadedAt    string `json:"downloadedAt,omitempty"`
}

type SetupOptions struct {
	Version   string
	Force     bool
	CacheRoot string
	Client    *http.Client
}

type ResolveOptions struct {
	Mode      string
	Path      string
	Version   string
	CacheRoot string
}

type ConfigOverrides struct {
	Mode    string
	Path    string
	Version string
}

func DefaultCacheRoot() (string, error) {
	root, err := config.UserCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "opengrep"), nil
}

func Setup(ctx context.Context, options SetupOptions) (Runtime, error) {
	version := options.Version
	if version == "" {
		version = "latest"
	}
	cacheRoot := options.CacheRoot
	if cacheRoot == "" {
		var err error
		cacheRoot, err = DefaultCacheRoot()
		if err != nil {
			return Runtime{}, err
		}
	}
	release, err := FetchRelease(ctx, version, options.Client)
	if err != nil {
		return Runtime{}, err
	}
	binaryAsset, certAsset, sigAsset, err := SelectBinaryAsset(release, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Runtime{}, err
	}
	versionDir := filepath.Join(cacheRoot, strings.TrimPrefix(release.TagName, "v"))
	binaryPath := filepath.Join(versionDir, binaryFileName())
	metadataPath := filepath.Join(versionDir, "runtime.json")
	if !options.Force {
		if existing, err := LoadRuntime(metadataPath); err == nil {
			if _, statErr := os.Stat(existing.Path); statErr == nil {
				return existing, nil
			}
		}
	}
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		return Runtime{}, err
	}
	if err := downloadFile(ctx, options.Client, binaryAsset.BrowserDownloadURL, binaryPath, 0o755); err != nil {
		return Runtime{}, err
	}
	sum, err := utils.SHA256File(binaryPath)
	if err != nil {
		return Runtime{}, err
	}
	runtimeInfo := Runtime{
		Name:         "opengrep",
		Mode:         "managed",
		Version:      strings.TrimPrefix(release.TagName, "v"),
		Path:         binaryPath,
		Source:       "github-release",
		SHA256:       sum,
		Managed:      true,
		DownloadedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if certAsset.BrowserDownloadURL != "" {
		certPath := binaryPath + ".cert"
		if err := downloadFile(ctx, options.Client, certAsset.BrowserDownloadURL, certPath, 0o644); err == nil {
			runtimeInfo.CertificatePath = certPath
		}
	}
	if sigAsset.BrowserDownloadURL != "" {
		sigPath := binaryPath + ".sig"
		if err := downloadFile(ctx, options.Client, sigAsset.BrowserDownloadURL, sigPath, 0o644); err == nil {
			runtimeInfo.SignaturePath = sigPath
		}
	}
	if err := SaveRuntime(metadataPath, runtimeInfo); err != nil {
		return Runtime{}, err
	}
	return runtimeInfo, nil
}

func Resolve(options ResolveOptions) (Runtime, error) {
	mode := options.Mode
	if mode == "" {
		mode = "managed"
	}
	switch mode {
	case "managed":
		return Installed(options.Version, options.CacheRoot)
	case "system":
		path, err := exec.LookPath("opengrep")
		if err != nil {
			return Runtime{}, fmt.Errorf("system opengrep not found in PATH: %w", err)
		}
		return RuntimeFromPath(path, "system")
	case "path":
		if options.Path == "" {
			return Runtime{}, errors.New("opengrep.path is required when mode is path")
		}
		return RuntimeFromPath(options.Path, "path")
	default:
		return Runtime{}, fmt.Errorf("unsupported opengrep mode: %s", mode)
	}
}

func ResolveFromConfig(lock config.Lock, cfg config.Config, overrides ConfigOverrides) (Runtime, error) {
	mode := "managed"
	path := ""
	version := cfg.OpenGrep.Version
	if overrides.Mode != "" {
		mode = overrides.Mode
	}
	if overrides.Path != "" {
		path = overrides.Path
		if overrides.Mode == "" {
			mode = "path"
		}
	}
	if overrides.Version != "" {
		version = overrides.Version
	}
	if mode == "" {
		mode = "managed"
	}
	if mode == "managed" && lock.Engine != nil && lock.Engine.Path != "" && (lock.Engine.Managed || lock.Engine.Mode == "managed") {
		if _, err := os.Stat(lock.Engine.Path); err == nil {
			return Runtime{
				Name:            lock.Engine.Name,
				Mode:            firstNonEmpty(lock.Engine.Mode, "managed"),
				Version:         lock.Engine.Version,
				Path:            lock.Engine.Path,
				Source:          lock.Engine.Source,
				SHA256:          lock.Engine.SHA256,
				Managed:         lock.Engine.Managed,
				SignaturePath:   lock.Engine.SignaturePath,
				CertificatePath: lock.Engine.CertificatePath,
				DownloadedAt:    lock.Engine.DownloadedAt,
			}, nil
		}
	}
	return Resolve(ResolveOptions{
		Mode:    mode,
		Path:    path,
		Version: version,
	})
}

func LockedEngineFromRuntime(runtimeInfo Runtime) *config.LockedEngine {
	return &config.LockedEngine{
		Name:            runtimeInfo.Name,
		Mode:            runtimeInfo.Mode,
		Version:         runtimeInfo.Version,
		Path:            runtimeInfo.Path,
		Source:          runtimeInfo.Source,
		SHA256:          runtimeInfo.SHA256,
		Managed:         runtimeInfo.Managed,
		SignaturePath:   runtimeInfo.SignaturePath,
		CertificatePath: runtimeInfo.CertificatePath,
		DownloadedAt:    runtimeInfo.DownloadedAt,
	}
}

func RuntimeFromPath(path string, mode string) (Runtime, error) {
	resolved, err := filepath.Abs(path)
	if err != nil {
		return Runtime{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return Runtime{}, err
	}
	if info.IsDir() {
		return Runtime{}, fmt.Errorf("opengrep path is a directory: %s", resolved)
	}
	if info.Mode()&0o111 == 0 {
		return Runtime{}, fmt.Errorf("opengrep path is not executable: %s", resolved)
	}
	version, err := commandVersion(resolved)
	if err != nil {
		return Runtime{}, err
	}
	sum, err := utils.SHA256File(resolved)
	if err != nil {
		return Runtime{}, err
	}
	source := "configured-path"
	if mode == "system" {
		source = "system-path"
	}
	return Runtime{
		Name:    "opengrep",
		Mode:    mode,
		Version: version,
		Path:    resolved,
		Source:  source,
		SHA256:  sum,
		Managed: false,
	}, nil
}

func Installed(version string, cacheRoot string) (Runtime, error) {
	if cacheRoot == "" {
		var err error
		cacheRoot, err = DefaultCacheRoot()
		if err != nil {
			return Runtime{}, err
		}
	}
	if version != "" && version != "latest" {
		return LoadRuntime(filepath.Join(cacheRoot, strings.TrimPrefix(version, "v"), "runtime.json"))
	}
	entries, err := os.ReadDir(cacheRoot)
	if err != nil {
		return Runtime{}, err
	}
	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			if _, err := os.Stat(filepath.Join(cacheRoot, entry.Name(), "runtime.json")); err == nil {
				versions = append(versions, entry.Name())
			}
		}
	}
	if len(versions) == 0 {
		return Runtime{}, errors.New("managed OpenGrep runtime is not installed")
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareVersion(versions[i], versions[j]) > 0
	})
	return LoadRuntime(filepath.Join(cacheRoot, versions[0], "runtime.json"))
}

func LoadRuntime(path string) (Runtime, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Runtime{}, err
	}
	var runtimeInfo Runtime
	if err := json.Unmarshal(data, &runtimeInfo); err != nil {
		return Runtime{}, err
	}
	if runtimeInfo.Mode == "" {
		if runtimeInfo.Managed {
			runtimeInfo.Mode = "managed"
		} else {
			runtimeInfo.Mode = "path"
		}
	}
	return runtimeInfo, nil
}

func SaveRuntime(path string, runtimeInfo Runtime) error {
	data, err := json.MarshalIndent(runtimeInfo, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func downloadFile(ctx context.Context, client *http.Client, url, destination string, mode os.FileMode) error {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s failed: %s", url, resp.Status)
	}
	tmp := destination + ".tmp"
	file, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, resp.Body)
	closeErr := file.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destination)
}

func binaryFileName() string {
	if runtime.GOOS == "windows" {
		return "opengrep.exe"
	}
	return "opengrep"
}

func compareVersion(a, b string) int {
	as := splitVersion(a)
	bs := splitVersion(b)
	for i := 0; i < len(as) || i < len(bs); i++ {
		av, bv := 0, 0
		if i < len(as) {
			av = as[i]
		}
		if i < len(bs) {
			bv = bs[i]
		}
		if av > bv {
			return 1
		}
		if av < bv {
			return -1
		}
	}
	return 0
}

func splitVersion(value string) []int {
	value = strings.TrimPrefix(value, "v")
	parts := strings.Split(value, ".")
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			n = n*10 + int(r-'0')
		}
		out = append(out, n)
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func commandVersion(path string) (string, error) {
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%s --version failed: %w: %s", path, err, strings.TrimSpace(string(out)))
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		return "unknown", nil
	}
	re := regexp.MustCompile(`v?([0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z.-]+)?)`)
	if match := re.FindStringSubmatch(text); len(match) > 1 {
		return strings.TrimPrefix(match[1], "v"), nil
	}
	fields := strings.Fields(text)
	if len(fields) > 0 {
		return strings.TrimPrefix(fields[len(fields)-1], "v"), nil
	}
	return text, nil
}
