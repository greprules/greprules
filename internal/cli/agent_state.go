package cli

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	"github.com/greprules/greprules/internal/agentstate"
	"github.com/greprules/greprules/internal/detect"
)

func runAgentState(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-state mark-dirty|prepare-targets|clear|record-scan|summarize")
	}
	switch args[0] {
	case "mark-dirty":
		return runAgentStateMarkDirty(args[1:])
	case "prepare-targets":
		return runAgentStatePrepareTargets(args[1:])
	case "clear":
		return runAgentStateClear(args[1:])
	case "record-scan":
		return runAgentStateRecordScan(args[1:])
	case "summarize":
		return runAgentStateSummarize(args[1:])
	default:
		return fmt.Errorf("unknown agent-state command: %s", args[0])
	}
}

func agentStateFromFlags(rootFlag string, stateDir string) (string, agentstate.State, error) {
	root, err := detect.FindRepoRoot(rootFlag)
	if err != nil {
		return "", agentstate.State{}, err
	}
	state, err := agentstate.New(root, stateDir)
	if err != nil {
		return "", agentstate.State{}, err
	}
	return root, state, nil
}

func runAgentStateMarkDirty(args []string) error {
	fs := flag.NewFlagSet("agent-state mark-dirty", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	stateDir := fs.String("state-dir", "", "agent state directory")
	cwdFlag := fs.String("cwd", "", "base directory for relative paths")
	var paths stringList
	fs.Var(&paths, "path", "edited path, repeatable or comma-separated")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(paths) == 0 {
		return errors.New("at least one --path is required")
	}
	root, state, err := agentStateFromFlags(*rootFlag, *stateDir)
	if err != nil {
		return err
	}
	baseDir := *cwdFlag
	if baseDir == "" {
		baseDir = root
	} else if !filepath.IsAbs(baseDir) {
		baseDir = filepath.Join(root, baseDir)
	}
	files, err := state.MarkDirtyFrom(paths, baseDir)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"marked":         len(files) > 0,
		"files":          files,
		"stateDir":       state.StateDir,
		"dirtyFilesPath": state.DirtyFilesPath(),
	})
}

func runAgentStatePrepareTargets(args []string) error {
	fs := flag.NewFlagSet("agent-state prepare-targets", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	stateDir := fs.String("state-dir", "", "agent state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, state, err := agentStateFromFlags(*rootFlag, *stateDir)
	if err != nil {
		return err
	}
	targets, err := state.PrepareScanTargets()
	if err != nil {
		return err
	}
	return printJSON(map[string]any{
		"targets":     targets,
		"targetsPath": state.ScanTargetsPath(),
		"stateDir":    state.StateDir,
	})
}

func runAgentStateClear(args []string) error {
	fs := flag.NewFlagSet("agent-state clear", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	stateDir := fs.String("state-dir", "", "agent state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, state, err := agentStateFromFlags(*rootFlag, *stateDir)
	if err != nil {
		return err
	}
	state.ClearDirtyState()
	return printJSON(map[string]any{"cleared": true, "stateDir": state.StateDir})
}

func runAgentStateRecordScan(args []string) error {
	fs := flag.NewFlagSet("agent-state record-scan", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	stateDir := fs.String("state-dir", "", "agent state directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	_, state, err := agentStateFromFlags(*rootFlag, *stateDir)
	if err != nil {
		return err
	}
	state.RecordScanAttempt()
	return printJSON(map[string]any{"recorded": true, "stateDir": state.StateDir})
}

func runAgentStateSummarize(args []string) error {
	fs := flag.NewFlagSet("agent-state summarize", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	pathFlag := fs.String("path", "", "agent-result.json path")
	label := fs.String("label", "", "scan label for compact summary")
	automatic := fs.Bool("automatic", false, "use automatic scan wording")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := detect.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	path := *pathFlag
	if path == "" {
		path = filepath.Join(root, ".greprules", "out", "agent-result.json")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	fmt.Println(agentstate.SummarizeAgentResult(path, agentstate.SummaryOptions{Automatic: *automatic, Label: *label}))
	return nil
}
