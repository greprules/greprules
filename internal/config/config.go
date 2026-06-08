package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	ConfigSchemaVersion      = "greprules.config.v1"
	UserConfigSchemaVersion  = "greprules.user.v1"
	LocalConfigSchemaVersion = "greprules.local.v1"
	LockSchemaVersion        = "greprules.lock.v1"
	DefaultRegistry          = "https://api.greprules.io"
)

type Config struct {
	SchemaVersion string       `json:"schemaVersion" yaml:"schemaVersion"`
	Registry      string       `json:"registry" yaml:"registry"`
	Mode          string       `json:"mode" yaml:"mode"`
	Languages     []string     `json:"languages" yaml:"languages"`
	Frameworks    []string     `json:"frameworks" yaml:"frameworks"`
	Packs         []string     `json:"packs" yaml:"packs"`
	CacheDir      string       `json:"cacheDir" yaml:"cacheDir"`
	OutputDir     string       `json:"outputDir" yaml:"outputDir"`
	Scan          ScanConfig   `json:"scan" yaml:"scan"`
	OpenGrep      EngineConfig `json:"opengrep" yaml:"opengrep"`
}

type ScanConfig struct {
	ChangedDefault bool `json:"changedDefault" yaml:"changedDefault"`
	SARIF          bool `json:"sarif" yaml:"sarif"`
	AgentJSON      bool `json:"agentJson" yaml:"agentJson"`
}

type EngineConfig struct {
	Managed             bool   `json:"managed" yaml:"managed"`
	Mode                string `json:"mode" yaml:"mode"`
	Version             string `json:"version" yaml:"version"`
	Path                string `json:"path" yaml:"path"`
	IncludeDefaultRules bool   `json:"includeDefaultRules" yaml:"includeDefaultRules"`
}

type ConfigResolution struct {
	SchemaVersion string         `json:"schemaVersion"`
	Config        Config         `json:"config"`
	Sources       []ConfigSource `json:"sources"`
	Warnings      []string       `json:"warnings,omitempty"`
}

type ConfigSource struct {
	Scope  string `json:"scope"`
	Path   string `json:"path,omitempty"`
	Loaded bool   `json:"loaded"`
}

type ConfigPatch struct {
	SchemaVersion *string           `json:"schemaVersion" yaml:"schemaVersion"`
	Registry      *string           `json:"registry" yaml:"registry"`
	Mode          *string           `json:"mode" yaml:"mode"`
	Languages     *[]string         `json:"languages" yaml:"languages"`
	Frameworks    *[]string         `json:"frameworks" yaml:"frameworks"`
	Packs         *[]string         `json:"packs" yaml:"packs"`
	CacheDir      *string           `json:"cacheDir" yaml:"cacheDir"`
	OutputDir     *string           `json:"outputDir" yaml:"outputDir"`
	Scan          ScanConfigPatch   `json:"scan" yaml:"scan"`
	OpenGrep      EngineConfigPatch `json:"opengrep" yaml:"opengrep"`
}

type ScanConfigPatch struct {
	ChangedDefault *bool `json:"changedDefault" yaml:"changedDefault"`
	SARIF          *bool `json:"sarif" yaml:"sarif"`
	AgentJSON      *bool `json:"agentJson" yaml:"agentJson"`
}

type EngineConfigPatch struct {
	Managed             *bool   `json:"managed" yaml:"managed"`
	Mode                *string `json:"mode" yaml:"mode"`
	Version             *string `json:"version" yaml:"version"`
	Path                *string `json:"path" yaml:"path"`
	IncludeDefaultRules *bool   `json:"includeDefaultRules" yaml:"includeDefaultRules"`
}

type Lock struct {
	SchemaVersion string         `json:"schemaVersion"`
	Registry      string         `json:"registry"`
	Packs         []LockedPack   `json:"packs"`
	Engine        *LockedEngine  `json:"engine,omitempty"`
	GeneratedAt   string         `json:"generatedAt"`
	Metadata      map[string]any `json:"metadata,omitempty"`
}

type LockedPack struct {
	ID             string   `json:"id"`
	Version        string   `json:"version"`
	Source         string   `json:"source"`
	SHA256         string   `json:"sha256"`
	ManifestSHA256 string   `json:"manifestSha256"`
	ManifestPath   string   `json:"manifestPath"`
	TarballPath    string   `json:"tarballPath"`
	RulePath       string   `json:"rulePath"`
	TotalRules     int      `json:"totalRules"`
	Languages      []string `json:"languages,omitempty"`
	DownloadedAt   string   `json:"downloadedAt"`
}

type LockedEngine struct {
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

func DefaultConfig() Config {
	return Config{
		SchemaVersion: ConfigSchemaVersion,
		Registry:      DefaultRegistry,
		Mode:          "auto",
		Languages:     []string{},
		Frameworks:    []string{},
		Packs:         []string{},
		CacheDir:      ".greprules/cache",
		OutputDir:     ".greprules/out",
		Scan: ScanConfig{
			ChangedDefault: true,
			SARIF:          true,
			AgentJSON:      true,
		},
		OpenGrep: EngineConfig{
			Managed:             true,
			Mode:                "managed",
			Version:             "latest",
			Path:                "",
			IncludeDefaultRules: false,
		},
	}
}

func ConfigPath(root string) string {
	return filepath.Join(root, ".greprules", "config.yaml")
}

func LocalConfigPath(root string) string {
	return filepath.Join(root, ".greprules", "config.local.json")
}

func UserConfigPath() (string, error) {
	if explicit := os.Getenv("GREPRULES_USER_CONFIG"); explicit != "" {
		return expandHome(explicit), nil
	}
	if home := os.Getenv("GREPRULES_CONFIG_HOME"); home != "" {
		return filepath.Join(expandHome(home), "greprules", "config.json"), nil
	}
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "greprules", "config.json"), nil
}

func LockPath(root string) string {
	return filepath.Join(root, ".greprules", "lock.json")
}

func LoadConfig(root string) (Config, error) {
	path := ConfigPath(root)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, err
		}
		return Config{}, err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	normalizeConfig(&cfg)
	return cfg, nil
}

func LoadEffectiveConfig(root string) (ConfigResolution, error) {
	cfg := DefaultConfig()
	resolution := ConfigResolution{
		SchemaVersion: "greprules.config.resolution.v1",
		Sources: []ConfigSource{
			{Scope: "default", Loaded: true},
		},
	}

	userPath, err := UserConfigPath()
	if err != nil {
		resolution.Warnings = append(resolution.Warnings, "could not resolve user config path: "+err.Error())
	} else {
		loaded, warnings, err := applyPatchFile(&cfg, userPath, "global", true, json.Unmarshal)
		if err != nil {
			return resolution, err
		}
		resolution.Sources = append(resolution.Sources, ConfigSource{Scope: "global", Path: userPath, Loaded: loaded})
		resolution.Warnings = append(resolution.Warnings, warnings...)
	}

	sharedPath := ConfigPath(root)
	loaded, warnings, err := applyPatchFile(&cfg, sharedPath, "repo", false, yaml.Unmarshal)
	if err != nil {
		return resolution, err
	}
	resolution.Sources = append(resolution.Sources, ConfigSource{Scope: "repo", Path: sharedPath, Loaded: loaded})
	resolution.Warnings = append(resolution.Warnings, warnings...)

	localPath := LocalConfigPath(root)
	loaded, warnings, err = applyPatchFile(&cfg, localPath, "local", true, json.Unmarshal)
	if err != nil {
		return resolution, err
	}
	resolution.Sources = append(resolution.Sources, ConfigSource{Scope: "local", Path: localPath, Loaded: loaded})
	resolution.Warnings = append(resolution.Warnings, warnings...)

	warnings = applyEnv(&cfg)
	resolution.Warnings = append(resolution.Warnings, warnings...)
	normalizeConfig(&cfg)
	resolution.Config = cfg
	return resolution, nil
}

func SaveConfig(root string, cfg Config) error {
	normalizeConfig(&cfg)
	if err := os.MkdirAll(filepath.Dir(ConfigPath(root)), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(ConfigPath(root), data, 0o644)
}

func SaveLocalConfig(root string, cfg Config) error {
	normalizeConfig(&cfg)
	if cfg.SchemaVersion == "" || cfg.SchemaVersion == ConfigSchemaVersion {
		cfg.SchemaVersion = LocalConfigSchemaVersion
	}
	return SaveConfigJSON(LocalConfigPath(root), cfg)
}

func SaveUserConfig(cfg Config) error {
	normalizeConfig(&cfg)
	cfg.SchemaVersion = UserConfigSchemaVersion
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	return SaveConfigJSON(path, cfg)
}

func SaveConfigJSON(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func LoadLock(root string) (Lock, error) {
	data, err := os.ReadFile(LockPath(root))
	if err != nil {
		return Lock{}, err
	}
	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return Lock{}, err
	}
	return lock, nil
}

func SaveLock(root string, lock Lock) error {
	if lock.SchemaVersion == "" {
		lock.SchemaVersion = LockSchemaVersion
	}
	if lock.GeneratedAt == "" {
		lock.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := os.MkdirAll(filepath.Dir(LockPath(root)), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(LockPath(root), data, 0o644)
}

func normalizeConfig(cfg *Config) {
	if cfg.SchemaVersion == "" {
		cfg.SchemaVersion = ConfigSchemaVersion
	}
	if cfg.Registry == "" {
		cfg.Registry = DefaultRegistry
	}
	if cfg.Mode == "" {
		cfg.Mode = "auto"
	}
	if cfg.CacheDir == "" {
		cfg.CacheDir = ".greprules/cache"
	}
	if cfg.OutputDir == "" {
		cfg.OutputDir = ".greprules/out"
	}
	if !cfg.Scan.SARIF && !cfg.Scan.AgentJSON {
		cfg.Scan.SARIF = true
		cfg.Scan.AgentJSON = true
	}
	if cfg.OpenGrep.Mode == "" {
		if cfg.OpenGrep.Path != "" {
			cfg.OpenGrep.Mode = "path"
		} else if !cfg.OpenGrep.Managed {
			cfg.OpenGrep.Mode = "system"
		} else {
			cfg.OpenGrep.Mode = "managed"
		}
	}
	if cfg.OpenGrep.Mode != "managed" && cfg.OpenGrep.Mode != "system" && cfg.OpenGrep.Mode != "path" {
		cfg.OpenGrep.Mode = "managed"
	}
	cfg.OpenGrep.Managed = cfg.OpenGrep.Mode == "managed"
	if cfg.OpenGrep.Version == "" {
		cfg.OpenGrep.Version = "latest"
	}
}

func applyPatchFile(cfg *Config, path string, scope string, allowEnginePath bool, unmarshal func([]byte, any) error) (bool, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil, nil
		}
		return false, nil, err
	}
	var patch ConfigPatch
	if err := unmarshal(data, &patch); err != nil {
		return false, nil, err
	}
	return true, applyPatch(cfg, patch, scope, allowEnginePath), nil
}

func applyPatch(cfg *Config, patch ConfigPatch, scope string, allowEnginePath bool) []string {
	var warnings []string
	if patch.SchemaVersion != nil {
		cfg.SchemaVersion = *patch.SchemaVersion
	}
	if patch.Registry != nil {
		cfg.Registry = *patch.Registry
	}
	if patch.Mode != nil {
		cfg.Mode = *patch.Mode
	}
	if patch.Languages != nil {
		cfg.Languages = *patch.Languages
	}
	if patch.Frameworks != nil {
		cfg.Frameworks = *patch.Frameworks
	}
	if patch.Packs != nil {
		cfg.Packs = *patch.Packs
	}
	if patch.CacheDir != nil {
		cfg.CacheDir = *patch.CacheDir
	}
	if patch.OutputDir != nil {
		cfg.OutputDir = *patch.OutputDir
	}
	if patch.Scan.ChangedDefault != nil {
		cfg.Scan.ChangedDefault = *patch.Scan.ChangedDefault
	}
	if patch.Scan.SARIF != nil {
		cfg.Scan.SARIF = *patch.Scan.SARIF
	}
	if patch.Scan.AgentJSON != nil {
		cfg.Scan.AgentJSON = *patch.Scan.AgentJSON
	}
	if patch.OpenGrep.Managed != nil {
		cfg.OpenGrep.Managed = *patch.OpenGrep.Managed
	}
	if patch.OpenGrep.Mode != nil {
		cfg.OpenGrep.Mode = *patch.OpenGrep.Mode
	}
	if patch.OpenGrep.Version != nil {
		cfg.OpenGrep.Version = *patch.OpenGrep.Version
	}
	if patch.OpenGrep.Path != nil {
		if allowEnginePath {
			cfg.OpenGrep.Path = *patch.OpenGrep.Path
		} else {
			warnings = append(warnings, scope+" config opengrep.path ignored; put executable paths in global config, local config, env, or CLI flags")
		}
	}
	if patch.OpenGrep.IncludeDefaultRules != nil {
		cfg.OpenGrep.IncludeDefaultRules = *patch.OpenGrep.IncludeDefaultRules
	}
	normalizeConfig(cfg)
	return warnings
}

func applyEnv(cfg *Config) []string {
	var warnings []string
	if value := os.Getenv("GREPRULES_REGISTRY"); value != "" {
		cfg.Registry = value
	}
	if value := os.Getenv("GREPRULES_ENGINE"); value != "" {
		cfg.OpenGrep.Mode = value
	}
	if value := os.Getenv("GREPRULES_OPENGREP_PATH"); value != "" {
		cfg.OpenGrep.Path = expandHome(value)
		if os.Getenv("GREPRULES_ENGINE") == "" {
			cfg.OpenGrep.Mode = "path"
		}
	}
	if value := os.Getenv("GREPRULES_OPENGREP_VERSION"); value != "" {
		cfg.OpenGrep.Version = value
	}
	if value := os.Getenv("GREPRULES_OPENGREP_INCLUDE_DEFAULT_RULES"); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			warnings = append(warnings, "GREPRULES_OPENGREP_INCLUDE_DEFAULT_RULES ignored; expected boolean")
		} else {
			cfg.OpenGrep.IncludeDefaultRules = parsed
		}
	}
	if value := os.Getenv("GREPRULES_CACHE_DIR"); value != "" {
		cfg.CacheDir = value
	}
	if value := os.Getenv("GREPRULES_OUTPUT_DIR"); value != "" {
		cfg.OutputDir = value
	}
	if cfg.OpenGrep.Mode == "path" && cfg.OpenGrep.Path == "" {
		warnings = append(warnings, "opengrep.mode is path but opengrep.path is empty")
	}
	return warnings
}

func expandHome(path string) string {
	if path == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return path
}
