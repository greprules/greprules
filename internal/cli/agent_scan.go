package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/greprules/greprules/internal/agentstate"
	"github.com/greprules/greprules/internal/detect"
	"github.com/greprules/greprules/internal/doctor"
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
	TooManyMessage        func(count int, limit int) string
}

type agentEditedScanOptions struct {
	Root                string
	State               agentstate.State
	Label               string
	Automatic           bool
	MaxTargets          int
	Sarif               bool
	EngineMode          string
	OpenGrepPath        string
	OpenGrepVersion     string
	LastSummaryPath     string
	ClearOnHandledError bool
	Messages            agentScanMessageOptions
	Logf                func(format string, args ...any)
}

type agentDirectScanOptions struct {
	Root            string
	Label           string
	Automatic       bool
	ScanArgs        []string
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
	Messages        agentScanMessageOptions
}

func runAgentScan(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-scan edited|scan")
	}
	switch args[0] {
	case "edited":
		return runAgentScanEdited(ctx, args[1:])
	case "scan":
		return runAgentScanDirect(ctx, args[1:])
	default:
		return fmt.Errorf("unknown agent-scan command: %s", args[0])
	}
}

func runAgentScanEdited(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-scan edited", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	stateDir := fs.String("state-dir", "", "agent state directory")
	label := fs.String("label", "edited-file", "scan label for compact summary")
	automatic := fs.Bool("automatic", false, "use automatic scan wording")
	maxTargets := fs.Int("max-targets", 0, "maximum edited targets before skipping; 0 disables the limit")
	tooManySuggestion := fs.String("too-many-suggestion", "", "guidance appended when edited targets exceed the limit")
	tooManyMessage := fs.String("too-many-message", "", "full message template for too many edited targets; supports {count} and {limit}")
	sarif := fs.Bool("sarif", true, "write SARIF output")
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
	state, err := agentstate.New(root, *stateDir)
	if err != nil {
		return err
	}
	messages := defaultAgentScanMessages(*automatic, *label)
	messages.TooManyMessage = func(count int, limit int) string {
		if strings.TrimSpace(*tooManyMessage) != "" {
			return strings.NewReplacer(
				"{count}", fmt.Sprint(count),
				"{limit}", fmt.Sprint(limit),
			).Replace(*tooManyMessage)
		}
		message := fmt.Sprintf("greprules %sedited-file scan skipped because %d edited files exceed the automatic limit (%d).", automaticPrefix(*automatic), count, limit)
		if strings.TrimSpace(*tooManySuggestion) != "" {
			message += " " + strings.TrimSpace(*tooManySuggestion)
		}
		return message
	}
	outcome, err := runAgentEditedScan(ctx, agentEditedScanOptions{
		Root:            root,
		State:           state,
		Label:           *label,
		Automatic:       *automatic,
		MaxTargets:      *maxTargets,
		Sarif:           *sarif,
		EngineMode:      *engineMode,
		OpenGrepPath:    *opengrepPath,
		OpenGrepVersion: *opengrepVersion,
		Messages:        messages,
	})
	if err != nil {
		return err
	}
	return printAgentScanOutcome(outcome, *format)
}

func runAgentScanDirect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-scan scan", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	label := fs.String("label", "scan", "scan label for compact summary")
	changed := fs.Bool("changed", false, "scan changed files")
	full := fs.Bool("full", false, "scan full repo")
	sarif := fs.Bool("sarif", true, "write SARIF output")
	format := fs.String("format", "text", "output format: text or json")
	engineMode := fs.String("engine", "", "OpenGrep engine mode override: managed, system, or path")
	opengrepPath := fs.String("opengrep-path", "", "OpenGrep binary path override")
	opengrepVersion := fs.String("opengrep-version", "", "managed OpenGrep version override")
	var targets stringList
	fs.Var(&targets, "target", "explicit scan target path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
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
	if !*sarif {
		scanArgs = append(scanArgs, "--sarif=false")
	}
	outcome, err := runAgentDirectScan(ctx, agentDirectScanOptions{
		Root:            root,
		Label:           *label,
		ScanArgs:        scanArgs,
		EngineMode:      *engineMode,
		OpenGrepPath:    *opengrepPath,
		OpenGrepVersion: *opengrepVersion,
		Messages:        defaultAgentScanMessages(false, *label),
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

func runAgentEditedScan(ctx context.Context, options agentEditedScanOptions) (agentScanOutcome, error) {
	if options.Label == "" {
		options.Label = "edited-file"
	}
	if options.Messages.DoctorFailedPrefix == "" {
		options.Messages = defaultAgentScanMessages(options.Automatic, options.Label)
	}
	if _, err := os.Stat(options.State.DirtyMarkerPath()); err != nil {
		if os.IsNotExist(err) {
			logAgentScan(options.Logf, "scan skipped; no dirty marker")
			return agentScanOutcome{Status: "skipped"}, nil
		}
		return agentScanOutcome{}, err
	}
	targets, err := options.State.PrepareScanTargets()
	if err != nil {
		return agentScanOutcome{}, err
	}
	if len(targets) == 0 {
		options.State.ClearDirtyState()
		logAgentScan(options.Logf, "scan skipped; no edited files captured")
		return agentScanOutcome{Status: "skipped"}, nil
	}
	if options.MaxTargets > 0 && len(targets) > options.MaxTargets {
		options.State.ClearDirtyState()
		options.State.RecordScanAttempt()
		message := defaultTooManyEditedTargetsMessage(options.Automatic, len(targets), options.MaxTargets, "")
		if options.Messages.TooManyMessage != nil {
			message = options.Messages.TooManyMessage(len(targets), options.MaxTargets)
		}
		return agentScanOutcome{
			Status:      "skipped",
			Message:     message,
			Targets:     targets,
			TargetsPath: options.State.ScanTargetsPath(),
		}, nil
	}
	scanArgs := []string{"--root", options.Root, "--targets-from", options.State.ScanTargetsPath()}
	if !options.Sarif {
		scanArgs = append(scanArgs, "--sarif=false")
	}
	outcome, err := runAgentScanAfterReadiness(ctx, agentDirectScanOptions{
		Root:            options.Root,
		Label:           options.Label,
		Automatic:       options.Automatic,
		ScanArgs:        scanArgs,
		EngineMode:      options.EngineMode,
		OpenGrepPath:    options.OpenGrepPath,
		OpenGrepVersion: options.OpenGrepVersion,
		Messages:        options.Messages,
	})
	outcome.Targets = targets
	outcome.TargetsPath = options.State.ScanTargetsPath()
	if err != nil {
		return outcome, err
	}
	if outcome.Status == "scanned" {
		if options.LastSummaryPath != "" {
			_ = os.WriteFile(options.LastSummaryPath, []byte(outcome.Message+"\n"), 0o644)
		}
		options.State.RecordScanAttempt()
		options.State.ClearDirtyState()
	} else if outcome.Message != "" && options.ClearOnHandledError {
		options.State.RecordScanAttempt()
		options.State.ClearDirtyState()
	}
	return outcome, nil
}

func runAgentDirectScan(ctx context.Context, options agentDirectScanOptions) (agentScanOutcome, error) {
	if options.Label == "" {
		options.Label = "scan"
	}
	if options.Messages.DoctorFailedPrefix == "" {
		options.Messages = defaultAgentScanMessages(false, options.Label)
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
		if err := runFetchWithOptions(ctx, []string{"--root", options.Root}, fetchCommandOptions{quiet: true, stdout: &fetchOutput}); err != nil {
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
	summary := agentstate.SummarizeAgentResult(
		filepath.Join(options.Root, ".greprules", "out", "agent-result.json"),
		agentstate.SummaryOptions{Automatic: options.Automatic, Label: options.Label},
	)
	return agentScanOutcome{Status: "scanned", Message: summary, Summary: summary}, nil
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
		TooManyMessage: func(count int, limit int) string {
			return defaultTooManyEditedTargetsMessage(automatic, count, limit, "")
		},
	}
}

func defaultTooManyEditedTargetsMessage(automatic bool, count int, limit int, suggestion string) string {
	message := fmt.Sprintf("greprules %sedited-file scan skipped because %d edited files exceed the automatic limit (%d).", automaticPrefix(automatic), count, limit)
	if strings.TrimSpace(suggestion) != "" {
		message += " " + strings.TrimSpace(suggestion)
	}
	return message
}

func automaticPrefix(automatic bool) string {
	if automatic {
		return "automatic "
	}
	return ""
}

func logAgentScan(logf func(format string, args ...any), format string, args ...any) {
	if logf != nil {
		logf(format, args...)
	}
}
