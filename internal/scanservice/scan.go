package scanservice

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
	"github.com/greprules/greprules/internal/gitutil"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/output"
	"github.com/greprules/greprules/internal/projectpath"
	"github.com/greprules/greprules/internal/runtimeconfig"
)

type Options struct {
	Root            string
	Changed         bool
	Full            bool
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

func Run(ctx context.Context, options Options) error {
	if err := ensureOutputRoot(options.Root); err != nil {
		return err
	}
	cfg, err := runtimeconfig.LoadOrDefaultConfig(options.Root)
	if err != nil {
		return err
	}
	lock, err := config.LoadLock(options.Root)
	if err != nil {
		return fmt.Errorf("lockfile missing; run greprules fetch first: %w", err)
	}
	runtimeInfo, err := runtimeconfig.FromConfigOrLock(lock, cfg, options.EngineMode, options.OpenGrepPath, options.OpenGrepVersion)
	if err != nil {
		return fmt.Errorf("OpenGrep runtime is not available: %w", err)
	}
	lock.Engine = runtimeconfig.LockedEngineFromRuntime(runtimeInfo)
	if err := config.SaveLock(options.Root, lock); err != nil {
		return err
	}
	configuredOutputDir := cfg.OutputDir
	if options.OutputDir != "" {
		configuredOutputDir = options.OutputDir
	}
	outputDir := projectpath.AbsFromRoot(options.Root, configuredOutputDir)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	explicitTargets, explicitTargetMode, err := scanTargetsFromFlags(options.Root, options.Targets, options.TargetsFrom)
	if err != nil {
		return err
	}
	if explicitTargetMode && options.Full {
		return errors.New("--full cannot be combined with --target or --targets-from")
	}
	targets := []string{"."}
	changedMode := options.Changed && !options.Full && !explicitTargetMode
	var changedFiles []string
	var emptyTargetsWarning string
	if explicitTargetMode {
		targets = explicitTargets
		if len(targets) == 0 {
			emptyTargetsWarning = "no explicit targets to scan"
		}
	} else if changedMode {
		changedFiles, err = gitutil.ChangedFiles(options.Root)
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
	scanConfigs := combinedScanConfigs(options.Root, lock, cfg.OpenGrep.IncludeDefaultRules)
	result := baseAgentResult(options.Root, lock, runtimeInfo, changedMode, changedFiles, targets, scanConfigs, jsonPath, sarifPath)
	result.Scan.StartedAt = startedAt
	if emptyTargetsWarning != "" {
		result.Status = "ok"
		result.Warnings = append(result.Warnings, emptyTargetsWarning)
		result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		return output.WriteAgentResult(agentPath, result)
	}
	if err := removeOutputFile(jsonPath); err != nil {
		return err
	}
	jsonRunErr := opengrep.RunScan(ctx, runtimeInfo, opengrep.ScanOptions{
		WorkingDir: options.Root,
		Configs:    scanConfigs,
		Targets:    targets,
		OutputPath: jsonPath,
		Format:     "json",
		Stdout:     options.Stdout,
		Stderr:     options.Stderr,
	})
	findings, parseErr := output.FindingsFromOpenGrepJSON(jsonPath)
	if parseErr != nil {
		result.Warnings = append(result.Warnings, "could not parse OpenGrep JSON: "+parseErr.Error())
		if jsonRunErr != nil {
			result.Status = "failed"
			result.Errors = append(result.Errors, jsonRunErr.Error())
			result.Scan.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			_ = output.WriteAgentResult(agentPath, result)
			return jsonRunErr
		}
	} else {
		result.Findings = findings
		if jsonRunErr != nil {
			result.Warnings = append(result.Warnings, openGrepRunWarning("JSON run", jsonRunErr))
		}
		if warnings, err := output.WarningsFromOpenGrepJSON(jsonPath); err != nil {
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
			Targets:    targets,
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
	if err := output.WriteAgentResult(agentPath, result); err != nil {
		return err
	}
	if !options.Quiet {
		writer := options.Stdout
		if writer == nil {
			writer = os.Stdout
		}
		fmt.Fprintln(writer, "wrote", projectpath.RelToRoot(options.Root, agentPath))
	}
	return nil
}

func ensureOutputRoot(root string) error {
	if root == "" {
		return errors.New("root is required")
	}
	return nil
}

func combinedScanConfigs(root string, lock config.Lock, includeDefaultRules bool) []string {
	paths := make([]string, 0, len(lock.Packs))
	for _, pack := range lock.Packs {
		paths = append(paths, projectpath.AbsFromRoot(root, pack.RulePath))
	}
	if includeDefaultRules {
		paths = append(paths, "auto")
	}
	return paths
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

func scanTargetsFromFlags(root string, targets []string, targetsFrom string) ([]string, bool, error) {
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

func baseAgentResult(root string, lock config.Lock, runtimeInfo opengrep.Runtime, changedMode bool, changedFiles []string, targets []string, scanConfigs []string, jsonPath string, sarifPath string) output.AgentResult {
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
			Configs: scanConfigs,
		},
		Findings:  []output.Finding{},
		JSONPath:  projectpath.RelToRoot(root, jsonPath),
		SARIFPath: projectpath.RelToRoot(root, sarifPath),
	}
}
