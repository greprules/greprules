package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/archive"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/detect"
	"github.com/greprules/greprules/internal/doctor"
	"github.com/greprules/greprules/internal/gitutil"
	"github.com/greprules/greprules/internal/hash"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/projectpath"
	"github.com/greprules/greprules/internal/recommend"
	"github.com/greprules/greprules/internal/registry"
	"github.com/greprules/greprules/internal/runtimeconfig"
	"github.com/greprules/greprules/internal/scanservice"
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
	case "agent-scan":
		err = runAgentScan(ctx, args[1:])
	case "cleanup", "uninstall":
		err = runCleanup(args[1:])
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
  greprules recommend [--agent] [--changed|--target PATH|--targets-from FILE]
  greprules fetch [--pack PACK|--changed|--target PATH|--targets-from FILE]
  greprules setup-opengrep [--version latest]
  greprules scan [--changed|--full|--target PATH|--targets-from FILE] [--output-dir DIR] [--engine managed|system|path] [--no-auto-fetch] [--no-auto-setup] [--explain-selection]
  greprules doctor [--debug] [--engine managed|system|path]
  greprules cleanup [--config|--cache|--opengrep|--plugin-cache|--repo|--all] [--dry-run]`)
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
	if err := ensureGreprulesGitignore(root); err != nil {
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
	fmt.Printf("opengrep: mode=%s version=%s includeDefaultRules=%t", resolution.Config.OpenGrep.Mode, resolution.Config.OpenGrep.Version, resolution.Config.OpenGrep.IncludeDefaultRules)
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
	agent := fs.Bool("agent", false, "print agent-assisted selection context")
	changed := fs.Bool("changed", false, "recommend for git changed, staged, and untracked files")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	var targetFlags stringList
	fs.Var(&targetFlags, "target", "explicit scan target path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := resolveCommandRoot(*rootFlag, *changed)
	if err != nil {
		return err
	}
	resolution, _ := config.LoadEffectiveConfig(root)
	cfg := resolution.Config
	if *registryURL == "" {
		*registryURL = cfg.Registry
	}
	rawTargets, err := collectTargetInputs(root, targetFlags, *targetsFrom, *changed)
	if err != nil {
		return err
	}
	result, err := detectForTargets(root, rawTargets)
	if err != nil {
		return err
	}
	packs, err := registry.New(*registryURL).ListPacks(ctx)
	if err != nil {
		return fmt.Errorf("list registry packs: %w", err)
	}
	candidates := recommend.ForDetection(result, packs)
	if *format == "json" {
		if *agent {
			return printJSON(recommend.BuildAgentContext(result, packs, candidates))
		}
		return printJSON(candidates)
	}
	for _, candidate := range candidates {
		fmt.Printf("%s confidence=%.2f reason=%s\n", candidate.PackID, candidate.Confidence, candidate.Reason)
	}
	return nil
}

func collectTargetInputs(root string, targets []string, targetsFrom string, changed bool) ([]string, error) {
	rawTargets := append([]string{}, targets...)
	if changed {
		changedFiles, err := gitutil.ChangedFiles(root)
		if err != nil {
			return nil, err
		}
		rawTargets = append(rawTargets, changedFiles...)
	}
	if targetsFrom == "" {
		return rawTargets, nil
	}
	data, err := os.ReadFile(targetsFrom)
	if err != nil {
		return nil, fmt.Errorf("read targets file: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rawTargets = append(rawTargets, line)
	}
	return rawTargets, nil
}

func resolveCommandRoot(rootFlag string, discoverGitRoot bool) (string, error) {
	if discoverGitRoot {
		return detect.FindRepoRoot(rootFlag)
	}
	if rootFlag == "" {
		rootFlag = "."
	}
	return filepath.Abs(rootFlag)
}

func detectForTargets(root string, rawTargets []string) (detect.Result, error) {
	if len(rawTargets) > 0 {
		return detect.DetectTargetsExact(root, rawTargets)
	}
	return detect.DetectExact(root)
}

type fetchCommandOptions struct {
	quiet  bool
	stdout io.Writer
}

var errNoPacksSelected = errors.New("no packs selected")

type packSelection struct {
	Detection  detect.Result
	Candidates []recommend.Candidate
	PackIDs    []string
	Source     string
}

func runFetch(ctx context.Context, args []string) error {
	return runFetchWithOptions(ctx, args, fetchCommandOptions{stdout: os.Stdout})
}

func runFetchWithOptions(ctx context.Context, args []string, options fetchCommandOptions) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	registryURL := fs.String("registry", "", "registry URL override")
	changed := fs.Bool("changed", false, "fetch packs for git changed, staged, and untracked files")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	var packFlags stringList
	fs.Var(&packFlags, "pack", "pack slug, repeatable or comma-separated")
	var targetFlags stringList
	fs.Var(&targetFlags, "target", "explicit scan target path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := resolveCommandRoot(*rootFlag, *changed)
	if err != nil {
		return err
	}
	if err := ensureGreprulesGitignore(root); err != nil {
		return err
	}
	cfg, err := runtimeconfig.LoadOrDefaultConfig(root)
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
		selection, err := selectPackIDsForTargets(ctx, root, cfg, client, targetFlags, *targetsFrom, *changed)
		if err != nil {
			return err
		}
		packIDs = selection.PackIDs
	}
	if len(packIDs) == 0 {
		return fmt.Errorf("%w; use --pack or run greprules recommend --format json --agent with the same targets", errNoPacksSelected)
	}
	return fetchAndLockPacks(ctx, root, cfg, client, packIDs, options)
}

func selectPackIDsForTargets(ctx context.Context, root string, cfg config.Config, client registry.Client, targets []string, targetsFrom string, changed bool) (packSelection, error) {
	if len(cfg.Packs) > 0 {
		return packSelection{PackIDs: append([]string{}, cfg.Packs...), Source: "config"}, nil
	}
	rawTargets, err := collectTargetInputs(root, targets, targetsFrom, changed)
	if err != nil {
		return packSelection{}, err
	}
	result, err := detectForTargets(root, rawTargets)
	if err != nil {
		return packSelection{}, err
	}
	available, err := client.ListPacks(ctx)
	if err != nil {
		return packSelection{}, fmt.Errorf("list registry packs: %w", err)
	}
	candidates := recommend.ForDetection(result, available)
	return packSelection{
		Detection:  result,
		Candidates: candidates,
		PackIDs:    recommend.PackIDs(candidates),
		Source:     "detected",
	}, nil
}

func fetchAndLockPacks(ctx context.Context, root string, cfg config.Config, client registry.Client, packIDs []string, options fetchCommandOptions) error {
	lockedPacks := make([]config.LockedPack, 0, len(packIDs))
	for _, packID := range packIDs {
		locked, err := fetchPack(ctx, root, cfg, client, packID)
		if err != nil {
			return err
		}
		lockedPacks = append(lockedPacks, locked)
		if !options.quiet {
			writer := options.stdout
			if writer == nil {
				writer = os.Stdout
			}
			fmt.Fprintln(writer, "fetched", packID)
		}
	}
	lock := config.Lock{
		SchemaVersion: config.LockSchemaVersion,
		Registry:      cfg.Registry,
		Packs:         lockedPacks,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	if runtimeInfo, err := runtimeconfig.FromConfigOrLock(config.Lock{}, cfg, "", "", ""); err == nil {
		lock.Engine = runtimeconfig.LockedEngineFromRuntime(runtimeInfo)
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
	cacheRoot := projectpath.AbsFromRoot(root, cfg.CacheDir)
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
		ManifestPath:   projectpath.RelToRoot(root, manifestPath),
		TarballPath:    projectpath.RelToRoot(root, tarballPath),
		RulePath:       projectpath.RelToRoot(root, filepath.Join(extractPath, "rules")),
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

type scanCommandOptions struct {
	quiet       bool
	stdout      io.Writer
	stderr      io.Writer
	autoPrepare bool
}

func runScan(ctx context.Context, args []string) error {
	return runScanWithOptions(ctx, args, scanCommandOptions{stdout: os.Stdout, stderr: os.Stderr, autoPrepare: true})
}

func runScanWithOptions(ctx context.Context, args []string, options scanCommandOptions) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	registryURL := fs.String("registry", "", "registry URL override for automatic rule-pack fetch")
	changed := fs.Bool("changed", false, "scan changed files")
	full := fs.Bool("full", false, "scan full repo")
	sarif := fs.Bool("sarif", true, "write SARIF output")
	engineMode := fs.String("engine", "", "OpenGrep engine mode override: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path override")
	opengrepVersion := fs.String("opengrep-version", "", "managed OpenGrep version override")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	outputDir := fs.String("output-dir", "", "scan output directory override")
	noAutoFetch := fs.Bool("no-auto-fetch", false, "do not fetch rule packs automatically when the lockfile is missing")
	noAutoSetup := fs.Bool("no-auto-setup", false, "do not install managed OpenGrep automatically when it is missing")
	explainSelection := fs.Bool("explain-selection", false, "print detected context and selected rule packs before scanning")
	var targetFlags stringList
	fs.Var(&targetFlags, "target", "explicit scan target path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := resolveCommandRoot(*rootFlag, *changed)
	if err != nil {
		return err
	}
	if err := ensureGreprulesGitignore(root); err != nil {
		return err
	}
	if options.autoPrepare {
		if err := prepareStandaloneScan(ctx, standaloneScanOptions{
			Root:             root,
			RegistryURL:      *registryURL,
			Changed:          *changed,
			Targets:          []string(targetFlags),
			TargetsFrom:      *targetsFrom,
			AutoFetch:        !*noAutoFetch,
			AutoSetup:        !*noAutoSetup,
			ExplainSelection: *explainSelection,
			EngineMode:       *engineMode,
			OpenGrepPath:     *opengrepPath,
			OpenGrepVersion:  *opengrepVersion,
			Quiet:            options.quiet,
			Stdout:           options.stdout,
		}); err != nil {
			return err
		}
	}
	return scanservice.Run(ctx, scanservice.Options{
		Root:            root,
		Changed:         *changed,
		Full:            *full,
		SARIF:           *sarif,
		EngineMode:      *engineMode,
		OpenGrepPath:    *opengrepPath,
		OpenGrepVersion: *opengrepVersion,
		Targets:         []string(targetFlags),
		TargetsFrom:     *targetsFrom,
		OutputDir:       *outputDir,
		Quiet:           options.quiet,
		Stdout:          options.stdout,
		Stderr:          options.stderr,
	})
}

type standaloneScanOptions struct {
	Root             string
	RegistryURL      string
	Changed          bool
	Targets          []string
	TargetsFrom      string
	AutoFetch        bool
	AutoSetup        bool
	ExplainSelection bool
	EngineMode       string
	OpenGrepPath     string
	OpenGrepVersion  string
	Quiet            bool
	Stdout           io.Writer
}

func prepareStandaloneScan(ctx context.Context, options standaloneScanOptions) error {
	cfg, err := runtimeconfig.LoadOrDefaultConfig(options.Root)
	if err != nil {
		return err
	}
	if options.RegistryURL != "" {
		cfg.Registry = options.RegistryURL
	}
	writer := options.Stdout
	if writer == nil {
		writer = os.Stdout
	}
	lock, lockErr := config.LoadLock(options.Root)
	lockReady := lockErr == nil && len(lock.Packs) > 0
	if lockErr != nil && !errors.Is(lockErr, os.ErrNotExist) {
		return fmt.Errorf("read lockfile: %w", lockErr)
	}

	if lockReady {
		if options.ExplainSelection && !options.Quiet {
			printLockedPackSelection(writer, lock)
		}
	} else if options.AutoFetch {
		client := registry.New(cfg.Registry)
		selection, err := selectPackIDsForTargets(ctx, options.Root, cfg, client, options.Targets, options.TargetsFrom, options.Changed)
		if err != nil {
			return err
		}
		if options.ExplainSelection && !options.Quiet {
			printPackSelection(writer, selection, "fetching selected rule packs")
		} else if !options.Quiet && len(selection.PackIDs) > 0 {
			fmt.Fprintln(writer, "selected rule packs:", strings.Join(selection.PackIDs, ", "))
		}
		if len(selection.PackIDs) == 0 {
			return fmt.Errorf("%w; use --pack, configure packs, or run greprules recommend with the same targets", errNoPacksSelected)
		}
		if err := fetchAndLockPacks(ctx, options.Root, cfg, client, selection.PackIDs, fetchCommandOptions{quiet: options.Quiet, stdout: options.Stdout}); err != nil {
			return err
		}
		lock, lockErr = config.LoadLock(options.Root)
		lockReady = lockErr == nil && len(lock.Packs) > 0
	} else if options.ExplainSelection && !options.Quiet {
		client := registry.New(cfg.Registry)
		selection, err := selectPackIDsForTargets(ctx, options.Root, cfg, client, options.Targets, options.TargetsFrom, options.Changed)
		if err != nil {
			return err
		}
		printPackSelection(writer, selection, "auto-fetch disabled")
	}

	if options.AutoSetup && lockReady {
		if err := ensureStandaloneOpenGrep(ctx, options.Root, cfg, lock, options); err != nil {
			return err
		}
	}
	return nil
}

func ensureStandaloneOpenGrep(ctx context.Context, root string, cfg config.Config, lock config.Lock, options standaloneScanOptions) error {
	if _, err := runtimeconfig.FromConfigOrLock(lock, cfg, options.EngineMode, options.OpenGrepPath, options.OpenGrepVersion); err == nil {
		return nil
	}
	mode := options.EngineMode
	if mode == "" {
		mode = cfg.OpenGrep.Mode
	}
	if mode == "" {
		mode = "managed"
	}
	if mode != "managed" {
		return nil
	}
	version := options.OpenGrepVersion
	if version == "" {
		version = cfg.OpenGrep.Version
	}
	runtimeInfo, err := opengrep.Setup(ctx, opengrep.SetupOptions{Version: version})
	if err != nil {
		return fmt.Errorf("OpenGrep runtime is not available and automatic setup failed; run greprules setup-opengrep: %w", err)
	}
	if !options.Quiet {
		writer := options.Stdout
		if writer == nil {
			writer = os.Stdout
		}
		fmt.Fprintf(writer, "installed OpenGrep %s at %s\n", runtimeInfo.Version, runtimeInfo.Path)
	}
	lock.Engine = runtimeconfig.LockedEngineFromRuntime(runtimeInfo)
	return config.SaveLock(root, lock)
}

func printPackSelection(writer io.Writer, selection packSelection, note string) {
	if len(selection.Detection.Languages) > 0 {
		fmt.Fprintln(writer, "detected languages:")
		for _, language := range selection.Detection.Languages {
			fmt.Fprintf(writer, "  %s confidence=%.2f sources=%s\n", language.Name, language.Confidence, strings.Join(language.Sources, ","))
		}
	}
	if len(selection.Detection.Frameworks) > 0 {
		fmt.Fprintln(writer, "detected frameworks:")
		for _, framework := range selection.Detection.Frameworks {
			fmt.Fprintf(writer, "  %s confidence=%.2f sources=%s\n", framework.Name, framework.Confidence, strings.Join(framework.Sources, ","))
		}
	}
	if len(selection.PackIDs) == 0 {
		fmt.Fprintln(writer, "selected rule packs: none")
		return
	}
	fmt.Fprintln(writer, "selected rule packs:")
	if selection.Source == "config" {
		for _, packID := range selection.PackIDs {
			fmt.Fprintf(writer, "  %s reason=configured in greprules config\n", packID)
		}
	} else {
		for _, candidate := range selection.Candidates {
			fmt.Fprintf(writer, "  %s confidence=%.2f reason=%s\n", candidate.PackID, candidate.Confidence, candidate.Reason)
		}
	}
	if note != "" {
		fmt.Fprintln(writer, "selection:", note)
	}
}

func printLockedPackSelection(writer io.Writer, lock config.Lock) {
	if len(lock.Packs) == 0 {
		fmt.Fprintln(writer, "using locked rule packs: none")
		return
	}
	fmt.Fprintln(writer, "using locked rule packs:")
	for _, pack := range lock.Packs {
		fmt.Fprintf(writer, "  %s version=%s rules=%d\n", pack.ID, pack.Version, pack.TotalRules)
	}
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
	report, err := doctor.Build(ctx, root, doctor.Options{
		Debug:           *debug,
		EngineMode:      *engineMode,
		OpenGrepPath:    *opengrepPath,
		OpenGrepVersion: *opengrepVersion,
	})
	if err != nil {
		return err
	}
	if *format == "json" {
		return printJSON(report)
	}
	doctor.PrintText(os.Stdout, report, *debug)
	return nil
}

func runCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	configFlag := fs.Bool("config", false, "remove user-level greprules config")
	cacheFlag := fs.Bool("cache", false, "remove all user-level greprules caches")
	opengrepFlag := fs.Bool("opengrep", false, "remove managed OpenGrep cache")
	pluginCacheFlag := fs.Bool("plugin-cache", false, "remove agent plugin CLI bootstrap caches")
	repoFlag := fs.Bool("repo", false, "remove repo-local .greprules directory")
	allFlag := fs.Bool("all", false, "remove user config, user caches, and repo-local .greprules")
	purgeFlag := fs.Bool("purge", false, "remove user config and user caches")
	dryRun := fs.Bool("dry-run", false, "print cleanup targets without deleting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *allFlag {
		*configFlag = true
		*cacheFlag = true
		*repoFlag = true
	}
	if *purgeFlag {
		*configFlag = true
		*cacheFlag = true
	}

	targets, err := cleanupTargets(*rootFlag, *configFlag, *cacheFlag, *opengrepFlag, *pluginCacheFlag, *repoFlag)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Println("no cleanup target selected")
		fmt.Println("use one or more of: --config, --cache, --opengrep, --plugin-cache, --repo, --purge, --all")
		return nil
	}
	for _, target := range targets {
		if *dryRun {
			fmt.Println("would remove", target)
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		fmt.Println("removed", target)
	}
	return nil
}

var greprulesGitignoreEntries = []string{
	".greprules/cache/",
	".greprules/out/",
	".greprules/plugin-data/",
	".greprules/config.local.json",
}

func ensureGreprulesGitignore(root string) error {
	if !isGitWorkTree(root) {
		return nil
	}
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	missing := missingGreprulesGitignoreEntries(data)
	if len(missing) == 0 {
		return nil
	}
	var builder strings.Builder
	if len(data) > 0 {
		builder.Write(data)
		if !strings.HasSuffix(string(data), "\n") {
			builder.WriteByte('\n')
		}
	}
	for _, entry := range missing {
		builder.WriteString(entry)
		builder.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func isGitWorkTree(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func missingGreprulesGitignoreEntries(data []byte) []string {
	lines := gitignoreEffectiveLines(data)
	if lines[".greprules"] || lines[".greprules/"] || lines[".greprules/**"] {
		return nil
	}
	missing := []string{}
	for _, entry := range greprulesGitignoreEntries {
		if gitignoreEntryExists(lines, entry) {
			continue
		}
		missing = append(missing, entry)
	}
	return missing
}

func gitignoreEffectiveLines(data []byte) map[string]bool {
	lines := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines[line] = true
	}
	return lines
}

func gitignoreEntryExists(lines map[string]bool, entry string) bool {
	if lines[entry] {
		return true
	}
	if strings.HasSuffix(entry, "/") && lines[strings.TrimSuffix(entry, "/")] {
		return true
	}
	return false
}

func cleanupTargets(rootFlag string, removeConfig bool, removeCache bool, removeOpenGrep bool, removePluginCache bool, removeRepo bool) ([]string, error) {
	seen := map[string]bool{}
	targets := []string{}
	add := func(path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if !seen[clean] {
			seen[clean] = true
			targets = append(targets, clean)
		}
	}

	if removeConfig {
		path, err := config.UserConfigPath()
		if err != nil {
			return nil, err
		}
		add(path)
	}
	if removeCache {
		root, err := greprulesUserCacheRoot()
		if err != nil {
			return nil, err
		}
		add(root)
	} else {
		if removeOpenGrep {
			root, err := opengrep.DefaultCacheRoot()
			if err != nil {
				return nil, err
			}
			add(root)
		}
		if removePluginCache {
			root, err := greprulesPluginCacheRoot()
			if err != nil {
				return nil, err
			}
			add(root)
		}
	}
	if removeRepo {
		root, err := detect.FindRepoRoot(rootFlag)
		if err != nil {
			return nil, err
		}
		add(filepath.Join(root, ".greprules"))
	}
	sort.Strings(targets)
	return targets, nil
}

func greprulesUserCacheRoot() (string, error) {
	root, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "greprules"), nil
}

func greprulesPluginCacheRoot() (string, error) {
	root, err := greprulesUserCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "plugins"), nil
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
	case "scan.changedDefault", "scan.sarif", "scan.agentJson", "opengrep.managed", "opengrep.includeDefaultRules":
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
		"opengrep.includeDefaultRules",
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
