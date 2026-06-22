package rules

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/utils"
)

var ErrNoPacksSelected = errors.New("no packs selected")

type Request struct {
	Root        string
	Changed     bool
	Targets     []string
	TargetsFrom string
}

type Selection struct {
	Detection  Result
	Candidates []Candidate
	PackIDs    []string
	Source     string
}

type FetchOptions struct {
	Quiet  bool
	Stdout io.Writer
}

type EnsurePolicy struct {
	AutoFetch bool
	Verbose   bool
}

type EnsureResult struct {
	Config    config.Config
	Lock      config.Lock
	LockReady bool
	Selection Selection
	Fetched   bool
}

type EnsureIO struct {
	Quiet  bool
	Stdout io.Writer
}

func Ensure(ctx context.Context, request Request, policy EnsurePolicy, ioOptions EnsureIO) (EnsureResult, error) {
	cfg, err := config.LoadEffectiveOrDefault(request.Root)
	if err != nil {
		return EnsureResult{}, err
	}
	writer := ioOptions.Stdout
	if writer == nil {
		writer = os.Stdout
	}
	lock, lockErr := config.LoadLock(request.Root)
	lockReady := lockErr == nil && config.LockArtifactsReady(request.Root, lock)
	if lockErr != nil && !errors.Is(lockErr, os.ErrNotExist) {
		return EnsureResult{}, fmt.Errorf("read lockfile: %w", lockErr)
	}
	result := EnsureResult{Config: cfg, Lock: lock, LockReady: lockReady}

	if lockReady {
		if !ioOptions.Quiet {
			PrintCachedPacks(writer, lock, policy.Verbose)
		}
	} else if policy.AutoFetch {
		client := NewRegistry(cfg.Registry)
		selection, err := SelectForTargets(ctx, request.Root, cfg, client, request.Targets, request.TargetsFrom, request.Changed)
		if err != nil {
			return result, err
		}
		result.Selection = selection
		if policy.Verbose && !ioOptions.Quiet {
			PrintSelection(writer, selection, "fetching selected rule packs")
		} else if !ioOptions.Quiet && len(selection.PackIDs) > 0 {
			fmt.Fprintln(writer, "selected rule packs:", strings.Join(selection.PackIDs, ", "))
		}
		if len(selection.PackIDs) == 0 {
			return result, ErrNoPacksSelected
		}
		if err := FetchAndLock(ctx, request.Root, cfg, client, selection.PackIDs, FetchOptions{Quiet: ioOptions.Quiet, Stdout: ioOptions.Stdout}); err != nil {
			return result, err
		}
		result.Fetched = true
		lock, lockErr = config.LoadLock(request.Root)
		lockReady = lockErr == nil && config.LockArtifactsReady(request.Root, lock)
		result.Lock = lock
		result.LockReady = lockReady
	} else if policy.Verbose && !ioOptions.Quiet {
		client := NewRegistry(cfg.Registry)
		selection, err := SelectForTargets(ctx, request.Root, cfg, client, request.Targets, request.TargetsFrom, request.Changed)
		if err != nil {
			return result, err
		}
		result.Selection = selection
		PrintSelection(writer, selection, "automatic preparation disabled")
	}

	return result, nil
}

func CollectTargetInputs(root string, targets []string, targetsFrom string, changed bool) ([]string, error) {
	rawTargets := append([]string{}, targets...)
	if changed {
		changedFiles, err := utils.ChangedFiles(root)
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

func DetectForTargets(root string, rawTargets []string) (Result, error) {
	if len(rawTargets) > 0 {
		return DetectTargetsExact(root, rawTargets)
	}
	return DetectExact(root)
}

func SelectForTargets(ctx context.Context, root string, cfg config.Config, client Client, targets []string, targetsFrom string, changed bool) (Selection, error) {
	if len(cfg.Packs) > 0 {
		return Selection{PackIDs: append([]string{}, cfg.Packs...), Source: "config"}, nil
	}
	rawTargets, err := CollectTargetInputs(root, targets, targetsFrom, changed)
	if err != nil {
		return Selection{}, err
	}
	result, err := DetectForTargets(root, rawTargets)
	if err != nil {
		return Selection{}, err
	}
	available, err := client.ListPacks(ctx)
	if err != nil {
		return Selection{}, fmt.Errorf("list registry packs: %w", err)
	}
	candidates := ForDetection(result, available)
	return Selection{
		Detection:  result,
		Candidates: candidates,
		PackIDs:    PackIDs(candidates),
		Source:     "detected",
	}, nil
}

func FetchAndLock(ctx context.Context, root string, cfg config.Config, client Client, packIDs []string, options FetchOptions) error {
	lockedPacks := make([]config.LockedPack, 0, len(packIDs))
	for _, packID := range packIDs {
		locked, err := FetchPack(ctx, root, cfg, client, packID)
		if err != nil {
			return err
		}
		lockedPacks = append(lockedPacks, locked)
		if !options.Quiet {
			writer := options.Stdout
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
	if runtimeInfo, err := opengrep.ResolveFromConfig(config.Lock{}, cfg, opengrep.ConfigOverrides{}); err == nil {
		lock.Engine = opengrep.LockedEngineFromRuntime(runtimeInfo)
	}
	return config.SaveLock(root, lock)
}

func FetchPack(ctx context.Context, root string, cfg config.Config, client Client, packID string) (config.LockedPack, error) {
	manifestBytes, manifest, err := client.FetchManifest(ctx, packID)
	if err != nil {
		return config.LockedPack{}, err
	}
	tarballBytes, err := client.DownloadPack(ctx, packID)
	if err != nil {
		return config.LockedPack{}, err
	}
	tarballSHA := utils.SHA256Bytes(tarballBytes)
	manifestSHA := utils.SHA256Bytes(manifestBytes)
	cacheRoot, err := config.RulePackCacheRoot(root, cfg.CacheDir)
	if err != nil {
		return config.LockedPack{}, err
	}
	packRoot := filepath.Join(cacheRoot, packID, tarballSHA)
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
	if err := utils.ExtractTarGz(tarballBytes, extractPath); err != nil {
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
		ManifestPath:   utils.RelToRoot(root, manifestPath),
		TarballPath:    utils.RelToRoot(root, tarballPath),
		RulePath:       utils.RelToRoot(root, filepath.Join(extractPath, "rules")),
		TotalRules:     manifest.TotalRules,
		Languages:      manifest.Languages,
		DownloadedAt:   time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func PrintSelection(writer io.Writer, selection Selection, note string) {
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
			fmt.Fprintf(writer, "  %s reason=configured in config files\n", packID)
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

func PrintCachedPacks(writer io.Writer, lock config.Lock, verbose bool) {
	if len(lock.Packs) == 0 {
		fmt.Fprintln(writer, "using cached rule packs: none")
		return
	}
	if !verbose {
		packIDs := make([]string, 0, len(lock.Packs))
		for _, pack := range lock.Packs {
			packIDs = append(packIDs, pack.ID)
		}
		fmt.Fprintln(writer, "using cached rule packs:", strings.Join(packIDs, ", "))
		return
	}
	fmt.Fprintln(writer, "using cached rule packs:")
	for _, pack := range lock.Packs {
		fmt.Fprintf(writer, "  %s version=%s rules=%d rulePath=%s manifest=%s sha256=%s\n",
			pack.ID,
			pack.Version,
			pack.TotalRules,
			pack.RulePath,
			pack.ManifestPath,
			pack.SHA256,
		)
	}
}
