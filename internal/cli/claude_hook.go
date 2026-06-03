package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/output"
)

const (
	claudeHookDefaultMinIntervalSeconds = 45
	claudeHookDefaultMaxChangedFiles    = 100
)

type claudeHookRuntime struct {
	projectDir      string
	stateDir        string
	logPath         string
	dirtyMarkerPath string
	dirtyFilesPath  string
	scanTargetsPath string
	lastScanPath    string
	lastSummaryPath string
	minInterval     int
	maxChangedFiles int
}

type claudeHookOutput struct {
	Continue           *bool                     `json:"continue,omitempty"`
	SystemMessage      string                    `json:"systemMessage,omitempty"`
	Decision           string                    `json:"decision,omitempty"`
	Reason             string                    `json:"reason,omitempty"`
	HookSpecificOutput *claudeHookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

type claudeHookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

func runClaudeHook(ctx context.Context, args []string) error {
	return runClaudeHookWithIO(ctx, args, os.Stdin, os.Stdout)
}

func runClaudeHookWithIO(ctx context.Context, args []string, stdin io.Reader, stdout io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: greprules claude-hook mark-dirty|scan-if-dirty|doctor")
	}
	runtime, err := newClaudeHookRuntime()
	if err != nil {
		return err
	}
	input, err := io.ReadAll(stdin)
	if err != nil {
		return err
	}
	switch args[0] {
	case "mark-dirty":
		return runtime.markDirty(input)
	case "scan-if-dirty":
		return runtime.scanIfDirty(ctx, input, stdout)
	case "doctor":
		return runtime.doctorContext(ctx, stdout)
	default:
		return fmt.Errorf("unknown claude-hook command: %s", args[0])
	}
}

func newClaudeHookRuntime() (claudeHookRuntime, error) {
	projectDir := os.Getenv("CLAUDE_PROJECT_DIR")
	if projectDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return claudeHookRuntime{}, err
		}
		projectDir = cwd
	}
	projectDir, err := filepath.Abs(projectDir)
	if err != nil {
		return claudeHookRuntime{}, err
	}
	stateDir := os.Getenv("GREPRULES_PLUGIN_STATE_DIR")
	if stateDir == "" {
		stateDir = filepath.Join(projectDir, ".greprules", "plugin-data", "claude-code")
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return claudeHookRuntime{}, err
	}
	return claudeHookRuntime{
		projectDir:      projectDir,
		stateDir:        stateDir,
		logPath:         filepath.Join(stateDir, "hook.log"),
		dirtyMarkerPath: filepath.Join(stateDir, "dirty"),
		dirtyFilesPath:  filepath.Join(stateDir, "dirty-files"),
		scanTargetsPath: filepath.Join(stateDir, "scan-targets.txt"),
		lastScanPath:    filepath.Join(stateDir, "last-scan"),
		lastSummaryPath: filepath.Join(stateDir, "last-summary.txt"),
		minInterval:     intFromEnv("GREPRULES_AUTO_SCAN_MIN_INTERVAL_SECONDS", claudeHookDefaultMinIntervalSeconds),
		maxChangedFiles: intFromEnv("GREPRULES_AUTO_SCAN_MAX_CHANGED_FILES", claudeHookDefaultMaxChangedFiles),
	}, nil
}

func intFromEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func (runtime claudeHookRuntime) logMsg(format string, args ...any) {
	line := fmt.Sprintf("%s %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, args...))
	file, err := os.OpenFile(runtime.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	_, _ = file.WriteString(line)
}

func (runtime claudeHookRuntime) recordScanAttempt() {
	_ = os.WriteFile(runtime.lastScanPath, []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"), 0o644)
}

func (runtime claudeHookRuntime) clearDirtyState() {
	_ = os.Remove(runtime.dirtyMarkerPath)
	_ = os.Remove(runtime.dirtyFilesPath)
	_ = os.Remove(runtime.scanTargetsPath)
}

func (runtime claudeHookRuntime) finishPendingScanWithMessage(stdout io.Writer, message string) error {
	runtime.recordScanAttempt()
	runtime.clearDirtyState()
	return emitClaudeHookSystemMessage(stdout, message)
}

func (runtime claudeHookRuntime) markDirty(input []byte) error {
	if !claudeHookAutoScanEnabled() {
		runtime.logMsg("auto scan disabled; dirty marker skipped")
		return nil
	}
	if stat, err := os.Stat(runtime.projectDir); err != nil || !stat.IsDir() {
		runtime.logMsg("dirty marker skipped because project dir is not available: %s", runtime.projectDir)
		return nil
	}
	editedFiles := runtime.filterScanCandidates(runtime.captureEditedFiles(input))
	if len(editedFiles) == 0 {
		runtime.logMsg("dirty marker skipped; no scan candidate files captured")
		return nil
	}
	if err := appendLines(runtime.dirtyFilesPath, editedFiles); err != nil {
		return err
	}
	if err := dedupeFileLines(runtime.dirtyFilesPath); err != nil {
		return err
	}
	marker := fmt.Sprintf("project=%s\nmarkedAt=%s\n", runtime.projectDir, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(runtime.dirtyMarkerPath, []byte(marker), 0o644); err != nil {
		return err
	}
	runtime.logMsg("marked dirty: %s files=%s", runtime.projectDir, strings.Join(editedFiles, ","))
	return nil
}

func (runtime claudeHookRuntime) scanIfDirty(ctx context.Context, input []byte, stdout io.Writer) error {
	if hookInputBool(input, "stop_hook_active", "stopHookActive") {
		runtime.logMsg("stop_hook_active=true; skipping automatic scan block")
		runtime.clearDirtyState()
		return nil
	}
	if !claudeHookAutoScanEnabled() {
		runtime.logMsg("auto scan disabled")
		runtime.clearDirtyState()
		return nil
	}
	if _, err := os.Stat(runtime.dirtyMarkerPath); err != nil {
		if os.IsNotExist(err) {
			runtime.logMsg("scan skipped; no dirty marker")
			return nil
		}
		return err
	}
	if runtime.minInterval > 0 {
		last, ok := runtime.lastScanUnix()
		if ok {
			elapsed := int(time.Since(time.Unix(last, 0)).Seconds())
			if elapsed < runtime.minInterval {
				runtime.logMsg("scan skipped; min interval not reached: %ds < %ds", elapsed, runtime.minInterval)
				runtime.clearDirtyState()
				return nil
			}
		}
	}
	if stat, err := os.Stat(runtime.projectDir); err != nil || !stat.IsDir() {
		runtime.logMsg("scan skipped because project dir is not available: %s", runtime.projectDir)
		runtime.recordScanAttempt()
		runtime.clearDirtyState()
		return nil
	}
	targets, err := runtime.prepareScanTargets()
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		runtime.clearDirtyState()
		runtime.logMsg("scan skipped; no edited files captured")
		return nil
	}
	if len(targets) > runtime.maxChangedFiles {
		return runtime.finishPendingScanWithMessage(stdout, fmt.Sprintf("greprules automatic scan skipped because %d edited files exceed the automatic limit (%d). Run /greprules:scan when ready.", len(targets), runtime.maxChangedFiles))
	}

	report, err := buildDoctorReport(ctx, runtime.projectDir, false, "", "", "")
	if err != nil {
		runtime.logMsg("doctor failed: %s", err)
		return runtime.finishPendingScanWithMessage(stdout, "greprules automatic scan skipped because doctor failed: "+err.Error())
	}
	if !report.Lock.Exists && report.Registry.OK {
		var fetchOutput bytes.Buffer
		if err := runFetchWithOptions(ctx, []string{"--root", runtime.projectDir}, fetchCommandOptions{quiet: true, stdout: &fetchOutput}); err != nil {
			runtime.logMsg("fetch failed: %s", strings.TrimSpace(fetchOutput.String()+"\n"+err.Error()))
			return runtime.finishPendingScanWithMessage(stdout, "greprules automatic scan skipped because rule pack fetch failed: "+strings.TrimSpace(fetchOutput.String()+"\n"+err.Error()))
		}
		report, err = buildDoctorReport(ctx, runtime.projectDir, false, "", "", "")
		if err != nil {
			return runtime.finishPendingScanWithMessage(stdout, "greprules fetched rule packs, but readiness check failed: "+err.Error())
		}
	}
	if !report.OpenGrep.Active.OK {
		recommended := strings.Join(report.RecommendedCommands, ", ")
		if recommended == "" {
			recommended = "greprules setup-opengrep"
		}
		return runtime.finishPendingScanWithMessage(stdout, "greprules automatic scan skipped because OpenGrep is not ready. Recommended commands: "+recommended)
	}

	var scanOutput bytes.Buffer
	if err := runScanWithOptions(ctx, []string{"--root", runtime.projectDir, "--targets-from", runtime.scanTargetsPath}, scanCommandOptions{
		quiet:  true,
		stdout: &scanOutput,
		stderr: &scanOutput,
	}); err != nil {
		message := strings.TrimSpace(scanOutput.String() + "\nerror: " + err.Error())
		runtime.logMsg("scan failed: %s", message)
		return runtime.finishPendingScanWithMessage(stdout, "greprules automatic edited-file scan failed: "+message)
	}

	summary := summarizeAgentResult(filepath.Join(runtime.projectDir, ".greprules", "out", "agent-result.json"))
	_ = os.WriteFile(runtime.lastSummaryPath, []byte(summary+"\n"), 0o644)
	runtime.recordScanAttempt()
	runtime.clearDirtyState()
	return emitClaudeHookBlock(stdout, summary)
}

func (runtime claudeHookRuntime) doctorContext(ctx context.Context, stdout io.Writer) error {
	if !claudeHookAutoScanEnabled() {
		runtime.logMsg("auto scan disabled; doctor skipped")
		return nil
	}
	if stat, err := os.Stat(runtime.projectDir); err != nil || !stat.IsDir() {
		runtime.logMsg("doctor skipped because project dir is not available: %s", runtime.projectDir)
		return nil
	}
	report, err := buildDoctorReport(ctx, runtime.projectDir, false, "", "", "")
	if err != nil {
		runtime.logMsg("doctor failed: %s", err)
		return emitClaudeHookSystemMessage(stdout, "greprules readiness check failed: "+err.Error())
	}
	if report.Registry.OK && report.Lock.Exists && report.OpenGrep.Active.OK {
		runtime.logMsg("doctor ok")
		return nil
	}

	cfg := report.Config.Config
	setupGuidance := ""
	if !report.OpenGrep.Active.OK {
		setupGuidance = " Run /greprules:configure or /greprules:doctor to choose an OpenGrep runtime."
		if report.OpenGrep.System.OK && report.OpenGrep.System.Runtime != nil {
			setupGuidance += " System OpenGrep was detected at " + fallbackString(report.OpenGrep.System.Runtime.Path, "opengrep")
			if report.OpenGrep.System.Runtime.Version != "" {
				setupGuidance += " (version " + report.OpenGrep.System.Runtime.Version + ")"
			}
			setupGuidance += "."
		} else {
			setupGuidance += " No system opengrep was found on PATH."
		}
	}
	recommended := strings.Join(report.RecommendedCommands, ", ")
	if recommended == "" {
		recommended = "greprules doctor --format json"
	}
	message := fmt.Sprintf(
		"greprules needs setup before automatic scans. Registry ready: %t; lockfile exists: %t; OpenGrep ready: %t; configured OpenGrep mode: %s. Recommended commands: %s.%s",
		report.Registry.OK,
		report.Lock.Exists,
		report.OpenGrep.Active.OK,
		fallbackString(cfg.OpenGrep.Mode, "unknown"),
		recommended,
		setupGuidance,
	)
	return emitClaudeHookSystemMessage(stdout, message)
}

func claudeHookAutoScanEnabled() bool {
	switch os.Getenv("GREPRULES_AUTO_SCAN") {
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return true
	}
}

func appendLines(path string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	for _, line := range lines {
		if _, err := fmt.Fprintln(file, line); err != nil {
			return err
		}
	}
	return nil
}

func dedupeFileLines(path string) error {
	lines, err := readNonEmptyLines(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	sort.Strings(lines)
	unique := lines[:0]
	var previous string
	for i, line := range lines {
		if i == 0 || line != previous {
			unique = append(unique, line)
			previous = line
		}
	}
	return writeLines(path, unique)
}

func readNonEmptyLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func writeLines(path string, lines []string) error {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line)
		builder.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func (runtime claudeHookRuntime) lastScanUnix() (int64, bool) {
	data, err := os.ReadFile(runtime.lastScanPath)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func (runtime claudeHookRuntime) prepareScanTargets() ([]string, error) {
	lines, err := readNonEmptyLines(runtime.dirtyFilesPath)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(runtime.scanTargetsPath, nil, 0o644)
			return nil, nil
		}
		return nil, err
	}
	targets := runtime.filterScanCandidates(lines)
	if err := writeLines(runtime.scanTargetsPath, targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func (runtime claudeHookRuntime) captureEditedFiles(input []byte) []string {
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return nil
	}
	pathKeys := map[string]bool{
		"file_path":     true,
		"filePath":      true,
		"notebook_path": true,
		"notebookPath":  true,
	}
	toolName := stringFromAny(firstPresent(payload, "tool_name", "toolName"))
	switch toolName {
	case "Edit", "MultiEdit", "Write", "NotebookEdit":
		pathKeys["path"] = true
	}
	seen := map[string]bool{}
	var paths []string
	var walk func(value any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, item := range typed {
				if pathKeys[key] {
					if rel, ok := runtime.normalizeExistingPath(stringFromAny(item)); ok && !seen[rel] {
						seen[rel] = true
						paths = append(paths, rel)
					}
					continue
				}
				walk(item)
			}
		case []any:
			for _, item := range typed {
				walk(item)
			}
		}
	}
	walk(firstPresent(payload, "tool_input", "toolInput"))
	walk(firstPresent(payload, "tool_response", "toolResponse"))
	sort.Strings(paths)
	return paths
}

func (runtime claudeHookRuntime) normalizeExistingPath(raw string) (string, bool) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", false
	}
	absolute := candidate
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(runtime.projectDir, absolute)
	}
	absolute, err := filepath.Abs(absolute)
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	root := runtime.projectDir
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	rel, err := filepath.Rel(root, absolute)
	if err != nil {
		return "", false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", false
	}
	info, err := os.Stat(absolute)
	if err != nil || info.IsDir() {
		return "", false
	}
	return filepath.Clean(rel), true
}

func (runtime claudeHookRuntime) filterScanCandidates(paths []string) []string {
	seen := map[string]bool{}
	var filtered []string
	for _, raw := range paths {
		rel, ok := runtime.normalizeExistingPath(raw)
		if !ok || !isClaudeHookScanCandidate(rel) || seen[rel] {
			continue
		}
		seen[rel] = true
		filtered = append(filtered, rel)
	}
	sort.Strings(filtered)
	return filtered
}

func isClaudeHookScanCandidate(rel string) bool {
	ignoredDirs := map[string]bool{
		".git":         true,
		".greprules":   true,
		".hg":          true,
		".svn":         true,
		".cache":       true,
		".next":        true,
		".nuxt":        true,
		".turbo":       true,
		".venv":        true,
		"build":        true,
		"coverage":     true,
		"dist":         true,
		"node_modules": true,
		"target":       true,
		"vendor":       true,
		"venv":         true,
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		if ignoredDirs[part] {
			return false
		}
	}
	name := strings.ToLower(filepath.Base(rel))
	scanFilenames := map[string]bool{
		".dockerignore":       true,
		".npmrc":              true,
		"brewfile":            true,
		"cargo.lock":          true,
		"cargo.toml":          true,
		"composer.json":       true,
		"composer.lock":       true,
		"containerfile":       true,
		"dockerfile":          true,
		"gemfile":             true,
		"gemfile.lock":        true,
		"go.mod":              true,
		"go.sum":              true,
		"jenkinsfile":         true,
		"makefile":            true,
		"package-lock.json":   true,
		"package.json":        true,
		"pipfile":             true,
		"pipfile.lock":        true,
		"pnpm-lock.yaml":      true,
		"podfile":             true,
		"poetry.lock":         true,
		"pom.xml":             true,
		"pyproject.toml":      true,
		"rakefile":            true,
		"requirements.txt":    true,
		"settings.gradle":     true,
		"settings.gradle.kts": true,
		"tsconfig.json":       true,
		"yarn.lock":           true,
	}
	if scanFilenames[name] || strings.HasPrefix(name, "dockerfile.") {
		return true
	}
	scanExtensions := map[string]bool{
		".bash":       true,
		".c":          true,
		".cc":         true,
		".cfg":        true,
		".clj":        true,
		".cljs":       true,
		".conf":       true,
		".cpp":        true,
		".cs":         true,
		".cxx":        true,
		".dart":       true,
		".ex":         true,
		".exs":        true,
		".fs":         true,
		".go":         true,
		".gql":        true,
		".gradle":     true,
		".graphql":    true,
		".groovy":     true,
		".h":          true,
		".hcl":        true,
		".hh":         true,
		".hpp":        true,
		".hrl":        true,
		".hs":         true,
		".htm":        true,
		".html":       true,
		".hxx":        true,
		".ini":        true,
		".java":       true,
		".js":         true,
		".json":       true,
		".jsx":        true,
		".kt":         true,
		".kts":        true,
		".lua":        true,
		".m":          true,
		".mjs":        true,
		".ml":         true,
		".mli":        true,
		".mm":         true,
		".nim":        true,
		".php":        true,
		".phtml":      true,
		".pl":         true,
		".pm":         true,
		".properties": true,
		".proto":      true,
		".ps1":        true,
		".py":         true,
		".pyw":        true,
		".r":          true,
		".rb":         true,
		".rego":       true,
		".rs":         true,
		".scala":      true,
		".sh":         true,
		".sol":        true,
		".sql":        true,
		".svelte":     true,
		".swift":      true,
		".tf":         true,
		".tfvars":     true,
		".toml":       true,
		".ts":         true,
		".tsx":        true,
		".vue":        true,
		".xml":        true,
		".yaml":       true,
		".yml":        true,
		".zig":        true,
		".zsh":        true,
	}
	return scanExtensions[filepath.Ext(name)]
}

func hookInputBool(input []byte, keys ...string) bool {
	var payload map[string]any
	if err := json.Unmarshal(input, &payload); err != nil {
		return false
	}
	for _, key := range keys {
		value, ok := payload[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			return typed == "true"
		}
	}
	return false
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func stringFromAny(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func emitClaudeHookSystemMessage(writer io.Writer, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	cont := true
	return writeClaudeHookOutput(writer, claudeHookOutput{Continue: &cont, SystemMessage: message})
}

func emitClaudeHookBlock(writer io.Writer, message string) error {
	message = strings.TrimSpace(message)
	if message == "" {
		return nil
	}
	return writeClaudeHookOutput(writer, claudeHookOutput{Decision: "block", Reason: message})
}

func writeClaudeHookOutput(writer io.Writer, payload claudeHookOutput) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(writer, string(data))
	return err
}

func summarizeAgentResult(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("greprules scan finished. Full result: %s", path)
	}
	var result output.AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Sprintf("greprules scan finished. Full result: %s", path)
	}
	scanLabel := "edited-file"
	if result.Repo.ChangedMode {
		scanLabel = "changed-file"
	}
	lines := []string{fmt.Sprintf("greprules automatic %s scan completed with status: %s.", scanLabel, fallbackString(result.Status, "unknown"))}
	if len(result.Warnings) > 0 {
		lines = append(lines, "Warnings: "+strings.Join(firstNStrings(result.Warnings, 3), "; "))
	}
	if len(result.Errors) > 0 {
		lines = append(lines, "Errors: "+strings.Join(firstNStrings(result.Errors, 3), "; "))
	}
	if len(result.Findings) == 0 {
		lines = append(lines, "No OpenGrep findings were reported for the current automatic scan.")
	} else {
		lines = append(lines, fmt.Sprintf(
			"OpenGrep reported %d finding(s) on files you edited. Review .greprules/out/agent-result.json and any relevant project context needed to classify each as true positive, false positive, or needs investigation. Report findings and reasoning only. Do not edit code, add nosemgrep/suppressions, chase zero findings, or rerun greprules unless the user explicitly asks.",
			len(result.Findings),
		))
		for _, finding := range result.Findings[:minInt(len(result.Findings), 10)] {
			message := strings.Join(strings.Fields(finding.Message), " ")
			lines = append(lines, fmt.Sprintf("- %s %s %s:%d %s", fallbackString(finding.Severity, "unknown"), fallbackString(finding.RuleID, "<unknown-rule>"), fallbackString(finding.Path, "<unknown>"), finding.Start.Line, message))
		}
		if len(result.Findings) > 10 {
			lines = append(lines, fmt.Sprintf("- %d additional finding(s) omitted from hook context.", len(result.Findings)-10))
		}
	}
	if len(result.Scan.Targets) > 0 {
		lines = append(lines, "Scanned targets: "+strings.Join(firstNStrings(result.Scan.Targets, 10), ", "))
		if len(result.Scan.Targets) > 10 {
			lines = append(lines, fmt.Sprintf("- %d additional target(s) omitted from hook context.", len(result.Scan.Targets)-10))
		}
	}
	lines = append(lines, "Full result: .greprules/out/agent-result.json")
	return strings.Join(lines, "\n")
}

func firstNStrings(values []string, n int) []string {
	if len(values) < n {
		n = len(values)
	}
	return values[:n]
}

func fallbackString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func minInt(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
