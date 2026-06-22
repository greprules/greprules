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
	Help            bool
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
	if request.Help {
		printScanUsage(options.Stdout)
		return nil
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
	targets := append([]string{}, parsed.Targets...)
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
		Help:            parsed.Help,
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
	Help         bool
	Targets      []string
	OpenGrepArgs []string
}

func parseScanArgs(args []string) (scanArgs, error) {
	mixedArgs, rawOpenGrepArgs := splitScanArgs(args)
	parsed := scanArgs{Root: "."}
	for index := 0; index < len(mixedArgs); index++ {
		arg := mixedArgs[index]
		switch {
		case arg == "--root":
			if index+1 >= len(mixedArgs) {
				return parsed, errors.New("flag needs an argument: --root")
			}
			parsed.Root = mixedArgs[index+1]
			parsed.RootExplicit = true
			index++
		case strings.HasPrefix(arg, "--root="):
			parsed.Root = strings.TrimPrefix(arg, "--root=")
			parsed.RootExplicit = true
		case arg == "--changed" || arg == "--no-prepare" || arg == "--verbose":
			setScanBoolFlag(&parsed, strings.TrimPrefix(arg, "--"), true)
		case strings.HasPrefix(arg, "--changed="), strings.HasPrefix(arg, "--no-prepare="), strings.HasPrefix(arg, "--verbose="):
			name, raw, _ := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			value, err := strconv.ParseBool(raw)
			if err != nil {
				return parsed, fmt.Errorf("invalid --%s value: %w", name, err)
			}
			setScanBoolFlag(&parsed, name, value)
		case arg == "--help" || arg == "-h":
			parsed.Help = true
		case strings.HasPrefix(arg, "-"):
			consumed, openGrepArgs, err := parseOpenGrepFlag(mixedArgs, index)
			if err != nil {
				return parsed, err
			}
			parsed.OpenGrepArgs = append(parsed.OpenGrepArgs, openGrepArgs...)
			index += consumed
		default:
			parsed.Targets = append(parsed.Targets, arg)
			parsed.OpenGrepArgs = append(parsed.OpenGrepArgs, arg)
		}
	}
	parsed.OpenGrepArgs = append(parsed.OpenGrepArgs, rawOpenGrepArgs...)
	return parsed, nil
}

func parseOpenGrepFlag(args []string, index int) (int, []string, error) {
	arg := args[index]
	spec, known := openGrepFlagSpecFor(arg)
	if !known {
		return 0, nil, fmt.Errorf("unsupported OpenGrep flag before --: %s; use -- before advanced OpenGrep flags, for example: greprules scan <target> -- %s", arg, arg)
	}
	if !spec.Value {
		return 0, []string{arg}, nil
	}
	if strings.Contains(arg, "=") || compactShortValue(arg) {
		return 0, []string{arg}, nil
	}
	if index+1 >= len(args) {
		return 0, nil, fmt.Errorf("OpenGrep flag needs an argument: %s", arg)
	}
	return 1, []string{arg, args[index+1]}, nil
}

func compactShortValue(arg string) bool {
	if len(arg) <= 2 || !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
		return false
	}
	_, ok := supportedOpenGrepShortFlags[arg[:2]]
	return ok
}

func setScanBoolFlag(parsed *scanArgs, name string, value bool) {
	switch name {
	case "changed":
		parsed.Changed = value
	case "no-prepare":
		parsed.NoPrepare = value
	case "verbose":
		parsed.Verbose = value
	}
}

func splitScanArgs(args []string) ([]string, []string) {
	for index, arg := range args {
		if arg == "--" {
			return args[:index], append([]string{}, args[index+1:]...)
		}
	}
	return args, nil
}

func printScanUsage(writer io.Writer) {
	if writer == nil {
		writer = os.Stdout
	}
	fmt.Fprintln(writer, `Usage:
  greprules scan [PATH_OR_OPENGREP_ARGS...] [--root PATH] [--changed] [--verbose] [--no-prepare] [-- RAW_OPENGREP_ARGS...]

greprules prepares greprules.io rule packs, then runs opengrep scan.

greprules options:
  --root PATH     project root used for detection, state, and scan working directory
  --changed       scan git changed files from the resolved project root
  --verbose       print greprules rule-pack selection and lock details
  --no-prepare    skip rule-pack selection, fetching, and managed OpenGrep setup

Supported OpenGrep scan options can be mixed in normal OpenGrep style:
  greprules scan . --json --severity ERROR
  greprules scan --json-output result.json src

Other OpenGrep flags must be passed after -- so greprules does not mistake
their values for scan targets during rule-pack selection:
  greprules scan src -- --some-future-opengrep-flag value`)
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
		return args, nil
	}
	if len(args) == 0 {
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

func openGrepFlagSpecFor(arg string) (openGrepFlagSpec, bool) {
	if strings.HasPrefix(arg, "--") {
		name, _, _ := strings.Cut(arg, "=")
		spec, ok := supportedOpenGrepLongFlags[name]
		return spec, ok
	}
	if strings.HasPrefix(arg, "-") {
		name, _, _ := strings.Cut(arg, "=")
		if spec, ok := supportedOpenGrepShortFlags[name]; ok {
			return spec, true
		}
		if compactShortValue(arg) {
			return supportedOpenGrepShortFlags[arg[:2]], true
		}
	}
	return openGrepFlagSpec{}, false
}

type openGrepFlagSpec struct {
	Value bool
}

func valueFlag() openGrepFlagSpec {
	return openGrepFlagSpec{Value: true}
}

func boolFlag() openGrepFlagSpec {
	return openGrepFlagSpec{}
}

// Keep this list small: these are common local scan flags whose values must not
// be mistaken for scan targets during greprules rule-pack selection.
var supportedOpenGrepLongFlags = map[string]openGrepFlagSpec{
	"--baseline-commit":  valueFlag(),
	"--config":           valueFlag(),
	"--error":            boolFlag(),
	"--exclude":          valueFlag(),
	"--exclude-rule":     valueFlag(),
	"--force-exclude":    boolFlag(),
	"--include":          valueFlag(),
	"--jobs":             valueFlag(),
	"--json":             boolFlag(),
	"--json-output":      valueFlag(),
	"--lang":             valueFlag(),
	"--max-target-bytes": valueFlag(),
	"--no-git-ignore":    boolFlag(),
	"--output":           valueFlag(),
	"--pattern":          valueFlag(),
	"--quiet":            boolFlag(),
	"--sarif":            boolFlag(),
	"--sarif-output":     valueFlag(),
	"--severity":         valueFlag(),
	"--strict":           boolFlag(),
	"--text":             boolFlag(),
	"--text-output":      valueFlag(),
	"--timeout":          valueFlag(),
}

var supportedOpenGrepShortFlags = map[string]openGrepFlagSpec{
	"-c": valueFlag(),
	"-e": valueFlag(),
	"-f": valueFlag(),
	"-j": valueFlag(),
	"-l": valueFlag(),
	"-o": valueFlag(),
	"-q": boolFlag(),
}
