package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/doctor"
	"github.com/greprules/greprules/internal/rules"
)

type ScanOutcome struct {
	Status      string   `json:"status"`
	Message     string   `json:"message,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	TargetsPath string   `json:"targetsPath,omitempty"`
}

type scanMessageOptions struct {
	DoctorFailedPrefix    string
	FetchFailedPrefix     string
	ReadinessFailedPrefix string
	ScanFailedPrefix      string
	ReadyCommandFallback  string
}

type directScanOptions struct {
	Request   scanRequest
	Label     string
	Automatic bool
	Messages  scanMessageOptions
}

type scanCommand struct {
	Request   scanRequest
	Label     string
	Automatic bool
	Format    string
}

type scanRequest struct {
	RulePacks       rules.Request
	OutputDir       string
	SARIF           bool
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
}

type scanIO struct {
	Quiet  bool
	Stdout io.Writer
	Stderr io.Writer
}

func RunScanCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-scan scan|recommend")
	}
	switch args[0] {
	case "scan":
		return runScanDirect(ctx, args[1:])
	case "recommend":
		return runRecommendDirect(ctx, args[1:])
	default:
		return fmt.Errorf("unknown agent-scan command: %s", args[0])
	}
}

func runRecommendDirect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-scan recommend", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	changed := fs.Bool("changed", false, "recommend for git changed, staged, and untracked files")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	format := fs.String("format", "json", "output format: json")
	targets, err := cmdutil.ParseInterspersed(fs, args)
	if err != nil {
		return err
	}
	root, err := cmdutil.ResolveCommandRoot(*rootFlag, *changed)
	if err != nil {
		return err
	}
	rawTargets, err := rules.CollectTargetInputs(root, targets, *targetsFrom, *changed)
	if err != nil {
		return err
	}
	root, rawTargets, err = cmdutil.MaybePromoteSingleExternalTargetToRoot(root, rawTargets, *targetsFrom, cmdutil.HasFlag(args, "root"), *changed)
	if err != nil {
		return err
	}
	resolution, _ := config.LoadEffectiveConfig(root)
	cfg := resolution.Config
	result, err := rules.DetectForTargets(root, rawTargets)
	if err != nil {
		return err
	}
	packs, err := rules.NewRegistry(cfg.Registry).ListPacks(ctx)
	if err != nil {
		return fmt.Errorf("list registry packs: %w", err)
	}
	candidates := rules.ForDetection(result, packs)
	switch *format {
	case "json", "":
		return cmdutil.PrintJSON(rules.BuildAgentContext(result, packs, candidates))
	default:
		return fmt.Errorf("unknown output format: %s", *format)
	}
}

func runScanDirect(ctx context.Context, args []string) error {
	command, err := parseScanCommand(args)
	if err != nil {
		return err
	}
	outcome, err := runDirectScan(ctx, directScanOptions{
		Request:   command.Request,
		Label:     command.Label,
		Automatic: command.Automatic,
		Messages:  defaultScanMessages(command.Automatic, command.Label),
	})
	if err != nil {
		return err
	}
	return printScanOutcome(outcome, command.Format)
}

func parseScanCommand(args []string) (scanCommand, error) {
	fs := flag.NewFlagSet("agent-scan scan", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	label := fs.String("label", "scan", "scan label for compact summary")
	automatic := fs.Bool("automatic", false, "use automatic scan wording")
	changed := fs.Bool("changed", false, "scan changed files")
	noSARIF := fs.Bool("no-sarif", false, "skip SARIF output")
	format := fs.String("format", "text", "output format: text or json")
	engineMode := fs.String("engine", "", "OpenGrep engine mode override: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path override")
	opengrepVersion := fs.String("opengrep-version", "", "managed OpenGrep version override")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	outputDir := fs.String("output-dir", "", "scan output directory override")
	targets, err := cmdutil.ParseInterspersed(fs, args)
	if err != nil {
		return scanCommand{}, err
	}
	root, err := cmdutil.ResolveCommandRoot(*rootFlag, *changed)
	if err != nil {
		return scanCommand{}, err
	}
	root, targets, err = cmdutil.MaybePromoteSingleExternalTargetToRoot(root, targets, *targetsFrom, cmdutil.HasFlag(args, "root"), *changed)
	if err != nil {
		return scanCommand{}, err
	}
	return scanCommand{
		Request: scanRequest{
			RulePacks: rules.Request{
				Root:        root,
				Changed:     *changed,
				Targets:     targets,
				TargetsFrom: *targetsFrom,
			},
			OutputDir:       *outputDir,
			SARIF:           !*noSARIF,
			EngineMode:      *engineMode,
			OpenGrepPath:    *opengrepPath,
			OpenGrepVersion: *opengrepVersion,
		},
		Label:     *label,
		Automatic: *automatic,
		Format:    *format,
	}, nil
}

func printScanOutcome(outcome ScanOutcome, format string) error {
	switch format {
	case "json":
		data, err := json.MarshalIndent(outcome, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "text", "":
		if outcome.Message != "" {
			fmt.Println(outcome.Message)
		}
	default:
		return fmt.Errorf("unknown output format: %s", format)
	}
	return nil
}

func runDirectScan(ctx context.Context, options directScanOptions) (ScanOutcome, error) {
	if options.Label == "" {
		options.Label = "scan"
	}
	if options.Messages.DoctorFailedPrefix == "" {
		options.Messages = defaultScanMessages(options.Automatic, options.Label)
	}
	return runScanAfterReadiness(ctx, options)
}

func runScanAfterReadiness(ctx context.Context, options directScanOptions) (ScanOutcome, error) {
	request := options.Request
	packs := request.RulePacks
	if err := cmdutil.EnsureGreprulesGitignore(packs.Root); err != nil {
		return ScanOutcome{Status: "skipped", Message: options.Messages.DoctorFailedPrefix + err.Error()}, nil
	}
	report, err := doctor.Build(ctx, packs.Root, doctor.Options{
		EngineMode:      request.EngineMode,
		OpenGrepPath:    request.OpenGrepPath,
		OpenGrepVersion: request.OpenGrepVersion,
	})
	if err != nil {
		return ScanOutcome{Status: "skipped", Message: options.Messages.DoctorFailedPrefix + err.Error()}, nil
	}
	if !report.Lock.Exists && report.Registry.OK {
		var fetchOutput bytes.Buffer
		prepareResult, err := rules.Ensure(ctx, packs, rules.EnsurePolicy{AutoFetch: true}, rules.EnsureIO{Quiet: true, Stdout: &fetchOutput})
		if err != nil {
			if errors.Is(err, rules.ErrNoPacksSelected) {
				return ScanOutcome{
					Status:  "needs_pack_selection",
					Message: packSelectionMessage(request),
				}, nil
			}
			message := strings.TrimSpace(fetchOutput.String() + "\n" + err.Error())
			return ScanOutcome{Status: "skipped", Message: options.Messages.FetchFailedPrefix + message}, nil
		}
		if !prepareResult.LockReady {
			return ScanOutcome{
				Status:  "needs_pack_selection",
				Message: packSelectionMessage(request),
			}, nil
		}
		report, err = doctor.Build(ctx, packs.Root, doctor.Options{
			EngineMode:      request.EngineMode,
			OpenGrepPath:    request.OpenGrepPath,
			OpenGrepVersion: request.OpenGrepVersion,
		})
		if err != nil {
			return ScanOutcome{Status: "skipped", Message: "greprules fetched rule packs, but readiness check failed: " + err.Error()}, nil
		}
	}
	if !report.OpenGrep.Active.OK {
		recommended := strings.Join(report.RecommendedCommands, ", ")
		if recommended == "" {
			recommended = options.Messages.ReadyCommandFallback
		}
		if recommended == "" {
			recommended = "greprules setup-opengrep"
		}
		return ScanOutcome{Status: "skipped", Message: options.Messages.ReadinessFailedPrefix + recommended}, nil
	}

	var scanOutput bytes.Buffer
	if err := runStructuredScan(ctx, request, scanIO{Quiet: true, Stdout: &scanOutput, Stderr: &scanOutput}); err != nil {
		message := strings.TrimSpace(scanOutput.String() + "\nerror: " + err.Error())
		return ScanOutcome{Status: "failed", Message: options.Messages.ScanFailedPrefix + message}, nil
	}
	summary := SummarizeAgentResult(
		resultPath(packs.Root, request.OutputDir),
		SummaryOptions{Automatic: options.Automatic, Label: options.Label},
	)
	return ScanOutcome{Status: "scanned", Message: summary, Summary: summary}, nil
}

func resultPath(root string, outputDir string) string {
	if outputDir == "" {
		outputDir = filepath.Join(".greprules", "out")
	}
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(root, outputDir)
	}
	return filepath.Join(outputDir, "agent-result.json")
}

func packSelectionMessage(request scanRequest) string {
	packs := request.RulePacks
	recommendArgs := []string{"greprules", "agent-scan", "recommend", "--root", packs.Root, "--format", "json"}
	if packs.Changed {
		recommendArgs = append(recommendArgs, "--changed")
	}
	if packs.TargetsFrom != "" {
		recommendArgs = append(recommendArgs, "--targets-from", packs.TargetsFrom)
	}
	recommendArgs = append(recommendArgs, packs.Targets...)
	fetchArgs := []string{"greprules", "fetch", "--root", packs.Root, "<slug>"}
	return "greprules needs agent-assisted rule-pack selection before scanning. Run `" + strings.Join(recommendArgs, " ") + "`, inspect detection, targets, availablePacks, and candidates, choose explicit pack slugs that match the scan target, run `" + strings.Join(fetchArgs, " ") + "` for each chosen pack, then rerun the greprules scan. Do not invent pack slugs."
}

func runStructuredScan(ctx context.Context, request scanRequest, ioOptions scanIO) error {
	packs := request.RulePacks
	return runScan(ctx, runScanOptions{
		Root:            packs.Root,
		Changed:         packs.Changed,
		SARIF:           request.SARIF,
		EngineMode:      request.EngineMode,
		OpenGrepPath:    request.OpenGrepPath,
		OpenGrepVersion: request.OpenGrepVersion,
		Targets:         packs.Targets,
		TargetsFrom:     packs.TargetsFrom,
		OutputDir:       request.OutputDir,
		Quiet:           ioOptions.Quiet,
		Stdout:          ioOptions.Stdout,
		Stderr:          ioOptions.Stderr,
	})
}

func defaultScanMessages(automatic bool, label string) scanMessageOptions {
	scanName := strings.TrimSpace(label)
	if scanName == "" {
		scanName = "scan"
	}
	prefix := "greprules " + automaticPrefix(automatic) + scanName + " scan"
	return scanMessageOptions{
		DoctorFailedPrefix:    prefix + " skipped because status check failed: ",
		FetchFailedPrefix:     prefix + " skipped because rule pack fetch failed: ",
		ReadinessFailedPrefix: prefix + " skipped because OpenGrep is not ready. Recommended commands: ",
		ScanFailedPrefix:      prefix + " failed: ",
		ReadyCommandFallback:  "greprules setup-opengrep",
	}
}

func automaticPrefix(automatic bool) string {
	if automatic {
		return "automatic "
	}
	return ""
}
