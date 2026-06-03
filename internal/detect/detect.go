package detect

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Signal struct {
	Name       string   `json:"name"`
	Confidence float64  `json:"confidence"`
	Sources    []string `json:"sources"`
}

type Result struct {
	Root       string   `json:"root"`
	Targets    []string `json:"targets,omitempty"`
	Languages  []Signal `json:"languages"`
	Frameworks []Signal `json:"frameworks"`
	Warnings   []string `json:"warnings,omitempty"`
}

func FindRepoRoot(start string) (string, error) {
	if start == "" {
		start = "."
	}
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	if root, err := gitRoot(abs); err == nil && root != "" {
		return root, nil
	}
	current := abs
	for {
		if _, err := os.Stat(filepath.Join(current, ".git")); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return abs, nil
		}
		current = parent
	}
}

func Detect(root string) (Result, error) {
	root, err := FindRepoRoot(root)
	if err != nil {
		return Result{}, err
	}
	acc := newAccumulator(root)
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			acc.warnings = append(acc.warnings, walkErr.Error())
			return nil
		}
		if path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if entry.IsDir() {
			if shouldSkipDir(entry.Name()) || strings.Count(rel, string(os.PathSeparator)) > 4 {
				return filepath.SkipDir
			}
			return nil
		}
		acc.inspectFile(path, rel)
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return acc.result(), nil
}

func DetectTargets(root string, rawTargets []string) (Result, error) {
	if len(rawTargets) == 0 {
		return Detect(root)
	}
	root, err := FindRepoRoot(root)
	if err != nil {
		return Result{}, err
	}
	acc := accumulator{
		root:       root,
		targets:    []string{},
		languages:  map[string]*Signal{},
		frameworks: map[string]*Signal{},
	}
	seen := map[string]bool{}
	for _, raw := range rawTargets {
		target, rel, ok := normalizeTarget(root, raw)
		if !ok {
			acc.warnings = append(acc.warnings, "target is not available or outside root: "+raw)
			continue
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		acc.targets = append(acc.targets, rel)
		acc.inspectContextManifests(target)
		info, err := os.Stat(target)
		if err != nil {
			acc.warnings = append(acc.warnings, err.Error())
			continue
		}
		if !info.IsDir() {
			acc.inspectTargetFile(target, rel)
			continue
		}
		_ = filepath.WalkDir(target, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				acc.warnings = append(acc.warnings, walkErr.Error())
				return nil
			}
			if path == target {
				return nil
			}
			childRel, _ := filepath.Rel(root, path)
			if entry.IsDir() {
				if shouldSkipDir(entry.Name()) || strings.Count(childRel, string(os.PathSeparator))-strings.Count(rel, string(os.PathSeparator)) > 4 {
					return filepath.SkipDir
				}
				return nil
			}
			acc.inspectTargetFile(path, childRel)
			return nil
		})
	}
	return acc.result(), nil
}

type accumulator struct {
	root       string
	targets    []string
	languages  map[string]*Signal
	frameworks map[string]*Signal
	warnings   []string
}

func newAccumulator(root string) accumulator {
	return accumulator{
		root:       root,
		languages:  map[string]*Signal{},
		frameworks: map[string]*Signal{},
	}
}

func (a *accumulator) inspectFile(path, rel string) {
	name := filepath.Base(path)
	lower := strings.ToLower(name)
	switch lower {
	case "package.json":
		a.addLanguage("javascript", 0.78, rel)
		a.inspectPackageJSON(path, rel)
	case "tsconfig.json":
		a.addLanguage("typescript", 0.9, rel)
	case "go.mod":
		a.addLanguage("go", 0.95, rel)
	case "pyproject.toml", "requirements.txt", "setup.py", "pipfile":
		a.addLanguage("python", 0.86, rel)
		a.inspectPython(path, rel)
	case "pom.xml", "build.gradle", "build.gradle.kts":
		a.addLanguage("java", 0.86, rel)
		a.addLanguage("jvm", 0.8, rel)
		a.inspectTextFrameworks(path, rel, map[string]string{
			"spring": "spring",
		})
	case "composer.json":
		a.addLanguage("php", 0.88, rel)
		a.inspectTextFrameworks(path, rel, map[string]string{
			"laravel": "laravel",
			"symfony": "symfony",
		})
	case "gemfile":
		a.addLanguage("ruby", 0.86, rel)
		a.inspectTextFrameworks(path, rel, map[string]string{
			"rails": "rails",
		})
	case "cargo.toml":
		a.addLanguage("rust", 0.9, rel)
	case "dockerfile", "docker-compose.yml", "docker-compose.yaml", "kustomization.yaml":
		a.addLanguage("config", 0.7, rel)
	}
	ext := strings.ToLower(filepath.Ext(name))
	switch ext {
	case ".js", ".jsx", ".mjs", ".cjs":
		a.addLanguage("javascript", 0.68, rel+":extension"+ext)
	case ".ts", ".tsx":
		a.addLanguage("typescript", 0.72, rel+":extension"+ext)
	case ".py":
		a.addLanguage("python", 0.72, rel+":extension.py")
	case ".go":
		a.addLanguage("go", 0.74, rel+":extension.go")
	case ".java", ".kt", ".kts", ".scala":
		a.addLanguage("java", 0.7, rel+":extension"+ext)
		a.addLanguage("jvm", 0.66, rel+":extension"+ext)
	case ".php":
		a.addLanguage("php", 0.72, rel+":extension.php")
	case ".rb":
		a.addLanguage("ruby", 0.72, rel+":extension.rb")
	case ".rs":
		a.addLanguage("rust", 0.72, rel+":extension.rs")
	case ".c", ".h":
		a.addLanguage("c", 0.68, rel+":extension"+ext)
	case ".cc", ".cpp", ".cxx", ".hpp", ".hh":
		a.addLanguage("cpp", 0.68, rel+":extension"+ext)
	case ".csproj", ".sln":
		a.addLanguage("csharp", 0.85, rel)
		a.addLanguage("dotnet", 0.8, rel)
	case ".cs":
		a.addLanguage("csharp", 0.72, rel+":extension.cs")
	case ".tf", ".yaml", ".yml", ".json":
		if strings.Contains(rel, ".github/workflows/") || strings.Contains(rel, "k8s") || strings.Contains(rel, "kubernetes") {
			a.addLanguage("config", 0.6, rel)
		}
	}
}

func (a *accumulator) inspectTargetFile(path, rel string) {
	a.inspectFile(path, rel)
	a.inspectSourceFrameworks(path, rel)
}

func (a *accumulator) inspectSourceFrameworks(path, rel string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".py", ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx", ".java", ".kt", ".kts", ".php", ".rb":
	default:
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	if len(data) > 1024*1024 {
		data = data[:1024*1024]
	}
	lower := strings.ToLower(string(data))
	switch ext {
	case ".py":
		if strings.Contains(lower, "from fastapi") || strings.Contains(lower, "import fastapi") {
			a.addFramework("fastapi", 0.82, rel+":import.fastapi")
		}
		if strings.Contains(lower, "from django") || strings.Contains(lower, "import django") {
			a.addFramework("django", 0.82, rel+":import.django")
		}
		if strings.Contains(lower, "from flask") || strings.Contains(lower, "import flask") {
			a.addFramework("flask", 0.82, rel+":import.flask")
		}
	case ".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx":
		if containsAny(lower, `"next`, "'next", "`next") {
			a.addFramework("nextjs", 0.78, rel+":import.next")
		}
		if strings.Contains(lower, "react") {
			a.addFramework("react", 0.72, rel+":import.react")
		}
		if strings.Contains(lower, "express") {
			a.addFramework("express", 0.76, rel+":import.express")
		}
		if strings.Contains(lower, "@nestjs/") || strings.Contains(lower, "from '@nestjs") || strings.Contains(lower, `from "@nestjs`) {
			a.addFramework("nestjs", 0.8, rel+":import.nestjs")
		}
	case ".java", ".kt", ".kts":
		if strings.Contains(lower, "org.springframework") || strings.Contains(lower, "@springbootapplication") {
			a.addFramework("spring", 0.82, rel+":spring")
		}
	case ".php":
		if strings.Contains(lower, "illuminate\\") || strings.Contains(lower, "laravel") {
			a.addFramework("laravel", 0.78, rel+":laravel")
		}
	case ".rb":
		if strings.Contains(lower, "rails") || strings.Contains(lower, "applicationrecord") || strings.Contains(lower, "actioncontroller") {
			a.addFramework("rails", 0.76, rel+":rails")
		}
	}
}

func (a *accumulator) inspectContextManifests(target string) {
	info, err := os.Stat(target)
	if err != nil {
		return
	}
	dir := target
	if !info.IsDir() {
		dir = filepath.Dir(target)
	}
	root, _ := filepath.Abs(a.root)
	dir, _ = filepath.Abs(dir)
	for {
		if rel, err := filepath.Rel(root, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			break
		}
		for _, name := range contextManifestNames() {
			path := filepath.Join(dir, name)
			if fileInfo, err := os.Stat(path); err == nil && !fileInfo.IsDir() {
				rel, _ := filepath.Rel(a.root, path)
				a.inspectFile(path, rel)
			}
		}
		if dir == root {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
}

func (a *accumulator) inspectPackageJSON(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	deps := map[string]string{}
	for name := range pkg.Dependencies {
		deps[strings.ToLower(name)] = name
	}
	for name := range pkg.DevDependencies {
		deps[strings.ToLower(name)] = name
	}
	if _, ok := deps["typescript"]; ok {
		a.addLanguage("typescript", 0.9, rel+":dependencies.typescript")
	}
	frameworks := map[string]string{
		"next":          "nextjs",
		"react":         "react",
		"vue":           "vue",
		"express":       "express",
		"nestjs":        "nestjs",
		"@nestjs/core":  "nestjs",
		"angular":       "angular",
		"@angular/core": "angular",
		"svelte":        "svelte",
	}
	for dep, framework := range frameworks {
		if _, ok := deps[dep]; ok {
			a.addFramework(framework, 0.88, rel+":dependencies."+dep)
		}
	}
}

func (a *accumulator) inspectPython(path, rel string) {
	a.inspectTextFrameworks(path, rel, map[string]string{
		"django":  "django",
		"flask":   "flask",
		"fastapi": "fastapi",
	})
}

func (a *accumulator) inspectTextFrameworks(path, rel string, markers map[string]string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	lower := strings.ToLower(string(data))
	for marker, framework := range markers {
		if strings.Contains(lower, marker) {
			a.addFramework(framework, 0.82, rel+":"+marker)
		}
	}
}

func (a *accumulator) addLanguage(name string, confidence float64, source string) {
	addSignal(a.languages, name, confidence, source)
}

func (a *accumulator) addFramework(name string, confidence float64, source string) {
	addSignal(a.frameworks, name, confidence, source)
}

func addSignal(target map[string]*Signal, name string, confidence float64, source string) {
	if existing, ok := target[name]; ok {
		if confidence > existing.Confidence {
			existing.Confidence = confidence
		}
		existing.Sources = appendUnique(existing.Sources, source)
		return
	}
	target[name] = &Signal{Name: name, Confidence: confidence, Sources: []string{source}}
}

func (a *accumulator) result() Result {
	sort.Strings(a.targets)
	return Result{
		Root:       a.root,
		Targets:    a.targets,
		Languages:  sortedSignals(a.languages),
		Frameworks: sortedSignals(a.frameworks),
		Warnings:   a.warnings,
	}
}

func normalizeTarget(root string, raw string) (string, string, bool) {
	target := strings.TrimSpace(raw)
	if target == "" {
		return "", "", false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(root, target)
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return "", "", false
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	rootAbs, _ := filepath.Abs(root)
	if resolvedRoot, err := filepath.EvalSymlinks(rootAbs); err == nil {
		rootAbs = resolvedRoot
	}
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", "", false
	}
	if _, err := os.Stat(target); err != nil {
		return "", "", false
	}
	return target, filepath.Clean(rel), true
}

func contextManifestNames() []string {
	return []string{
		"package.json",
		"tsconfig.json",
		"go.mod",
		"pyproject.toml",
		"requirements.txt",
		"setup.py",
		"Pipfile",
		"pom.xml",
		"build.gradle",
		"build.gradle.kts",
		"composer.json",
		"Gemfile",
		"Cargo.toml",
		"docker-compose.yml",
		"docker-compose.yaml",
		"kustomization.yaml",
	}
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func sortedSignals(values map[string]*Signal) []Signal {
	result := make([]Signal, 0, len(values))
	for _, signal := range values {
		sort.Strings(signal.Sources)
		result = append(result, *signal)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Confidence == result[j].Confidence {
			return result[i].Name < result[j].Name
		}
		return result[i].Confidence > result[j].Confidence
	})
	return result
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".greprules", "node_modules", "vendor", ".venv", "venv", "dist", "build", "target", ".next":
		return true
	default:
		return false
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func gitRoot(path string) (string, error) {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--show-toplevel")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	root := strings.TrimSpace(string(out))
	if root == "" {
		return "", errors.New("empty git root")
	}
	return root, nil
}
