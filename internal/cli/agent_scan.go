package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/greprules/greprules/internal/doctor"
	"github.com/greprules/greprules/internal/output"
)

type agentScanOutcome struct {
	Status      string   `json:"status"`
	Message     string   `json:"message,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Targets     []string `json:"targets,omitempty"`
	TargetsPath string   `json:"targetsPath,omitempty"`
}

type agentScanMessageOptions struct {
	DoctorFailedPrefix    string
	FetchFailedPrefix     string
	ReadinessFailedPrefix string
	ScanFailedPrefix      string
	ReadyCommandFallback  string
}

type agentDirectScanOptions struct {
	Root            string
	Label           string
	Automatic       bool
	ScanArgs        []string
	OutputDir       string
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
	Messages        agentScanMessageOptions
}

func runAgentScan(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-scan scan")
	}
	switch args[0] {
	case "scan":
		return runAgentScanDirect(ctx, args[1:])
	default:
		return fmt.Errorf("unknown agent-scan command: %s", args[0])
	}
}

func runAgentScanDirect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-scan scan", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	label := fs.String("label", "scan", "scan label for compact summary")
	automatic := fs.Bool("automatic", false, "use automatic scan wording")
	changed := fs.Bool("changed", false, "scan changed files")
	full := fs.Bool("full", false, "scan full repo")
	sarif := fs.Bool("sarif", true, "write SARIF output")
	format := fs.String("format", "text", "output format: text or json")
	engineMode := fs.String("engine", "", "OpenGrep engine mode override: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path override")
	opengrepVersion := fs.String("opengrep-version", "", "managed OpenGrep version override")
	targetsFrom := fs.String("targets-from", "", "newline-delimited file of scan targets relative to root")
	outputDir := fs.String("output-dir", "", "scan output directory override")
	var targets stringList
	fs.Var(&targets, "target", "explicit scan target path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := resolveCommandRoot(*rootFlag, *changed)
	if err != nil {
		return err
	}
	scanArgs := []string{"--root", root}
	if *full {
		scanArgs = append(scanArgs, "--full")
	} else if *changed {
		scanArgs = append(scanArgs, "--changed")
	}
	for _, target := range targets {
		scanArgs = append(scanArgs, "--target", target)
	}
	if *targetsFrom != "" {
		scanArgs = append(scanArgs, "--targets-from", *targetsFrom)
	}
	if *outputDir != "" {
		scanArgs = append(scanArgs, "--output-dir", *outputDir)
	}
	if !*sarif {
		scanArgs = append(scanArgs, "--sarif=false")
	}
	outcome, err := runAgentDirectScan(ctx, agentDirectScanOptions{
		Root:            root,
		Label:           *label,
		Automatic:       *automatic,
		ScanArgs:        scanArgs,
		OutputDir:       *outputDir,
		EngineMode:      *engineMode,
		OpenGrepPath:    *opengrepPath,
		OpenGrepVersion: *opengrepVersion,
		Messages:        defaultAgentScanMessages(*automatic, *label),
	})
	if err != nil {
		return err
	}
	return printAgentScanOutcome(outcome, *format)
}

func printAgentScanOutcome(outcome agentScanOutcome, format string) error {
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

func runAgentDirectScan(ctx context.Context, options agentDirectScanOptions) (agentScanOutcome, error) {
	if options.Label == "" {
		options.Label = "scan"
	}
	if options.Messages.DoctorFailedPrefix == "" {
		options.Messages = defaultAgentScanMessages(options.Automatic, options.Label)
	}
	return runAgentScanAfterReadiness(ctx, options)
}

func runAgentScanAfterReadiness(ctx context.Context, options agentDirectScanOptions) (agentScanOutcome, error) {
	report, err := doctor.Build(ctx, options.Root, doctor.Options{
		EngineMode:      options.EngineMode,
		OpenGrepPath:    options.OpenGrepPath,
		OpenGrepVersion: options.OpenGrepVersion,
	})
	if err != nil {
		return agentScanOutcome{Status: "skipped", Message: options.Messages.DoctorFailedPrefix + err.Error()}, nil
	}
	if !report.Lock.Exists && report.Registry.OK {
		var fetchOutput bytes.Buffer
		fetchArgs := fetchArgsForAgentScan(options.Root, options.ScanArgs)
		if err := runFetchWithOptions(ctx, fetchArgs, fetchCommandOptions{quiet: true, stdout: &fetchOutput}); err != nil {
			if errors.Is(err, errNoPacksSelected) {
				return agentScanOutcome{
					Status:  "needs_pack_selection",
					Message: agentPackSelectionMessage(options.Root, options.ScanArgs),
				}, nil
			}
			message := strings.TrimSpace(fetchOutput.String() + "\n" + err.Error())
			return agentScanOutcome{Status: "skipped", Message: options.Messages.FetchFailedPrefix + message}, nil
		}
		report, err = doctor.Build(ctx, options.Root, doctor.Options{
			EngineMode:      options.EngineMode,
			OpenGrepPath:    options.OpenGrepPath,
			OpenGrepVersion: options.OpenGrepVersion,
		})
		if err != nil {
			return agentScanOutcome{Status: "skipped", Message: "greprules fetched rule packs, but readiness check failed: " + err.Error()}, nil
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
		return agentScanOutcome{Status: "skipped", Message: options.Messages.ReadinessFailedPrefix + recommended}, nil
	}

	scanArgs := append([]string{}, options.ScanArgs...)
	if options.EngineMode != "" {
		scanArgs = append(scanArgs, "--engine", options.EngineMode)
	}
	if options.OpenGrepPath != "" {
		scanArgs = append(scanArgs, "--opengrep-path", options.OpenGrepPath)
	}
	if options.OpenGrepVersion != "" {
		scanArgs = append(scanArgs, "--opengrep-version", options.OpenGrepVersion)
	}
	var scanOutput bytes.Buffer
	if err := runScanWithOptions(ctx, scanArgs, scanCommandOptions{
		quiet:  true,
		stdout: &scanOutput,
		stderr: &scanOutput,
	}); err != nil {
		message := strings.TrimSpace(scanOutput.String() + "\nerror: " + err.Error())
		return agentScanOutcome{Status: "failed", Message: options.Messages.ScanFailedPrefix + message}, nil
	}
	summary := output.SummarizeAgentResult(
		agentResultPath(options.Root, options.OutputDir),
		output.SummaryOptions{Automatic: options.Automatic, Label: options.Label},
	)
	return agentScanOutcome{Status: "scanned", Message: summary, Summary: summary}, nil
}

func agentResultPath(root string, outputDir string) string {
	if outputDir == "" {
		outputDir = filepath.Join(".greprules", "out")
	}
	if !filepath.IsAbs(outputDir) {
		outputDir = filepath.Join(root, outputDir)
	}
	return filepath.Join(outputDir, "agent-result.json")
}

func fetchArgsForAgentScan(root string, scanArgs []string) []string {
	args := []string{"--root", root}
	for index := 0; index < len(scanArgs); index++ {
		arg := scanArgs[index]
		switch {
		case arg == "--target" && index+1 < len(scanArgs):
			args = append(args, "--target", scanArgs[index+1])
			index++
		case strings.HasPrefix(arg, "--target="):
			args = append(args, arg)
		case arg == "--targets-from" && index+1 < len(scanArgs):
			args = append(args, "--targets-from", scanArgs[index+1])
			index++
		case strings.HasPrefix(arg, "--targets-from="):
			args = append(args, arg)
		case changedFlagEnabled(arg):
			args = append(args, arg)
		}
	}
	return args
}

func agentPackSelectionMessage(root string, scanArgs []string) string {
	recommendArgs := []string{"greprules", "recommend", "--root", root, "--format", "json", "--agent"}
	for index := 0; index < len(scanArgs); index++ {
		arg := scanArgs[index]
		switch {
		case arg == "--target" && index+1 < len(scanArgs):
			recommendArgs = append(recommendArgs, "--target", scanArgs[index+1])
			index++
		case strings.HasPrefix(arg, "--target="):
			recommendArgs = append(recommendArgs, arg)
		case arg == "--targets-from" && index+1 < len(scanArgs):
			recommendArgs = append(recommendArgs, "--targets-from", scanArgs[index+1])
			index++
		case strings.HasPrefix(arg, "--targets-from="):
			recommendArgs = append(recommendArgs, arg)
		case changedFlagEnabled(arg):
			recommendArgs = append(recommendArgs, arg)
		}
	}
	fetchArgs := []string{"greprules", "fetch", "--root", root, "--pack", "<slug>"}
	return "greprules needs agent-assisted rule-pack selection before scanning. Run `" + strings.Join(recommendArgs, " ") + "`, inspect detection, targets, availablePacks, and candidates, choose explicit pack slugs that match the scan target, run `" + strings.Join(fetchArgs, " ") + "` for each chosen pack, then rerun the greprules scan. Do not invent pack slugs."
}

func changedFlagEnabled(arg string) bool {
	if arg == "--changed" {
		return true
	}
	value, ok := strings.CutPrefix(arg, "--changed=")
	if !ok {
		return false
	}
	value = strings.ToLower(strings.TrimSpace(value))
	return value == "1" || value == "true" || value == "t" || value == "yes" || value == "y"
}

func defaultAgentScanMessages(automatic bool, label string) agentScanMessageOptions {
	scanName := strings.TrimSpace(label)
	if scanName == "" {
		scanName = "scan"
	}
	prefix := "greprules " + automaticPrefix(automatic) + scanName + " scan"
	return agentScanMessageOptions{
		DoctorFailedPrefix:    prefix + " skipped because doctor failed: ",
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
