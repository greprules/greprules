package agentstate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/greprules/greprules/internal/output"
)

const DefaultStateSubdir = ".greprules/plugin-data/agent"

type State struct {
	Root     string
	StateDir string
}

type SummaryOptions struct {
	Automatic bool
	Label     string
}

func New(root string, stateDir string) (State, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return State{}, err
	}
	if stateDir == "" {
		stateDir = filepath.Join(root, DefaultStateSubdir)
	}
	stateDir, err = filepath.Abs(stateDir)
	if err != nil {
		return State{}, err
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return State{}, err
	}
	return State{Root: root, StateDir: stateDir}, nil
}

func (state State) DirtyMarkerPath() string {
	return filepath.Join(state.StateDir, "dirty")
}

func (state State) DirtyFilesPath() string {
	return filepath.Join(state.StateDir, "dirty-files")
}

func (state State) ScanTargetsPath() string {
	return filepath.Join(state.StateDir, "scan-targets.txt")
}

func (state State) LastScanPath() string {
	return filepath.Join(state.StateDir, "last-scan")
}

func (state State) MarkDirty(paths []string) ([]string, error) {
	return state.MarkDirtyFrom(paths, state.Root)
}

func (state State) MarkDirtyFrom(paths []string, baseDir string) ([]string, error) {
	editedFiles := state.FilterScanCandidatesFrom(paths, baseDir)
	if len(editedFiles) == 0 {
		return nil, nil
	}
	if err := appendLines(state.DirtyFilesPath(), editedFiles); err != nil {
		return nil, err
	}
	if err := dedupeFileLines(state.DirtyFilesPath()); err != nil {
		return nil, err
	}
	marker := fmt.Sprintf("project=%s\nmarkedAt=%s\n", state.Root, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(state.DirtyMarkerPath(), []byte(marker), 0o644); err != nil {
		return nil, err
	}
	return editedFiles, nil
}

func (state State) ClearDirtyState() {
	_ = os.Remove(state.DirtyMarkerPath())
	_ = os.Remove(state.DirtyFilesPath())
	_ = os.Remove(state.ScanTargetsPath())
}

func (state State) RecordScanAttempt() {
	_ = os.WriteFile(state.LastScanPath(), []byte(strconv.FormatInt(time.Now().Unix(), 10)+"\n"), 0o644)
}

func (state State) LastScanUnix() (int64, bool) {
	data, err := os.ReadFile(state.LastScanPath())
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func (state State) PrepareScanTargets() ([]string, error) {
	lines, err := readNonEmptyLines(state.DirtyFilesPath())
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.WriteFile(state.ScanTargetsPath(), nil, 0o644)
			return nil, nil
		}
		return nil, err
	}
	targets := state.FilterScanCandidates(lines)
	if err := writeLines(state.ScanTargetsPath(), targets); err != nil {
		return nil, err
	}
	return targets, nil
}

func (state State) FilterScanCandidates(paths []string) []string {
	return state.FilterScanCandidatesFrom(paths, state.Root)
}

func (state State) FilterScanCandidatesFrom(paths []string, baseDir string) []string {
	seen := map[string]bool{}
	var filtered []string
	for _, raw := range paths {
		rel, ok := state.NormalizeExistingPathFrom(raw, baseDir)
		if !ok || !IsScanCandidate(rel) || seen[rel] {
			continue
		}
		seen[rel] = true
		filtered = append(filtered, rel)
	}
	sort.Strings(filtered)
	return filtered
}

func (state State) NormalizeExistingPath(raw string) (string, bool) {
	return state.NormalizeExistingPathFrom(raw, state.Root)
}

func (state State) NormalizeExistingPathFrom(raw string, baseDir string) (string, bool) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		return "", false
	}
	absolute := candidate
	if !filepath.IsAbs(absolute) {
		if baseDir == "" {
			baseDir = state.Root
		}
		absolute = filepath.Join(baseDir, absolute)
	}
	absolute, err := filepath.Abs(absolute)
	if err != nil {
		return "", false
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	root := state.Root
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

func IsScanCandidate(rel string) bool {
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

func SummarizeAgentResult(path string, options SummaryOptions) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("greprules scan finished. Full result: %s", path)
	}
	var result output.AgentResult
	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Sprintf("greprules scan finished. Full result: %s", path)
	}
	scanLabel := options.Label
	if scanLabel == "" {
		scanLabel = "edited-file"
		if result.Repo.ChangedMode {
			scanLabel = "changed-file"
		}
	}
	prefix := "greprules "
	if options.Automatic {
		prefix += "automatic "
	}
	lines := []string{
		fmt.Sprintf(
			"%s%s scan completed: status=%s, findings=%d, warnings=%d, errors=%d, targets=%d.",
			prefix,
			scanLabel,
			fallbackString(result.Status, "unknown"),
			len(result.Findings),
			len(result.Warnings),
			len(result.Errors),
			len(result.Scan.Targets),
		),
	}
	if len(result.Findings) == 0 {
		if options.Automatic {
			lines = append(lines, "No OpenGrep findings were reported for the current automatic scan.")
		} else {
			lines = append(lines, "No OpenGrep findings were reported for this scan.")
		}
	} else if options.Automatic {
		lines = append(lines, "Review .greprules/out/agent-result.json and classify each finding as true positive, false positive, or needs investigation. Report findings and reasoning only. Do not edit code, add suppressions, chase zero findings, or rerun greprules unless the user explicitly asks.")
	} else {
		lines = append(lines, "Review .greprules/out/agent-result.json and classify each finding as true positive, false positive, or needs investigation. Report findings and reasoning only unless the user asks for fixes.")
	}
	lines = append(lines, "Full result: .greprules/out/agent-result.json")
	return strings.Join(lines, "\n")
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

func fallbackString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
