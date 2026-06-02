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
	acc := accumulator{
		root:       root,
		languages:  map[string]*Signal{},
		frameworks: map[string]*Signal{},
	}
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

type accumulator struct {
	root       string
	languages  map[string]*Signal
	frameworks map[string]*Signal
	warnings   []string
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
	case ".csproj", ".sln":
		a.addLanguage("csharp", 0.85, rel)
		a.addLanguage("dotnet", 0.8, rel)
	case ".tf", ".yaml", ".yml", ".json":
		if strings.Contains(rel, ".github/workflows/") || strings.Contains(rel, "k8s") || strings.Contains(rel, "kubernetes") {
			a.addLanguage("config", 0.6, rel)
		}
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
	return Result{
		Root:       a.root,
		Languages:  sortedSignals(a.languages),
		Frameworks: sortedSignals(a.frameworks),
		Warnings:   a.warnings,
	}
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
