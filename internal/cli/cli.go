package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/archive"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/detect"
	"github.com/greprules/greprules/internal/gitutil"
	"github.com/greprules/greprules/internal/hash"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/output"
	"github.com/greprules/greprules/internal/recommend"
	"github.com/greprules/greprules/internal/registry"
	"gopkg.in/yaml.v3"
)

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	for _, item := range strings.Split(value, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			*s = append(*s, item)
		}
	}
	return nil
}

func Execute(args []string, version string) int {
	if len(args) == 0 {
		printUsage()
		return 0
	}
	ctx := context.Background()
	var err error
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		printUsage()
	case "detect":
		err = runDetect(args[1:])
	case "init":
		err = runInit(args[1:])
	case "config":
		err = runConfig(args[1:])
	case "recommend":
		err = runRecommend(ctx, args[1:])
	case "fetch":
		err = runFetch(ctx, args[1:])
	case "setup-opengrep":
		err = runSetupOpenGrep(ctx, args[1:])
	case "scan":
		err = runScan(ctx, args[1:])
	case "doctor":
		err = runDoctor(ctx, args[1:])
	default:
		err = fmt.Errorf("unknown command: %s", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Println(`greprules is a managed OpenGrep rule-pack scanner.

Usage:
  greprules detect --format json
  greprules init --mode auto
  greprules config inspect --format json
  greprules config set opengrep.mode system --global
  greprules recommend
  greprules fetch [--pack PACK]
  greprules setup-opengrep [--version latest]
  greprules scan [--changed|--full|--target PATH|--targets-from FILE] [--engine managed|system|path]
  greprules doctor [--debug] [--engine managed|system|path]`)
}

func runDetect(args []string) error {
	fs := flag.NewFlagSet("detect", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text or json")
	rootFlag := fs.String("root", ".", "repo root or child path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	result, err := detect.Detect(*rootFlag)
	if err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(result)
	}
	for _, lang := range result.Languages {
		fmt.Printf("language %s confidence=%.2f sources=%s\n", lang.Name, lang.Confidence, strings.Join(lang.Sources, ","))
	}
	for _, framework := range result.Frameworks {
		fmt.Printf("framework %s confidence=%.2f sources=%s\n", framework.Name, framework.Confidence, strings.Join(framework.Sources, ","))
	}
	return nil
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	mode := fs.String("mode", "auto", "mode: auto or manual")
	registryURL := fs.String("registry", config.DefaultRegistry, "greprules registry URL")
	rootFlag := fs.String("root", ".", "repo root or child path")
	engineMode := fs.String("engine", "managed", "OpenGrep engine mode: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path when --engine path")
	opengrepVersion := fs.String("opengrep-version", "latest", "managed OpenGrep version")
	var languages stringList
	var frameworks stringList
	var packs stringList
	fs.Var(&languages, "language", "language override, repeatable or comma-separated")
	fs.Var(&frameworks, "framework", "framework override, repeatable or comma-separated")
	fs.Var(&packs, "pack", "pack override, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	cfg := config.DefaultConfig()
	cfg.Registry = *registryURL
	cfg.Mode = *mode
	cfg.OpenGrep.Mode = *engineMode
	cfg.OpenGrep.Managed = *engineMode == "managed"
	cfg.OpenGrep.Path = *opengrepPath
	cfg.OpenGrep.Version = *opengrepVersion
	if len(languages) == 0 || len(frameworks) == 0 {
		result, err := detect.Detect(root)
		if err != nil {
			return err
		}
		if len(languages) == 0 {
			for _, lang := range result.Languages {
				cfg.Languages = append(cfg.Languages, lang.Name)
			}
		}
		if len(frameworks) == 0 {
			for _, framework := range result.Frameworks {
				cfg.Frameworks = append(cfg.Frameworks, framework.Name)
			}
		}
	}
	if len(languages) > 0 {
		cfg.Languages = languages
	}
	if len(frameworks) > 0 {
		cfg.Frameworks = frameworks
	}
	if len(packs) > 0 {
		cfg.Packs = packs
	}
	localEnginePath := cfg.OpenGrep.Path
	localEngineMode := cfg.OpenGrep.Mode
	localEngineVersion := cfg.OpenGrep.Version
	if localEnginePath != "" {
		cfg.OpenGrep.Path = ""
		if cfg.OpenGrep.Mode == "path" {
			cfg.OpenGrep.Mode = "managed"
			cfg.OpenGrep.Managed = true
		}
	}
	if err := config.SaveConfig(root, cfg); err != nil {
		return err
	}
	if localEnginePath != "" {
		if err := writeConfigPatch(config.LocalConfigPath(root), "json", config.LocalConfigSchemaVersion, map[string]string{
			"opengrep.mode":    localEngineMode,
			"opengrep.path":    localEnginePath,
			"opengrep.version": localEngineVersion,
		}); err != nil {
			return err
		}
		fmt.Println("created", config.LocalConfigPath(root))
	}
	fmt.Println("created", config.ConfigPath(root))
	return nil
}

func runConfig(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules config inspect|set")
	}
	switch args[0] {
	case "inspect":
		return runConfigInspect(args[1:])
	case "set":
		return runConfigSet(args[1:])
	default:
		return fmt.Errorf("unknown config command: %s", args[0])
	}
}

func runConfigInspect(args []string) error {
	fs := flag.NewFlagSet("config inspect", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text or json")
	rootFlag := fs.String("root", ".", "repo root or child path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(resolution)
	}
	fmt.Println("registry:", resolution.Config.Registry)
	fmt.Printf("opengrep: mode=%s version=%s", resolution.Config.OpenGrep.Mode, resolution.Config.OpenGrep.Version)
	if resolution.Config.OpenGrep.Path != "" {
		fmt.Printf(" path=%s", resolution.Config.OpenGrep.Path)
	}
	fmt.Println()
	for _, source := range resolution.Sources {
		if source.Path == "" {
			fmt.Printf("source %s loaded=%t\n", source.Scope, source.Loaded)
		} else {
			fmt.Printf("source %s loaded=%t path=%s\n", source.Scope, source.Loaded, source.Path)
		}
	}
	for _, warning := range resolution.Warnings {
		fmt.Println("warning:", warning)
	}
	return nil
}

func runConfigSet(args []string) error {
	fs := flag.NewFlagSet("config set", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	scope := fs.String("scope", "global", "target scope: global, local, or repo")
	global := fs.Bool("global", false, "write user-level config")
	local := fs.Bool("local", false, "write repo-local config")
	repo := fs.Bool("repo", false, "write shared repo config")
	if err := fs.Parse(normalizeConfigSetArgs(args)); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: greprules config set <key> <value> [--global|--local|--repo]")
	}
	if *global {
		*scope = "global"
	}
	if *local {
		*scope = "local"
	}
	if *repo {
		*scope = "repo"
	}
	key := fs.Arg(0)
	value := fs.Arg(1)
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	var path string
	var format string
	var schemaVersion string
	switch *scope {
	case "global":
		path, err = config.UserConfigPath()
		if err != nil {
			return err
		}
		format = "json"
		schemaVersion = config.UserConfigSchemaVersion
	case "local":
		path = config.LocalConfigPath(root)
		format = "json"
		schemaVersion = config.LocalConfigSchemaVersion
	case "repo":
		if key == "opengrep.path" {
			return errors.New("opengrep.path cannot be written to shared repo config; use --global or --local")
		}
		path = config.ConfigPath(root)
		format = "yaml"
		schemaVersion = config.ConfigSchemaVersion
	default:
		return fmt.Errorf("unsupported config scope: %s", *scope)
	}
	if err := writeConfigPatch(path, format, schemaVersion, map[string]string{key: value}); err != nil {
		return err
	}
	fmt.Println("updated", path)
	return nil
}

func runRecommend(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("recommend", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text or json")
	rootFlag := fs.String("root", ".", "repo root or child path")
	registryURL := fs.String("registry", "", "registry URL override")
	noNetwork := fs.Bool("no-network", false, "skip registry availability lookup")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	resolution, _ := config.LoadEffectiveConfig(root)
	cfg := resolution.Config
	if *registryURL == "" {
		*registryURL = cfg.Registry
	}
	result, err := detect.Detect(root)
	if err != nil {
		return err
	}
	var packs []registry.PackSummary
	if !*noNetwork {
		packs, _ = registry.New(*registryURL).ListPacks(ctx)
	}
	candidates := recommend.ForDetection(result, packs)
	if *format == "json" {
		return printJSON(candidates)
	}
	for _, candidate := range candidates {
		fmt.Printf("%s confidence=%.2f reason=%s\n", candidate.PackID, candidate.Confidence, candidate.Reason)
	}
	return nil
}

func runFetch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	registryURL := fs.String("registry", "", "registry URL override")
	var packFlags stringList
	fs.Var(&packFlags, "pack", "pack slug, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	cfg, err := loadOrDefaultConfig(root)
	if err != nil {
		return err
	}
	if *registryURL != "" {
		cfg.Registry = *registryURL
	}
	packIDs := []string(packFlags)
	if len(packIDs) == 0 {
		packIDs = cfg.Packs
	}
	client := registry.New(cfg.Registry)
	if len(packIDs) == 0 {
		result, err := detect.Detect(root)
		if err != nil {
			return err
		}
		available, _ := client.ListPacks(ctx)
		packIDs = recommend.PackIDs(recommend.ForDetection(result, available))
	}
	if len(packIDs) == 0 {
		return errors.New("no packs selected; use --pack or check detection")
	}
	lockedPacks := make([]config.LockedPack, 0, len(packIDs))
	for _, packID := range packIDs {
		locked, err := fetchPack(ctx, root, cfg, client, packID)
		if err != nil {
			return err
		}
		lockedPacks = append(lockedPacks, locked)
		fmt.Println("fetched", packID)
	}
	lock := config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      cfg.Registry,
		Packs:         lockedPacks,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if runtimeInfo, err := runtimeFromConfigOrLock(config.Lock{}, cfg, "", "", ""); err == nil {
		lock.Engine = lockedEngineFromRuntime(runtimeInfo)
	}
	return config.SaveLock(root, lock)
}

func fetchPack(ctx context.Context, root string, cfg config.Config, client registry.Client, packID string) (config.LockedPack, error) {
	manifestBytes, manifest, err := client.FetchManifest(ctx, packID)
	if err != nil {
		return config.LockedPack{}, err
	}
	tarballBytes, err := client.DownloadPack(ctx, packID)
	if err != nil {
		return config.LockedPack{}, err
	}
	tarballSHA := hash.SHA256Bytes(tarballBytes)
	manifestSHA := hash.SHA256Bytes(manifestBytes)
	cacheRoot := absFromRoot(root, cfg.CacheDir)
	packRoot := filepath.Join(cacheRoot, "packs", packID, tarballSHA)
	if err := os.RemoveAll(packRoot); err != nil {
		return config.LockedPack{}, err
	}
	if err := os.MkdirAll(packRoot, 0o755); err != nil {
		return config.LockedPack{}, err
	}
	manifestPath := filepath.Join(packRoot, "manifest.json")
	tarballPath := filepath.Join(packRoot, "pack.tar.gz")
	extractPath := filepath.Join(packRoot, "contents")
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return config.LockedPack{}, err
	}
	if err := os.WriteFile(tarballPath, tarballBytes, 0o644); err != nil {
		return config.LockedPack{}, err
	}
	if err := os.MkdirAll(extractPath, 0o755); err != nil {
		return config.LockedPack{}, err
	}
	if err := archive.ExtractTarGz(tarballBytes, extractPath); err != nil {
		return config.LockedPack{}, err
	}
	version := manifest.BuildID
	if version == "" {
		version = manifest.GeneratedAt
	}
	if version == "" {
		version = tarballSHA[:12]
	}
	return config.LockedPack{
		ID:             packID,
		Version:        version,
		Source:         cfg.Registry,
		SHA256:         tarballSHA,
		ManifestSHA256: manifestSHA,
		ManifestPath:   relToRoot(root, manifestPath),
		TarballPath:    relToRoot(root, tarballPath),
		RulePath:       relToRoot(root, filepath.Join(extractPath, "rules")),
		TotalRules:     manifest.TotalRules,
		Languages:      manifest.Languages,
		DownloadedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func runSetupOpenGrep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup-opengrep", flag.ContinueOnError)
	version := fs.String("version", "latest", "OpenGrep version or latest")
	force := fs.Bool("force", false, "redownload even when installed")
	rootFlag := fs.String("root", ".", "repo root or child path for lockfile update")
	if err := fs.Parse(args); err != nil {
		return err
	}
	runtimeInfo, err := opengrep.Setup(ctx, opengrep.SetupOptions{Version: *version, Force: *force})
	if err != nil {
		return err
	}
	fmt.Printf("installed OpenGrep %s at %s\n", runtimeInfo.Version, runtimeInfo.Path)
	root, rootErr := detect.FindRepoRoot(*rootFlag)
	if rootErr == nil {
		if lock, err := config.LoadLock(root); err == nil {
			lock.Engine = &config.LockedEngine{
				Name:            runtimeInfo.Name,
				Mode:            runtimeInfo.Mode,
				Version:         runtimeInfo.Version,
				Path:            runtimeInfo.Path,
				Source:          runtimeInfo.Source,
				SHA256:          runtimeInfo.SHA256,
				Managed:         true,
				SignaturePath:   runtimeInfo.SignaturePath,
				CertificatePath: runtimeInfo.CertificatePath,
				DownloadedAt:    runtimeInfo.DownloadedAt,
			}
			return config.SaveLock(root, lock)
		}
	}
	return nil
}

func runScan(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	changed := fs.Bool("changed", true, "scan changed files")
	full := fs.Bool("full", false, "scan full repo")
	sarif := fs.Bool("sarif", true, "write SARIF output")
	engineMode := fs.String("engine", "", "OpenGrep engine mode override: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path override")
	opengrepVersion := fs.String("opengrep-version", "", "managed OpenGrep version override")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	var targetFlags stringList
	fs.Var(&targetFlags, "target", "explicit scan target path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	cfg, err := loadOrDefaultConfig(root)
	if err != nil {
		return err
	}
	lock, err := config.LoadLock(root)
	if err != nil {
		return fmt.Errorf("lockfile missing; run greprules fetch first: %w", err)
	}
	runtimeInfo, err := runtimeFromConfigOrLock(lock, cfg, *engineMode, *opengrepPath, *opengrepVersion)
	if err != nil {
		return fmt.Errorf("OpenGrep runtime is not available: %w", err)
	}
	lock.Engine = lockedEngineFromRuntime(runtimeInfo)
	if err := config.SaveLock(root, lock); err != nil {
		return err
	}
	outputDir := absFromRoot(root, cfg.OutputDir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	explicitTargets, explicitTargetMode, err := scanTargetsFromFlags(root, targetFlags, *targetsFrom)
	if err != nil {
		return err
	}
	if explicitTargetMode && *full {
		return errors.New("--full cannot be combined with --target or --targets-from")
	}
	targets := []string{"."}
	changedMode := *changed && !*full && !explicitTargetMode
	var changedFiles []string
	var emptyTargetsWarning string
	if explicitTargetMode {
		targets = explicitTargets
		if len(targets) == 0 {
			emptyTargetsWarning = "no explicit targets to scan"
		}
	} else if changedMode {
		changedFiles, err = gitutil.ChangedFiles(root)
		if err != nil {
			return err
		}
		targets = changedFiles
		if len(targets) == 0 {
			emptyTargetsWarning = "no changed files to scan"
		}
	}
	jsonPath := filepath.Join(outputDir, "scan.json")
	sarifPath := filepath.Join(outputDir, "scan.sarif")
	agentPath := filepath.Join(outputDir, "agent-result.json")
	startedAt := time.Now().UTC().Format(time.RFC3339)
	result := baseAgentResult(root, lock, runtimeInfo, changedMode, changedFiles, targets, jsonPath, sarifPath)
	result.Scan.StartedAt = startedAt
	if emptyTargetsWarning != "" {
		result.Status = "ok"
		result.Warnings = append(result.Warnings, emptyTargetsWarning)
		result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		return output.WriteAgentResult(agentPath, result)
	}
	rulePath := combinedRulePath(root, lock)
	if err := opengrep.RunScan(ctx, runtimeInfo, opengrep.ScanOptions{
		WorkingDir: root,
		RulePath:   rulePath,
		Targets:    targets,
		OutputPath: jsonPath,
		Format:     "json",
	}); err != nil {
		result.Status = "failed"
		result.Errors = append(result.Errors, err.Error())
		result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		_ = output.WriteAgentResult(agentPath, result)
		return err
	}
	findings, err := output.FindingsFromOpenGrepJSON(jsonPath)
	if err != nil {
		result.Warnings = append(result.Warnings, "could not parse OpenGrep JSON: "+err.Error())
	} else {
		result.Findings = findings
	}
	if *sarif {
		if err := opengrep.RunScan(ctx, runtimeInfo, opengrep.ScanOptions{
			WorkingDir: root,
			RulePath:   rulePath,
			Targets:    targets,
			OutputPath: sarifPath,
			Format:     "sarif",
		}); err != nil {
			result.Warnings = append(result.Warnings, "SARIF run failed: "+err.Error())
		}
	} else {
		result.SARIFPath = ""
	}
	result.Status = "ok"
	result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := output.WriteAgentResult(agentPath, result); err != nil {
		return err
	}
	fmt.Println("wrote", relToRoot(root, agentPath))
	return nil
}

func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	debug := fs.Bool("debug", false, "print debug details")
	format := fs.String("format", "text", "output format: text or json")
	engineMode := fs.String("engine", "", "OpenGrep engine mode override: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path override")
	opengrepVersion := fs.String("opengrep-version", "", "managed OpenGrep version override")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		return err
	}
	cfg := resolution.Config
	report := doctorReport{
		SchemaVersion:       "greprules.doctor.v1",
		Root:                root,
		Config:              resolution,
		RecommendedCommands: []string{},
		Warnings:            append([]string{}, resolution.Warnings...),
	}
	if _, err := registry.New(cfg.Registry).ListPacks(ctx); err != nil {
		report.Registry = checkStatus{OK: false, Error: err.Error(), URL: cfg.Registry}
		report.RecommendedCommands = append(report.RecommendedCommands, "check registry URL or run with --registry")
	} else {
		report.Registry = checkStatus{OK: true, URL: cfg.Registry}
	}
	if lock, err := config.LoadLock(root); err != nil {
		report.Lock = lockStatus{Exists: false, Path: config.LockPath(root), Error: err.Error()}
		report.RecommendedCommands = append(report.RecommendedCommands, "greprules fetch")
	} else {
		report.Lock = lockStatus{Exists: true, Path: config.LockPath(root), PackCount: len(lock.Packs)}
		if *debug {
			report.Lock.Value = &lock
		}
	}
	if runtimeInfo, err := opengrep.Installed(cfg.OpenGrep.Version, ""); err != nil {
		report.OpenGrep.Managed = runtimeCheck{OK: false, Error: err.Error()}
	} else {
		report.OpenGrep.Managed = runtimeCheck{OK: true, Runtime: &runtimeInfo}
	}
	if runtimeInfo, err := opengrep.Resolve(opengrep.ResolveOptions{Mode: "system"}); err != nil {
		report.OpenGrep.System = runtimeCheck{OK: false, Error: err.Error()}
	} else {
		report.OpenGrep.System = runtimeCheck{OK: true, Runtime: &runtimeInfo}
	}
	if runtimeInfo, err := runtimeFromConfigOrLock(config.Lock{}, cfg, *engineMode, *opengrepPath, *opengrepVersion); err != nil {
		report.OpenGrep.Active = runtimeCheck{OK: false, Error: err.Error()}
	} else {
		report.OpenGrep.Active = runtimeCheck{OK: true, Runtime: &runtimeInfo}
	}
	addOpenGrepRecommendations(&report, cfg)
	report.Status = "ok"
	if !report.Registry.OK || !report.Lock.Exists || !report.OpenGrep.Active.OK {
		report.Status = "needs_attention"
	}
	if *format == "json" {
		return printJSON(report)
	}
	printDoctorText(report, *debug)
	return nil
}

type doctorReport struct {
	SchemaVersion       string                  `json:"schemaVersion"`
	Status              string                  `json:"status"`
	Root                string                  `json:"root"`
	Config              config.ConfigResolution `json:"config"`
	Registry            checkStatus             `json:"registry"`
	Lock                lockStatus              `json:"lock"`
	OpenGrep            opengrepStatus          `json:"opengrep"`
	RecommendedCommands []string                `json:"recommendedCommands,omitempty"`
	Warnings            []string                `json:"warnings,omitempty"`
}

type checkStatus struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

type lockStatus struct {
	Exists    bool         `json:"exists"`
	Path      string       `json:"path"`
	PackCount int          `json:"packCount,omitempty"`
	Error     string       `json:"error,omitempty"`
	Value     *config.Lock `json:"value,omitempty"`
}

type opengrepStatus struct {
	Managed runtimeCheck `json:"managed"`
	System  runtimeCheck `json:"system"`
	Active  runtimeCheck `json:"active"`
}

type runtimeCheck struct {
	OK      bool              `json:"ok"`
	Runtime *opengrep.Runtime `json:"runtime,omitempty"`
	Error   string            `json:"error,omitempty"`
}

func addOpenGrepRecommendations(report *doctorReport, cfg config.Config) {
	if report.OpenGrep.Active.OK {
		return
	}

	switch cfg.OpenGrep.Mode {
	case "managed":
		if report.OpenGrep.System.OK {
			addRecommendedCommand(report, "greprules config set opengrep.mode system --global")
			return
		}
		addRecommendedCommand(report, "greprules setup-opengrep")
	case "system":
		if report.OpenGrep.Managed.OK {
			addRecommendedCommand(report, "greprules config set opengrep.mode managed --global")
			return
		}
		addRecommendedCommand(report, "greprules setup-opengrep")
	case "path":
		if report.OpenGrep.System.OK {
			addRecommendedCommand(report, "greprules config set opengrep.mode system --global")
			return
		}
		if report.OpenGrep.Managed.OK {
			addRecommendedCommand(report, "greprules config set opengrep.mode managed --global")
			return
		}
		addRecommendedCommand(report, "greprules setup-opengrep")
	default:
		addRecommendedCommand(report, "greprules doctor --format json")
	}
}

func addRecommendedCommand(report *doctorReport, command string) {
	for _, existing := range report.RecommendedCommands {
		if existing == command {
			return
		}
	}
	report.RecommendedCommands = append(report.RecommendedCommands, command)
}

func printDoctorText(report doctorReport, debug bool) {
	fmt.Println("repo:", report.Root)
	for _, source := range report.Config.Sources {
		if source.Path == "" {
			fmt.Printf("config %s: loaded=%t\n", source.Scope, source.Loaded)
		} else {
			fmt.Printf("config %s: loaded=%t path=%s\n", source.Scope, source.Loaded, source.Path)
		}
	}
	cfg := report.Config.Config
	fmt.Printf("opengrep config: mode=%s version=%s", cfg.OpenGrep.Mode, cfg.OpenGrep.Version)
	if cfg.OpenGrep.Path != "" {
		fmt.Printf(" path=%s", cfg.OpenGrep.Path)
	}
	fmt.Println()
	if report.Registry.OK {
		fmt.Println("registry: ok")
	} else {
		fmt.Println("registry:", report.Registry.Error)
	}
	if report.Lock.Exists {
		fmt.Printf("lock: %d pack(s)\n", report.Lock.PackCount)
		if debug && report.Lock.Value != nil {
			_ = printJSON(report.Lock.Value)
		}
	} else {
		fmt.Println("lock: missing")
	}
	printRuntimeText("opengrep managed", report.OpenGrep.Managed)
	printRuntimeText("opengrep system", report.OpenGrep.System)
	printRuntimeText("opengrep active", report.OpenGrep.Active)
	for _, warning := range report.Warnings {
		fmt.Println("warning:", warning)
	}
	for _, command := range report.RecommendedCommands {
		fmt.Println("recommended:", command)
	}
}

func printRuntimeText(label string, check runtimeCheck) {
	if !check.OK {
		fmt.Println(label+":", "missing")
		return
	}
	fmt.Printf("%s: mode=%s version=%s path=%s\n", label, check.Runtime.Mode, check.Runtime.Version, check.Runtime.Path)
}

func loadOrDefaultConfig(root string) (config.Config, error) {
	resolution, err := config.LoadEffectiveConfig(root)
	if err == nil {
		return resolution.Config, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.DefaultConfig(), nil
	}
	return config.Config{}, err
}

func runtimeFromConfigOrLock(lock config.Lock, cfg config.Config, modeOverride string, pathOverride string, versionOverride string) (opengrep.Runtime, error) {
	mode := cfg.OpenGrep.Mode
	path := cfg.OpenGrep.Path
	version := cfg.OpenGrep.Version
	if modeOverride != "" {
		mode = modeOverride
	}
	if pathOverride != "" {
		path = pathOverride
		if modeOverride == "" {
			mode = "path"
		}
	}
	if versionOverride != "" {
		version = versionOverride
	}
	if mode == "" {
		mode = "managed"
	}
	if mode == "managed" && lock.Engine != nil && lock.Engine.Path != "" && (lock.Engine.Managed || lock.Engine.Mode == "managed") {
		if _, err := os.Stat(lock.Engine.Path); err == nil {
			return opengrep.Runtime{
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
	return opengrep.Resolve(opengrep.ResolveOptions{
		Mode:    mode,
		Path:    path,
		Version: version,
	})
}

func lockedEngineFromRuntime(runtimeInfo opengrep.Runtime) *config.LockedEngine {
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func combinedRulePath(root string, lock config.Lock) string {
	paths := make([]string, 0, len(lock.Packs))
	for _, pack := range lock.Packs {
		paths = append(paths, absFromRoot(root, pack.RulePath))
	}
	return strings.Join(paths, string(os.PathListSeparator))
}

func scanTargetsFromFlags(root string, targets stringList, targetsFrom string) ([]string, bool, error) {
	rawTargets := append([]string{}, targets...)
	explicitMode := len(rawTargets) > 0 || targetsFrom != ""
	if targetsFrom != "" {
		fileTargets, err := readTargetsFile(targetsFrom)
		if err != nil {
			return nil, true, err
		}
		rawTargets = append(rawTargets, fileTargets...)
	}
	if !explicitMode {
		return nil, false, nil
	}
	normalized, err := normalizeScanTargets(root, rawTargets)
	if err != nil {
		return nil, true, err
	}
	return normalized, true, nil
}

func readTargetsFile(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read targets file: %w", err)
	}
	defer file.Close()

	var targets []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		targets = append(targets, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read targets file: %w", err)
	}
	return targets, nil
}

func normalizeScanTargets(root string, rawTargets []string) ([]string, error) {
	seen := map[string]bool{}
	var normalized []string
	for _, raw := range rawTargets {
		target, err := normalizeScanTarget(root, raw)
		if err != nil {
			return nil, err
		}
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		normalized = append(normalized, target)
	}
	sort.Strings(normalized)
	return normalized, nil
}

func normalizeScanTarget(root string, raw string) (string, error) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", nil
	}
	if filepath.IsAbs(target) {
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return "", err
		}
		target = rel
	}
	target = filepath.Clean(target)
	if target == "." {
		return ".", nil
	}
	if target == ".." || strings.HasPrefix(target, ".."+string(os.PathSeparator)) || filepath.IsAbs(target) {
		return "", fmt.Errorf("scan target is outside root: %s", raw)
	}
	if _, err := os.Stat(filepath.Join(root, target)); err != nil {
		return "", fmt.Errorf("scan target is not available: %s: %w", target, err)
	}
	return target, nil
}

func baseAgentResult(root string, lock config.Lock, runtimeInfo opengrep.Runtime, changedMode bool, changedFiles []string, targets []string, jsonPath string, sarifPath string) output.AgentResult {
	packs := make([]output.PackInfo, 0, len(lock.Packs))
	for _, pack := range lock.Packs {
		packs = append(packs, output.PackInfo{
			ID:         pack.ID,
			Version:    pack.Version,
			SHA256:     pack.SHA256,
			RulePath:   pack.RulePath,
			TotalRules: pack.TotalRules,
		})
	}
	return output.AgentResult{
		SchemaVersion: "greprules.agent.v1",
		Status:        "running",
		Repo: output.RepoInfo{
			Root:         root,
			ChangedMode:  changedMode,
			ChangedFiles: changedFiles,
		},
		Packs: packs,
		Engine: output.EngineInfo{
			Name:    runtimeInfo.Name,
			Mode:    runtimeInfo.Mode,
			Source:  runtimeInfo.Source,
			Version: runtimeInfo.Version,
			Path:    runtimeInfo.Path,
			SHA256:  runtimeInfo.SHA256,
			Managed: runtimeInfo.Managed,
		},
		Scan: output.ScanInfo{
			Targets: targets,
		},
		Findings:  []output.Finding{},
		JSONPath:  relToRoot(root, jsonPath),
		SARIFPath: relToRoot(root, sarifPath),
	}
}

func absFromRoot(root, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(root, path)
}

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

func writeConfigPatch(path string, format string, schemaVersion string, values map[string]string) error {
	document, err := readConfigDocument(path, format)
	if err != nil {
		return err
	}
	if _, ok := document["schemaVersion"]; !ok {
		document["schemaVersion"] = schemaVersion
	}
	for key, raw := range values {
		value, err := parseConfigValue(key, raw)
		if err != nil {
			return err
		}
		if err := setConfigDocumentValue(document, key, value); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var data []byte
	switch format {
	case "json":
		data, err = json.MarshalIndent(document, "", "  ")
	case "yaml":
		data, err = yaml.Marshal(document)
	default:
		return fmt.Errorf("unsupported config format: %s", format)
	}
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func normalizeConfigSetArgs(args []string) []string {
	flags := []string{}
	values := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--global" || arg == "--local" || arg == "--repo":
			flags = append(flags, arg)
		case arg == "--root" || arg == "--scope":
			flags = append(flags, arg)
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		case strings.HasPrefix(arg, "--root=") || strings.HasPrefix(arg, "--scope="):
			flags = append(flags, arg)
		default:
			values = append(values, arg)
		}
	}
	return append(flags, values...)
}

func readConfigDocument(path string, format string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	document := map[string]any{}
	switch format {
	case "json":
		err = json.Unmarshal(data, &document)
	case "yaml":
		err = yaml.Unmarshal(data, &document)
	default:
		err = fmt.Errorf("unsupported config format: %s", format)
	}
	if err != nil {
		return nil, err
	}
	if document == nil {
		document = map[string]any{}
	}
	return document, nil
}

func parseConfigValue(key string, raw string) (any, error) {
	switch key {
	case "languages", "frameworks", "packs":
		if strings.HasPrefix(strings.TrimSpace(raw), "[") {
			var values []string
			if err := json.Unmarshal([]byte(raw), &values); err != nil {
				return nil, err
			}
			return values, nil
		}
		if raw == "" {
			return []string{}, nil
		}
		parts := strings.Split(raw, ",")
		values := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				values = append(values, part)
			}
		}
		return values, nil
	case "scan.changedDefault", "scan.sarif", "scan.agentJson", "opengrep.managed":
		return strconv.ParseBool(raw)
	}
	return raw, nil
}

func setConfigDocumentValue(document map[string]any, key string, value any) error {
	if !validConfigKey(key) {
		return fmt.Errorf("unsupported config key: %s", key)
	}
	parts := strings.Split(key, ".")
	current := document
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func validConfigKey(key string) bool {
	switch key {
	case "registry",
		"mode",
		"languages",
		"frameworks",
		"packs",
		"cacheDir",
		"outputDir",
		"scan.changedDefault",
		"scan.sarif",
		"scan.agentJson",
		"opengrep.managed",
		"opengrep.mode",
		"opengrep.path",
		"opengrep.version":
		return true
	default:
		return false
	}
}

func printJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
