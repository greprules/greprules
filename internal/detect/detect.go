package detect

import (
	"bufio"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"

	enry "github.com/go-enry/go-enry/v2"
	"github.com/pelletier/go-toml/v2"
)

const maxDetectionBytes = 1024 * 1024

var (
	gradleCoordinatePattern   = regexp.MustCompile(`["']([A-Za-z0-9_.-]+):([A-Za-z0-9_.-]+)(?::[^"']*)?["']`)
	gradlePluginPattern       = regexp.MustCompile(`\bid\s*\(?\s*["']([^"']+)["']`)
	gemDeclarationPattern     = regexp.MustCompile(`^\s*gem\s+["']([^"']+)["']`)
	pythonNameSeparatorRegexp = regexp.MustCompile(`[-_.]+`)
	quotedRequirementPattern  = regexp.MustCompile(`["']([^"']+)["']`)
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
	return DetectExact(root)
}

func DetectExact(root string) (Result, error) {
	root, err := filepath.Abs(root)
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
	return DetectTargetsExact(root, rawTargets)
}

func DetectTargetsExact(root string, rawTargets []string) (Result, error) {
	if len(rawTargets) == 0 {
		return DetectExact(root)
	}
	root, err := filepath.Abs(root)
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
		info, err := os.Stat(target)
		if err != nil {
			acc.warnings = append(acc.warnings, err.Error())
			continue
		}
		if !info.IsDir() {
			acc.inspectTargetFile(target, rel)
			acc.inspectContextManifests(target, acc.languagesForTarget(rel))
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
		acc.inspectContextManifests(target, acc.languagesForTarget(rel))
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
	sample, readable := a.detectionSample(path)
	if readable && shouldSkipFile(rel, sample) {
		return
	}
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
	case "pyproject.toml":
		a.addLanguage("python", 0.86, rel)
		a.inspectPyProject(path, rel)
	case "requirements.txt":
		a.addLanguage("python", 0.86, rel)
		a.inspectPythonRequirements(path, rel)
	case "setup.py":
		a.addLanguage("python", 0.86, rel)
		a.inspectPythonSetup(path, rel)
	case "pipfile":
		a.addLanguage("python", 0.86, rel)
		a.inspectPipfile(path, rel)
	case "pom.xml":
		a.addLanguage("java", 0.86, rel)
		a.addLanguage("jvm", 0.8, rel)
		a.inspectPOM(path, rel)
	case "build.gradle", "build.gradle.kts":
		a.addLanguage("java", 0.86, rel)
		a.addLanguage("jvm", 0.8, rel)
		a.inspectGradle(path, rel)
	case "composer.json":
		a.addLanguage("php", 0.88, rel)
		a.inspectComposerJSON(path, rel)
	case "gemfile":
		a.addLanguage("ruby", 0.86, rel)
		a.inspectGemfile(path, rel)
	case "cargo.toml":
		a.addLanguage("rust", 0.9, rel)
	case "dockerfile", "docker-compose.yml", "docker-compose.yaml", "kustomization.yaml":
		a.addLanguage("config", 0.7, rel)
	}
	switch ext := strings.ToLower(filepath.Ext(name)); ext {
	case ".csproj", ".sln":
		a.addLanguage("csharp", 0.85, rel)
		a.addLanguage("dotnet", 0.8, rel)
		if ext == ".csproj" {
			a.inspectCSProj(path, rel)
		}
	case ".tf", ".yaml", ".yml", ".json":
		if strings.Contains(rel, ".github/workflows/") || strings.Contains(rel, "k8s") || strings.Contains(rel, "kubernetes") {
			a.addLanguage("config", 0.6, rel)
		}
	}
	if readable && !isContextManifest(lower) {
		a.inspectEnryLanguage(rel, sample)
	}
}

func (a *accumulator) inspectTargetFile(path, rel string) {
	a.inspectFile(path, rel)
}

func (a *accumulator) inspectContextManifests(target string, languages map[string]bool) {
	names := contextManifestNamesForLanguages(languages)
	globs := contextManifestGlobsForLanguages(languages)
	if len(names) == 0 && len(globs) == 0 {
		return
	}
	info, err := os.Stat(target)
	if err != nil {
		return
	}
	dir := target
	if !info.IsDir() {
		dir = filepath.Dir(target)
	}
	root, _ := filepath.Abs(a.root)
	if resolvedRoot, err := filepath.EvalSymlinks(root); err == nil {
		root = resolvedRoot
	}
	dir, _ = filepath.Abs(dir)
	if resolvedDir, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolvedDir
	}
	for {
		if rel, err := filepath.Rel(root, dir); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
			break
		}
		for _, name := range names {
			path := filepath.Join(dir, name)
			if fileInfo, err := os.Stat(path); err == nil && !fileInfo.IsDir() {
				rel, _ := filepath.Rel(root, path)
				a.inspectFile(path, rel)
			}
		}
		for _, glob := range globs {
			matches, err := filepath.Glob(filepath.Join(dir, glob))
			if err != nil {
				continue
			}
			for _, path := range matches {
				if fileInfo, err := os.Stat(path); err == nil && !fileInfo.IsDir() {
					rel, _ := filepath.Rel(root, path)
					a.inspectFile(path, rel)
				}
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

func (a *accumulator) languagesForTarget(rel string) map[string]bool {
	languages := map[string]bool{}
	for name, signal := range a.languages {
		for _, source := range signal.Sources {
			if sourceBelongsToTarget(source, rel) {
				languages[name] = true
				break
			}
		}
	}
	return languages
}

func sourceBelongsToTarget(source, targetRel string) bool {
	if idx := strings.Index(source, ":"); idx >= 0 {
		source = source[:idx]
	}
	source = filepath.Clean(source)
	targetRel = filepath.Clean(targetRel)
	if targetRel == "." {
		return true
	}
	if source == targetRel {
		return true
	}
	return strings.HasPrefix(source, targetRel+string(os.PathSeparator))
}

func contextManifestNamesForLanguages(languages map[string]bool) []string {
	names := []string{}
	seen := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			if seen[value] {
				continue
			}
			seen[value] = true
			names = append(names, value)
		}
	}
	if languages["javascript"] || languages["typescript"] {
		add("package.json", "tsconfig.json")
	}
	if languages["python"] {
		add("pyproject.toml", "requirements.txt", "setup.py", "Pipfile")
	}
	if languages["go"] {
		add("go.mod")
	}
	if languages["java"] || languages["jvm"] || languages["kotlin"] || languages["scala"] {
		add("pom.xml", "build.gradle", "build.gradle.kts")
	}
	if languages["php"] {
		add("composer.json")
	}
	if languages["ruby"] {
		add("Gemfile")
	}
	if languages["rust"] {
		add("Cargo.toml")
	}
	if languages["config"] || languages["dockerfile"] || languages["terraform"] || languages["yaml"] || languages["json"] {
		add("docker-compose.yml", "docker-compose.yaml", "kustomization.yaml")
	}
	return names
}

func contextManifestGlobsForLanguages(languages map[string]bool) []string {
	if languages["csharp"] || languages["dotnet"] {
		return []string{"*.csproj", "*.sln"}
	}
	return nil
}

func (a *accumulator) inspectPackageJSON(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var pkg struct {
		Dependencies         map[string]any `json:"dependencies"`
		DevDependencies      map[string]any `json:"devDependencies"`
		OptionalDependencies map[string]any `json:"optionalDependencies"`
		PeerDependencies     map[string]any `json:"peerDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	a.inspectPackageJSONDependencies(rel, "dependencies", pkg.Dependencies)
	a.inspectPackageJSONDependencies(rel, "devDependencies", pkg.DevDependencies)
	a.inspectPackageJSONDependencies(rel, "optionalDependencies", pkg.OptionalDependencies)
	a.inspectPackageJSONDependencies(rel, "peerDependencies", pkg.PeerDependencies)
}

func (a *accumulator) inspectPackageJSONDependencies(rel, section string, deps map[string]any) {
	for dep := range deps {
		if strings.EqualFold(dep, "typescript") {
			a.addLanguage("typescript", 0.9, rel+":"+section+".typescript")
		}
		a.addFrameworkForDependency("javascript", dep, 0.88, rel+":"+section+"."+dep)
	}
}

func (a *accumulator) inspectPyProject(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	if project, ok := asMap(doc["project"]); ok {
		for _, req := range asStringSlice(project["dependencies"]) {
			a.addPythonRequirement(req, 0.9, rel+":project.dependencies")
		}
		if optional, ok := asMap(project["optional-dependencies"]); ok {
			for group, values := range optional {
				for _, req := range asStringSlice(values) {
					a.addPythonRequirement(req, 0.88, rel+":project.optional-dependencies."+group)
				}
			}
		}
	}
	if tool, ok := asMap(doc["tool"]); ok {
		if poetry, ok := asMap(tool["poetry"]); ok {
			a.inspectPythonDependencyMap(rel, "tool.poetry.dependencies", asAnyMap(poetry["dependencies"]), 0.9)
			a.inspectPoetryGroups(rel, asAnyMap(poetry["group"]))
		}
		if pdm, ok := asMap(tool["pdm"]); ok {
			if devDeps, ok := asMap(pdm["dev-dependencies"]); ok {
				for group, values := range devDeps {
					for _, req := range asStringSlice(values) {
						a.addPythonRequirement(req, 0.86, rel+":tool.pdm.dev-dependencies."+group)
					}
				}
			}
		}
		if uv, ok := asMap(tool["uv"]); ok {
			if devDeps, ok := asMap(uv["dev-dependencies"]); ok {
				for group, values := range devDeps {
					for _, req := range asStringSlice(values) {
						a.addPythonRequirement(req, 0.86, rel+":tool.uv.dev-dependencies."+group)
					}
				}
			}
		}
	}
}

func (a *accumulator) inspectPoetryGroups(rel string, groups map[string]any) {
	for group, raw := range groups {
		groupMap, ok := asMap(raw)
		if !ok {
			continue
		}
		a.inspectPythonDependencyMap(rel, "tool.poetry.group."+group+".dependencies", asAnyMap(groupMap["dependencies"]), 0.86)
	}
}

func (a *accumulator) inspectPipfile(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	a.inspectPythonDependencyMap(rel, "packages", asAnyMap(doc["packages"]), 0.9)
	a.inspectPythonDependencyMap(rel, "dev-packages", asAnyMap(doc["dev-packages"]), 0.86)
}

func (a *accumulator) inspectPythonDependencyMap(rel, section string, deps map[string]any, confidence float64) {
	for dep := range deps {
		a.addFrameworkForDependency("python", dep, confidence, rel+":"+section+"."+dep)
	}
}

func (a *accumulator) inspectPythonRequirements(path, rel string) {
	file, err := os.Open(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if name, ok := pythonRequirementName(scanner.Text()); ok {
			a.addFrameworkForDependency("python", name, 0.86, rel+":requirement."+name)
		}
	}
	if err := scanner.Err(); err != nil {
		a.warnings = append(a.warnings, err.Error())
	}
}

func (a *accumulator) inspectPythonSetup(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	lower := strings.ToLower(string(data))
	if !strings.Contains(lower, "install_requires") && !strings.Contains(lower, "requires") {
		return
	}
	for _, match := range quotedRequirementPattern.FindAllStringSubmatch(string(data), -1) {
		if len(match) < 2 {
			continue
		}
		a.addPythonRequirement(match[1], 0.8, rel+":setup.py")
	}
}

func (a *accumulator) addPythonRequirement(requirement string, confidence float64, source string) {
	if name, ok := pythonRequirementName(requirement); ok {
		a.addFrameworkForDependency("python", name, confidence, source+"."+name)
	}
}

func (a *accumulator) inspectPOM(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var project struct {
		Dependencies []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
		} `xml:"dependencies>dependency"`
		ManagedDependencies []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
		} `xml:"dependencyManagement>dependencies>dependency"`
		Plugins []struct {
			GroupID    string `xml:"groupId"`
			ArtifactID string `xml:"artifactId"`
		} `xml:"build>plugins>plugin"`
	}
	if err := xml.Unmarshal(data, &project); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	for _, dep := range project.Dependencies {
		a.addJavaCoordinate(dep.GroupID, dep.ArtifactID, 0.9, rel+":maven.dependencies")
	}
	for _, dep := range project.ManagedDependencies {
		a.addJavaCoordinate(dep.GroupID, dep.ArtifactID, 0.86, rel+":maven.dependencyManagement")
	}
	for _, plugin := range project.Plugins {
		a.addJavaCoordinate(plugin.GroupID, plugin.ArtifactID, 0.84, rel+":maven.plugins")
	}
}

func (a *accumulator) inspectGradle(path, rel string) {
	file, err := os.Open(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripLineComment(scanner.Text())
		for _, match := range gradleCoordinatePattern.FindAllStringSubmatch(line, -1) {
			if len(match) >= 3 {
				a.addJavaCoordinate(match[1], match[2], 0.84, rel+":gradle.dependencies")
			}
		}
		for _, match := range gradlePluginPattern.FindAllStringSubmatch(line, -1) {
			if len(match) >= 2 {
				a.addFrameworkForDependency("java", match[1], 0.82, rel+":gradle.plugins."+match[1])
			}
		}
	}
	if err := scanner.Err(); err != nil {
		a.warnings = append(a.warnings, err.Error())
	}
}

func (a *accumulator) addJavaCoordinate(groupID, artifactID string, confidence float64, source string) {
	groupID = strings.TrimSpace(groupID)
	artifactID = strings.TrimSpace(artifactID)
	if groupID == "" && artifactID == "" {
		return
	}
	coordinate := groupID + ":" + artifactID
	a.addFrameworkForDependency("java", coordinate, confidence, source+"."+coordinate)
}

func (a *accumulator) inspectComposerJSON(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var composer struct {
		Require    map[string]any `json:"require"`
		RequireDev map[string]any `json:"require-dev"`
	}
	if err := json.Unmarshal(data, &composer); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	for dep := range composer.Require {
		a.addFrameworkForDependency("php", dep, 0.9, rel+":require."+dep)
	}
	for dep := range composer.RequireDev {
		a.addFrameworkForDependency("php", dep, 0.86, rel+":require-dev."+dep)
	}
}

func (a *accumulator) inspectGemfile(path, rel string) {
	file, err := os.Open(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripLineComment(scanner.Text())
		match := gemDeclarationPattern.FindStringSubmatch(line)
		if len(match) >= 2 {
			a.addFrameworkForDependency("ruby", match[1], 0.86, rel+":gem."+match[1])
		}
	}
	if err := scanner.Err(); err != nil {
		a.warnings = append(a.warnings, err.Error())
	}
}

func (a *accumulator) inspectCSProj(path, rel string) {
	data, err := os.ReadFile(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return
	}
	var project struct {
		PackageReferences []struct {
			Include string `xml:"Include,attr"`
			Update  string `xml:"Update,attr"`
		} `xml:"ItemGroup>PackageReference"`
		FrameworkReferences []struct {
			Include string `xml:"Include,attr"`
		} `xml:"ItemGroup>FrameworkReference"`
	}
	if err := xml.Unmarshal(data, &project); err != nil {
		a.warnings = append(a.warnings, "could not parse "+rel)
		return
	}
	for _, reference := range project.PackageReferences {
		dep := reference.Include
		if dep == "" {
			dep = reference.Update
		}
		a.addFrameworkForDependency("dotnet", dep, 0.86, rel+":packageReference."+dep)
	}
	for _, reference := range project.FrameworkReferences {
		a.addFrameworkForDependency("dotnet", reference.Include, 0.9, rel+":frameworkReference."+reference.Include)
	}
}

func (a *accumulator) addFrameworkForDependency(ecosystem, dependency string, confidence float64, source string) {
	if framework, ok := frameworkForDependency(ecosystem, dependency); ok {
		a.addFramework(framework, confidence, source)
	}
}

func frameworkForDependency(ecosystem, dependency string) (string, bool) {
	dependency = strings.ToLower(strings.TrimSpace(dependency))
	switch ecosystem {
	case "javascript":
		switch dependency {
		case "next":
			return "nextjs", true
		case "react":
			return "react", true
		case "vue":
			return "vue", true
		case "express":
			return "express", true
		case "nestjs", "@nestjs/core":
			return "nestjs", true
		case "angular", "@angular/core":
			return "angular", true
		case "svelte", "@sveltejs/kit":
			return "svelte", true
		case "nuxt":
			return "nuxt", true
		}
	case "python":
		switch normalizePythonPackageName(dependency) {
		case "django":
			return "django", true
		case "flask":
			return "flask", true
		case "fastapi":
			return "fastapi", true
		}
	case "java":
		if dependency == "org.springframework.boot" || strings.HasPrefix(dependency, "org.springframework:") || strings.Contains(dependency, ":spring-boot") {
			return "spring", true
		}
		if dependency == "io.quarkus" || strings.HasPrefix(dependency, "io.quarkus:") {
			return "quarkus", true
		}
		if dependency == "io.micronaut" || strings.HasPrefix(dependency, "io.micronaut:") {
			return "micronaut", true
		}
	case "php":
		switch dependency {
		case "laravel/framework", "laravel/lumen-framework":
			return "laravel", true
		case "symfony/symfony", "symfony/framework-bundle":
			return "symfony", true
		}
	case "ruby":
		switch dependency {
		case "rails", "railties":
			return "rails", true
		case "sinatra":
			return "sinatra", true
		case "hanami":
			return "hanami", true
		}
	case "dotnet":
		if dependency == "microsoft.aspnetcore.app" || strings.HasPrefix(dependency, "microsoft.aspnetcore.") {
			return "aspnetcore", true
		}
	}
	return "", false
}

func pythonRequirementName(requirement string) (string, bool) {
	line := strings.TrimSpace(requirement)
	if line == "" {
		return "", false
	}
	if idx := strings.Index(line, "#egg="); idx >= 0 {
		line = line[idx+len("#egg="):]
	} else {
		line = stripInlineComment(line)
	}
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "-") || strings.HasPrefix(line, "git+") || strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
		return "", false
	}
	cut := len(line)
	for _, token := range []string{"[", "==", ">=", "<=", "~=", "!=", ">", "<", "=", ";", " "} {
		if idx := strings.Index(line, token); idx >= 0 && idx < cut {
			cut = idx
		}
	}
	name := strings.TrimSpace(line[:cut])
	if name == "" {
		return "", false
	}
	for _, r := range name {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.') {
			return "", false
		}
	}
	return name, true
}

func normalizePythonPackageName(name string) string {
	return pythonNameSeparatorRegexp.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
}

func stripLineComment(line string) string {
	return stripInlineComment(line)
}

func stripInlineComment(line string) string {
	if idx := strings.Index(line, "#"); idx >= 0 {
		return line[:idx]
	}
	if idx := strings.Index(line, "//"); idx >= 0 {
		return line[:idx]
	}
	return line
}

func asMap(value any) (map[string]any, bool) {
	out, ok := value.(map[string]any)
	return out, ok
}

func asAnyMap(value any) map[string]any {
	if out, ok := asMap(value); ok {
		return out
	}
	return nil
}

func asStringSlice(value any) []string {
	raw, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if text, ok := item.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func (a *accumulator) detectionSample(path string) ([]byte, bool) {
	file, err := os.Open(path)
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return nil, false
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxDetectionBytes+1))
	if err != nil {
		a.warnings = append(a.warnings, err.Error())
		return nil, false
	}
	if len(data) > maxDetectionBytes {
		data = data[:maxDetectionBytes]
	}
	return data, true
}

func shouldSkipFile(rel string, sample []byte) bool {
	slashRel := filepath.ToSlash(rel)
	if enry.IsVendor(slashRel) || enry.IsBinary(sample) || enry.IsGenerated(slashRel, sample) {
		return true
	}
	return false
}

func (a *accumulator) inspectEnryLanguage(rel string, sample []byte) {
	if language, safe := enry.GetLanguageByShebang(sample); language != "" {
		confidence := 0.88
		if safe {
			confidence = 0.94
		}
		a.addEnryLanguage(language, confidence, rel+":enry.shebang")
	}
	language := enry.GetLanguage(filepath.ToSlash(rel), sample)
	if language == "" {
		return
	}
	confidence := 0.76
	if _, safe := enry.GetLanguageByFilename(filepath.ToSlash(rel)); safe {
		confidence = 0.86
	} else if _, safe := enry.GetLanguageByExtension(filepath.ToSlash(rel)); safe {
		confidence = 0.8
	}
	a.addEnryLanguage(language, confidence, rel+":enry."+languageKey(language))
}

func (a *accumulator) addEnryLanguage(language string, confidence float64, source string) {
	switch enry.GetLanguageType(language) {
	case enry.Unknown, enry.Prose:
		return
	}
	for _, name := range normalizeEnryLanguage(language) {
		a.addLanguage(name, confidence, source)
	}
}

func normalizeEnryLanguage(language string) []string {
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "javascript":
		return []string{"javascript"}
	case "typescript":
		return []string{"typescript"}
	case "python":
		return []string{"python"}
	case "go":
		return []string{"go"}
	case "java":
		return []string{"java", "jvm"}
	case "kotlin":
		return []string{"kotlin", "jvm"}
	case "scala":
		return []string{"scala", "jvm"}
	case "php":
		return []string{"php"}
	case "ruby":
		return []string{"ruby"}
	case "rust":
		return []string{"rust"}
	case "c":
		return []string{"c"}
	case "c++":
		return []string{"cpp"}
	case "c#":
		return []string{"csharp", "dotnet"}
	case "shell", "bash":
		return []string{"bash"}
	case "dockerfile":
		return []string{"dockerfile", "config"}
	case "hcl", "terraform":
		return []string{"terraform", "config"}
	case "yaml":
		return []string{"yaml"}
	case "json":
		return []string{"json"}
	case "html":
		return []string{"html"}
	case "xml":
		return []string{"xml"}
	case "swift":
		return []string{"swift"}
	case "elixir":
		return []string{"elixir"}
	case "clojure":
		return []string{"clojure"}
	case "ocaml":
		return []string{"ocaml"}
	case "protocol buffer":
		return []string{"protobuf"}
	default:
		key := languageKey(language)
		if key == "" {
			return nil
		}
		return []string{key}
	}
}

func languageKey(language string) string {
	var builder strings.Builder
	previousDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(language)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			previousDash = false
			continue
		}
		if builder.Len() > 0 && !previousDash {
			builder.WriteByte('-')
			previousDash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

func isContextManifest(lowerName string) bool {
	for _, name := range contextManifestNames() {
		if strings.ToLower(name) == lowerName {
			return true
		}
	}
	return false
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
