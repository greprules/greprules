package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/utils"
)

type runScanOptions struct {
	Root            string
	Changed         bool
	SARIF           bool
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
	Targets         []string
	TargetsFrom     string
	OutputDir       string
	Quiet           bool
	Stdout          io.Writer
	Stderr          io.Writer
}

func runScan(ctx context.Context, options runScanOptions) error {
	if err := ensureOutputRoot(options.Root); err != nil {
		return err
	}
	cfg, err := config.LoadEffectiveOrDefault(options.Root)
	if err != nil {
		return err
	}
	lock, err := config.LoadLock(options.Root)
	if err != nil {
		return fmt.Errorf("lockfile missing; run greprules agent-scan recommend, then fetch explicit packs with greprules fetch <slug>: %w", err)
	}
	if missing := config.MissingLockRulePaths(options.Root, lock); len(missing) > 0 {
		return fmt.Errorf("locked rule pack artifacts are missing: %s; run greprules agent-scan recommend, then fetch explicit packs with greprules fetch <slug>", strings.Join(missing, ", "))
	}
	runtimeInfo, err := opengrep.ResolveFromConfig(lock, cfg, opengrep.ConfigOverrides{
		Mode:    options.EngineMode,
		Path:    options.OpenGrepPath,
		Version: options.OpenGrepVersion,
	})
	if err != nil {
		return fmt.Errorf("OpenGrep runtime is not available: %w", err)
	}
	lock.Engine = opengrep.LockedEngineFromRuntime(runtimeInfo)
	if err := config.SaveLock(options.Root, lock); err != nil {
		return err
	}
	configuredOutputDir := cfg.OutputDir
	if options.OutputDir != "" {
		configuredOutputDir = options.OutputDir
	}
	outputDir := utils.AbsFromRoot(options.Root, configuredOutputDir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	targetSelection, err := resolveScanTargets(targetOptions{
		Root:        options.Root,
		Changed:     options.Changed,
		Targets:     options.Targets,
		TargetsFrom: options.TargetsFrom,
	})
	if err != nil {
		return err
	}
	jsonPath := filepath.Join(outputDir, "scan.json")
	sarifPath := filepath.Join(outputDir, "scan.sarif")
	agentPath := filepath.Join(outputDir, "agent-result.json")
	startedAt := time.Now().UTC().Format(time.RFC3339)
	scanConfigs := config.OpenGrepConfigPaths(options.Root, lock, cfg.OpenGrep.IncludeDefaultRules)
	result := baseAgentResult(options.Root, lock, runtimeInfo, targetSelection.ChangedMode, targetSelection.ChangedFiles, targetSelection.Targets, scanConfigs, jsonPath, sarifPath)
	result.Scan.StartedAt = startedAt
	if targetSelection.EmptyWarning != "" {
		result.Status = "ok"
		result.Warnings = append(result.Warnings, targetSelection.EmptyWarning)
		result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		return WriteAgentResult(agentPath, result)
	}
	if err := removeOutputFile(jsonPath); err != nil {
		return err
	}
	jsonRunErr := opengrep.RunScan(ctx, runtimeInfo, opengrep.ScanOptions{
		WorkingDir: options.Root,
		Configs:    scanConfigs,
		Targets:    targetSelection.Targets,
		OutputPath: jsonPath,
		Format:     "json",
		Stdout:     options.Stdout,
		Stderr:     options.Stderr,
	})
	findings, parseErr := FindingsFromOpenGrepJSON(jsonPath)
	if parseErr != nil {
		result.Warnings = append(result.Warnings, "could not parse OpenGrep JSON: "+parseErr.Error())
		if jsonRunErr != nil {
			result.Status = "failed"
			result.Errors = append(result.Errors, jsonRunErr.Error())
			result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			_ = WriteAgentResult(agentPath, result)
			return jsonRunErr
		}
	} else {
		result.Findings = findings
		if jsonRunErr != nil {
			result.Warnings = append(result.Warnings, openGrepRunWarning("JSON run", jsonRunErr))
		}
		if warnings, err := WarningsFromOpenGrepJSON(jsonPath); err != nil {
			result.Warnings = append(result.Warnings, "could not parse OpenGrep diagnostics: "+err.Error())
		} else {
			result.Warnings = append(result.Warnings, warnings...)
		}
	}
	if options.SARIF {
		if err := removeOutputFile(sarifPath); err != nil {
			return err
		}
		if err := opengrep.RunScan(ctx, runtimeInfo, opengrep.ScanOptions{
			WorkingDir: options.Root,
			Configs:    scanConfigs,
			Targets:    targetSelection.Targets,
			OutputPath: sarifPath,
			Format:     "sarif",
			Stdout:     options.Stdout,
			Stderr:     options.Stderr,
		}); err != nil {
			result.Warnings = append(result.Warnings, openGrepRunWarning("SARIF run", err))
		}
	} else {
		result.SARIFPath = ""
	}
	result.Status = "ok"
	result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	if err := WriteAgentResult(agentPath, result); err != nil {
		return err
	}
	if !options.Quiet {
		writer := options.Stdout
		if writer == nil {
			writer = os.Stdout
		}
		fmt.Fprintln(writer, "wrote", utils.RelToRoot(options.Root, agentPath))
	}
	return nil
}

func ensureOutputRoot(root string) error {
	if root == "" {
		return errors.New("root is required")
	}
	return nil
}

func openGrepRunWarning(label string, err error) string {
	if code, ok := opengrep.ExitCode(err); ok {
		return fmt.Sprintf("OpenGrep %s exited with status %d; preserving findings from generated output", label, code)
	}
	return fmt.Sprintf("OpenGrep %s reported an error after writing output: %s", label, err)
}

func removeOutputFile(path string) error {
	err := os.Remove(path)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type targetOptions struct {
	Root        string
	Changed     bool
	Targets     []string
	TargetsFrom string
}

type targetSelection struct {
	Targets      []string
	ChangedMode  bool
	ChangedFiles []string
	ExplicitMode bool
	EmptyWarning string
}

func resolveScanTargets(options targetOptions) (targetSelection, error) {
	explicitTargets, explicitMode, err := targetsFromOptions(options.Root, options.Targets, options.TargetsFrom)
	if err != nil {
		return targetSelection{}, err
	}
	selection := targetSelection{
		Targets:      []string{"."},
		ExplicitMode: explicitMode,
		ChangedMode:  options.Changed && !explicitMode,
	}
	if explicitMode {
		selection.Targets = explicitTargets
		if len(selection.Targets) == 0 {
			selection.EmptyWarning = "no explicit targets to scan"
		}
		return selection, nil
	}
	if !selection.ChangedMode {
		return selection, nil
	}
	changedFiles, err := utils.ChangedFiles(options.Root)
	if err != nil {
		return targetSelection{}, err
	}
	selection.Targets = changedFiles
	selection.ChangedFiles = changedFiles
	if len(selection.Targets) == 0 {
		selection.EmptyWarning = "no changed files to scan"
	}
	return selection, nil
}

func targetsFromOptions(root string, targets []string, targetsFrom string) ([]string, bool, error) {
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
	normalized, err := normalizeTargets(root, rawTargets)
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

func normalizeTargets(root string, rawTargets []string) ([]string, error) {
	seen := map[string]bool{}
	var normalized []string
	for _, raw := range rawTargets {
		target, err := normalizeTarget(root, raw)
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

func normalizeTarget(root string, raw string) (string, error) {
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

func baseAgentResult(root string, lock config.Lock, runtimeInfo opengrep.Runtime, changedMode bool, changedFiles []string, targets []string, scanConfigs []string, jsonPath string, sarifPath string) AgentResult {
	packs := make([]PackInfo, 0, len(lock.Packs))
	for _, pack := range lock.Packs {
		packs = append(packs, PackInfo{
			ID:         pack.ID,
			Version:    pack.Version,
			SHA256:     pack.SHA256,
			RulePath:   pack.RulePath,
			TotalRules: pack.TotalRules,
		})
	}
	return AgentResult{
		SchemaVersion: "greprules.agent.v1",
		Status:        "running",
		Repo: RepoInfo{
			Root:         root,
			ChangedMode:  changedMode,
			ChangedFiles: changedFiles,
		},
		Packs: packs,
		Engine: EngineInfo{
			Name:    runtimeInfo.Name,
			Mode:    runtimeInfo.Mode,
			Source:  runtimeInfo.Source,
			Version: runtimeInfo.Version,
			Path:    runtimeInfo.Path,
			SHA256:  runtimeInfo.SHA256,
			Managed: runtimeInfo.Managed,
		},
		Scan: ScanInfo{
			Targets: targets,
			Configs: scanConfigs,
		},
		Findings:  []Finding{},
		JSONPath:  utils.RelToRoot(root, jsonPath),
		SARIFPath: utils.RelToRoot(root, sarifPath),
	}
}
