package standalone

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/rules"
	"github.com/greprules/greprules/internal/utils"
)

type ScanOptions struct {
	Quiet           bool
	Stdout          io.Writer
	Stderr          io.Writer
	AutoPrepare     bool
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
}

type ScanRequest struct {
	RulePacks       rules.Request
	OpenGrepArgs    []string
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
}

type ScanPolicy struct {
	AutoFetch bool
	AutoSetup bool
	Verbose   bool
}

type scanIO struct {
	Quiet  bool
	Stdout io.Writer
	Stderr io.Writer
}

func RunScan(ctx context.Context, args []string) error {
	return RunScanWithOptions(ctx, args, ScanOptions{Stdout: os.Stdout, Stderr: os.Stderr, AutoPrepare: true})
}

func RunScanWithOptions(ctx context.Context, args []string, options ScanOptions) error {
	request, policy, err := ParseScanRequest(args, options)
	if err != nil {
		return err
	}
	ioOptions := scanIO{Quiet: options.Quiet, Stdout: options.Stdout, Stderr: options.Stderr}
	infoMode := openGrepInfoMode(request.OpenGrepArgs)
	if options.AutoPrepare && !infoMode {
		prepareIO := ioOptions
		prepareIO.Stdout = ioOptions.Stderr
		if prepareIO.Stdout == nil {
			prepareIO.Stdout = os.Stderr
		}
		prepareResult, err := rules.Ensure(ctx, request.RulePacks, rules.EnsurePolicy{
			AutoFetch: policy.AutoFetch,
			Verbose:   policy.Verbose,
		}, rules.EnsureIO{
			Quiet:  prepareIO.Quiet,
			Stdout: prepareIO.Stdout,
		})
		if err != nil {
			if errors.Is(err, rules.ErrNoPacksSelected) {
				if hasOpenGrepConfigArg(request.OpenGrepArgs) {
					return runOpenGrep(ctx, request, ioOptions)
				}
				return fmt.Errorf("%w; configure packs or fetch explicit pack slugs before scanning", rules.ErrNoPacksSelected)
			}
			return err
		}
		if policy.AutoSetup && prepareResult.LockReady {
			if err := ensureOpenGrepRuntime(ctx, request.RulePacks.Root, prepareResult.Config, prepareResult.Lock, request, prepareIO); err != nil {
				return err
			}
		}
	}
	return runOpenGrep(ctx, request, ioOptions)
}

func ParseScanRequest(args []string, options ScanOptions) (ScanRequest, ScanPolicy, error) {
	parsed, err := parseScanArgs(args)
	if err != nil {
		return ScanRequest{}, ScanPolicy{}, err
	}
	root, err := cmdutil.ResolveCommandRoot(parsed.Root, parsed.Changed)
	if err != nil {
		return ScanRequest{}, ScanPolicy{}, err
	}
	targets := openGrepTargetCandidates(parsed.OpenGrepArgs)
	root, targets, err = cmdutil.MaybePromoteSingleExternalTargetToRoot(root, targets, "", parsed.RootExplicit, parsed.Changed)
	if err != nil {
		return ScanRequest{}, ScanPolicy{}, err
	}
	request := ScanRequest{
		OpenGrepArgs: parsed.OpenGrepArgs,
		RulePacks: rules.Request{
			Root:    root,
			Changed: parsed.Changed,
			Targets: targets,
		},
		EngineMode:      options.EngineMode,
		OpenGrepPath:    options.OpenGrepPath,
		OpenGrepVersion: options.OpenGrepVersion,
	}
	policy := ScanPolicy{
		AutoFetch: !parsed.NoPrepare,
		AutoSetup: !parsed.NoPrepare,
		Verbose:   parsed.Verbose,
	}
	return request, policy, nil
}

type scanArgs struct {
	Root         string
	RootExplicit bool
	Changed      bool
	NoPrepare    bool
	Verbose      bool
	OpenGrepArgs []string
}

func parseScanArgs(args []string) (scanArgs, error) {
	parsed := scanArgs{Root: "."}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			parsed.OpenGrepArgs = append(parsed.OpenGrepArgs, args[index+1:]...)
			break
		}
		switch {
		case arg == "--root":
			if index+1 >= len(args) {
				return parsed, errors.New("flag needs an argument: --root")
			}
			parsed.Root = args[index+1]
			parsed.RootExplicit = true
			index++
		case strings.HasPrefix(arg, "--root="):
			parsed.Root = strings.TrimPrefix(arg, "--root=")
			parsed.RootExplicit = true
		case arg == "--changed":
			parsed.Changed = true
		case strings.HasPrefix(arg, "--changed="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--changed="))
			if err != nil {
				return parsed, fmt.Errorf("invalid --changed value: %w", err)
			}
			parsed.Changed = value
		case arg == "--no-prepare":
			parsed.NoPrepare = true
		case strings.HasPrefix(arg, "--no-prepare="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--no-prepare="))
			if err != nil {
				return parsed, fmt.Errorf("invalid --no-prepare value: %w", err)
			}
			parsed.NoPrepare = value
		case arg == "--verbose":
			parsed.Verbose = true
		case strings.HasPrefix(arg, "--verbose="):
			value, err := strconv.ParseBool(strings.TrimPrefix(arg, "--verbose="))
			if err != nil {
				return parsed, fmt.Errorf("invalid --verbose value: %w", err)
			}
			parsed.Verbose = value
		default:
			parsed.OpenGrepArgs = append(parsed.OpenGrepArgs, arg)
		}
	}
	return parsed, nil
}

func runOpenGrep(ctx context.Context, request ScanRequest, ioOptions scanIO) error {
	packs := request.RulePacks
	cfg, err := config.LoadEffectiveOrDefault(packs.Root)
	if err != nil {
		return err
	}
	infoMode := openGrepInfoMode(request.OpenGrepArgs)
	lock, lockErr := config.LoadLock(packs.Root)
	if lockErr != nil && !infoMode {
		if errors.Is(lockErr, os.ErrNotExist) && hasOpenGrepConfigArg(request.OpenGrepArgs) {
			lockErr = nil
		} else {
			return fmt.Errorf("lockfile missing; run greprules scan without --no-prepare to auto-select packs, or fetch explicit packs with greprules fetch <slug>: %w", lockErr)
		}
	}
	if lockErr != nil {
		lock = config.Lock{}
	}
	if !infoMode && lockErr == nil && len(lock.Packs) > 0 {
		if missing := config.MissingLockRulePaths(packs.Root, lock); len(missing) > 0 {
			return fmt.Errorf("locked rule pack artifacts are missing: %s; run greprules scan without --no-prepare to refresh selected packs, or fetch explicit packs with greprules fetch <slug>", strings.Join(missing, ", "))
		}
	}
	runtimeInfo, err := resolveOpenGrepRuntime(lock, cfg, request)
	if err != nil {
		if !infoMode || effectiveOpenGrepMode(cfg, request) != "managed" {
			return fmt.Errorf("OpenGrep runtime is not available: %w", err)
		}
		runtimeInfo, err = opengrep.Setup(ctx, opengrep.SetupOptions{Version: effectiveOpenGrepVersion(cfg, request)})
		if err != nil {
			return fmt.Errorf("OpenGrep runtime is not available and automatic setup failed; run greprules setup-opengrep: %w", err)
		}
	}
	if !infoMode && lockErr == nil && len(lock.Packs) > 0 {
		lock.Engine = opengrep.LockedEngineFromRuntime(runtimeInfo)
		if err := config.SaveLock(packs.Root, lock); err != nil {
			return err
		}
	}
	args, err := openGrepArgsForStandaloneScan(request)
	if err != nil {
		return err
	}
	configs := []string{}
	if !infoMode && len(lock.Packs) > 0 {
		configs = config.OpenGrepConfigPaths(packs.Root, lock, cfg.OpenGrep.IncludeDefaultRules)
	}
	return opengrep.RunRawScan(ctx, runtimeInfo, opengrep.RawScanOptions{
		WorkingDir: packs.Root,
		Configs:    configs,
		Args:       args,
		Stdout:     ioOptions.Stdout,
		Stderr:     ioOptions.Stderr,
	})
}

func openGrepArgsForStandaloneScan(request ScanRequest) ([]string, error) {
	args := append([]string{}, request.OpenGrepArgs...)
	if openGrepInfoMode(args) {
		return args, nil
	}
	if request.RulePacks.Changed {
		changedFiles, err := utils.ChangedFiles(request.RulePacks.Root)
		if err != nil {
			return nil, err
		}
		if len(changedFiles) == 0 {
			return nil, errNoChangedFiles
		}
		args = append(args, changedFiles...)
	}
	if len(openGrepTargetCandidates(args)) == 0 {
		args = append(args, ".")
	}
	return args, nil
}

var errNoChangedFiles = errors.New("no changed files to scan")

func ensureOpenGrepRuntime(ctx context.Context, root string, cfg config.Config, lock config.Lock, request ScanRequest, ioOptions scanIO) error {
	if _, err := resolveOpenGrepRuntime(lock, cfg, request); err == nil {
		return nil
	}
	mode := effectiveOpenGrepMode(cfg, request)
	if mode != "managed" {
		return nil
	}
	runtimeInfo, err := opengrep.Setup(ctx, opengrep.SetupOptions{Version: effectiveOpenGrepVersion(cfg, request)})
	if err != nil {
		return fmt.Errorf("OpenGrep runtime is not available and automatic setup failed; run greprules setup-opengrep: %w", err)
	}
	if !ioOptions.Quiet {
		writer := ioOptions.Stdout
		if writer == nil {
			writer = os.Stdout
		}
		fmt.Fprintf(writer, "installed OpenGrep %s at %s\n", runtimeInfo.Version, runtimeInfo.Path)
	}
	lock.Engine = opengrep.LockedEngineFromRuntime(runtimeInfo)
	return config.SaveLock(root, lock)
}

func resolveOpenGrepRuntime(lock config.Lock, cfg config.Config, request ScanRequest) (opengrep.Runtime, error) {
	return opengrep.ResolveFromConfig(lock, cfg, opengrep.ConfigOverrides{
		Mode:    request.EngineMode,
		Path:    request.OpenGrepPath,
		Version: request.OpenGrepVersion,
	})
}

func effectiveOpenGrepMode(cfg config.Config, request ScanRequest) string {
	if request.EngineMode != "" {
		return request.EngineMode
	}
	if request.OpenGrepPath != "" {
		return "path"
	}
	if cfg.OpenGrep.Mode != "" {
		return cfg.OpenGrep.Mode
	}
	return "managed"
}

func effectiveOpenGrepVersion(cfg config.Config, request ScanRequest) string {
	if request.OpenGrepVersion != "" {
		return request.OpenGrepVersion
	}
	return cfg.OpenGrep.Version
}

func openGrepInfoMode(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--help", "-h", "--version", "-V":
			return true
		}
	}
	return false
}

func hasOpenGrepConfigArg(args []string) bool {
	for _, arg := range args {
		if arg == "--config" || arg == "-c" || strings.HasPrefix(arg, "--config=") || strings.HasPrefix(arg, "-c=") {
			return true
		}
	}
	return false
}

func openGrepTargetCandidates(args []string) []string {
	targets := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			targets = append(targets, args[index+1:]...)
			break
		}
		if arg == "" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			if openGrepLongFlagConsumesValue(arg) && !strings.Contains(arg, "=") {
				index++
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if openGrepShortFlagConsumesValue(arg) && len(arg) == 2 {
				index++
			}
			continue
		}
		targets = append(targets, arg)
	}
	return targets
}

func openGrepLongFlagConsumesValue(arg string) bool {
	name := arg
	if before, _, ok := strings.Cut(arg, "="); ok {
		name = before
	}
	switch name {
	case "--baseline-commit",
		"--baseline-commit-status",
		"--config",
		"--dump-command-for-core",
		"--exclude",
		"--include",
		"--interfile-timeout",
		"--jobs",
		"--lang",
		"--max-chars-per-line",
		"--max-lines-per-finding",
		"--max-memory",
		"--max-target-bytes",
		"--optimizations",
		"--output",
		"--pattern",
		"--project-root",
		"--severity",
		"--timeout",
		"--timeout-threshold":
		return true
	default:
		return false
	}
}

func openGrepShortFlagConsumesValue(arg string) bool {
	switch arg {
	case "-c", "-e", "-j", "-l", "-o":
		return true
	default:
		return false
	}
}
