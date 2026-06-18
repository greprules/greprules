package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectTypeScriptNextProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{
  "dependencies": {
    "next": "15.0.0",
    "react": "19.0.0"
  },
  "devDependencies": {
    "typescript": "5.0.0"
  }
}`)
	writeFile(t, filepath.Join(root, "tsconfig.json"), `{}`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "typescript") {
		t.Fatalf("expected typescript language, got %#v", result.Languages)
	}
	if !hasSignal(result.Frameworks, "nextjs") {
		t.Fatalf("expected nextjs framework, got %#v", result.Frameworks)
	}
}

func TestDetectPythonWebProject(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "requirements.txt"), "fastapi==0.1.0\n")

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "python") {
		t.Fatalf("expected python language, got %#v", result.Languages)
	}
	if !hasSignal(result.Frameworks, "fastapi") {
		t.Fatalf("expected fastapi framework, got %#v", result.Frameworks)
	}
}

func TestDetectPythonManifestDoesNotUseSubstringFrameworkMatch(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), `[project]
name = "fastapi-helper"
dependencies = ["requests"]
`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if hasSignal(result.Frameworks, "fastapi") {
		t.Fatalf("expected project metadata substring not to detect fastapi, got %#v", result.Frameworks)
	}
}

func TestDetectPythonManifestParsesDependencySections(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pyproject.toml"), `[project]
dependencies = ["Django>=5"]

[project.optional-dependencies]
api = ["fastapi[standard]>=0.110"]

[tool.poetry.group.dev.dependencies]
Flask = "^3.0"
`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, framework := range []string{"django", "fastapi", "flask"} {
		if !hasSignal(result.Frameworks, framework) {
			t.Fatalf("expected %s framework, got %#v", framework, result.Frameworks)
		}
	}
}

func TestDetectRequirementsParsesPackageNamesOnly(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "requirements.txt"), "not-fastapi==1.0.0\nFlask==3.0.0\n")

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if hasSignal(result.Frameworks, "fastapi") {
		t.Fatalf("expected not-fastapi not to detect fastapi, got %#v", result.Frameworks)
	}
	if !hasSignal(result.Frameworks, "flask") {
		t.Fatalf("expected flask framework, got %#v", result.Frameworks)
	}
}

func TestDetectTargetsUsesNearbyManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "api", "pyproject.toml"), "[project]\ndependencies = [\"fastapi\"]\n")
	writeFile(t, filepath.Join(root, "packages", "api", "src", "main.py"), "from fastapi import FastAPI\napp = FastAPI()\n")

	result, err := DetectTargets(root, []string{filepath.Join("packages", "api", "src", "main.py")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Targets, []string{filepath.Join("packages", "api", "src", "main.py")}; len(got) != 1 || got[0] != want[0] {
		t.Fatalf("targets = %#v, want %#v", got, want)
	}
	if !hasSignal(result.Languages, "python") {
		t.Fatalf("expected python language, got %#v", result.Languages)
	}
	if !hasSignal(result.Frameworks, "fastapi") {
		t.Fatalf("expected fastapi framework, got %#v", result.Frameworks)
	}
}

func TestDetectTargetsIgnoresSourceFrameworkImportsWithoutManifest(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "packages", "api", "src", "main.py"), "from fastapi import FastAPI\napp = FastAPI()\n")

	result, err := DetectTargets(root, []string{filepath.Join("packages", "api", "src", "main.py")})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "python") {
		t.Fatalf("expected python language from enry, got %#v", result.Languages)
	}
	if hasSignal(result.Frameworks, "fastapi") {
		t.Fatalf("expected no source-import framework detection, got %#v", result.Frameworks)
	}
}

func TestDetectTargetsUsesLanguageScopedContextManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{
  "dependencies": {
    "next": "15.0.0"
  }
}`)
	writeFile(t, filepath.Join(root, "packages", "api", "src", "main.py"), "print('hello')\n")

	result, err := DetectTargets(root, []string{filepath.Join("packages", "api", "src", "main.py")})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "python") {
		t.Fatalf("expected python language from target, got %#v", result.Languages)
	}
	if hasSignal(result.Frameworks, "nextjs") {
		t.Fatalf("expected unrelated package.json to be ignored for python target, got %#v", result.Frameworks)
	}
}

func TestDetectTargetsNormalizesTSXForContextManifests(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "package.json"), `{
  "dependencies": {
    "next": "15.0.0",
    "react": "19.0.0"
  },
  "devDependencies": {
    "typescript": "5.0.0"
  }
}`)
	writeFile(t, filepath.Join(root, "app", "page.tsx"), "export default function Page(): JSX.Element { return <main>Hello</main> }\n")

	result, err := DetectTargets(root, []string{filepath.Join("app", "page.tsx")})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "typescript") {
		t.Fatalf("expected tsx target to normalize to typescript, got %#v", result.Languages)
	}
	if !hasSignal(result.Frameworks, "nextjs") {
		t.Fatalf("expected nextjs framework from package.json, got %#v", result.Frameworks)
	}
	if !hasSignal(result.Frameworks, "react") {
		t.Fatalf("expected react framework from package.json, got %#v", result.Frameworks)
	}
}

func TestDetectJavaPomFrameworkDependencies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pom.xml"), `<project>
  <dependencies>
    <dependency>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-starter-web</artifactId>
    </dependency>
  </dependencies>
</project>`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Frameworks, "spring") {
		t.Fatalf("expected spring framework, got %#v", result.Frameworks)
	}
}

func TestDetectGradleFrameworkDependencies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "build.gradle.kts"), `plugins {
    id("org.springframework.boot") version "3.4.0"
}
dependencies {
    implementation("io.quarkus:quarkus-resteasy-reactive:3.0.0")
}`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Frameworks, "spring") {
		t.Fatalf("expected spring framework, got %#v", result.Frameworks)
	}
	if !hasSignal(result.Frameworks, "quarkus") {
		t.Fatalf("expected quarkus framework, got %#v", result.Frameworks)
	}
}

func TestDetectComposerFrameworkDependencies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "composer.json"), `{
  "require": {
    "laravel/framework": "^11.0",
    "symfony/framework-bundle": "^7.0"
  }
}`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Frameworks, "laravel") {
		t.Fatalf("expected laravel framework, got %#v", result.Frameworks)
	}
	if !hasSignal(result.Frameworks, "symfony") {
		t.Fatalf("expected symfony framework, got %#v", result.Frameworks)
	}
}

func TestDetectGemfileFrameworkDependencies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Gemfile"), `source "https://rubygems.org"
gem "rails", "~> 7.2"
`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Frameworks, "rails") {
		t.Fatalf("expected rails framework, got %#v", result.Frameworks)
	}
}

func TestDetectDotnetFrameworkReference(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "App.csproj"), `<Project Sdk="Microsoft.NET.Sdk.Web">
  <ItemGroup>
    <FrameworkReference Include="Microsoft.AspNetCore.App" />
  </ItemGroup>
</Project>`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Frameworks, "aspnetcore") {
		t.Fatalf("expected aspnetcore framework, got %#v", result.Frameworks)
	}
}

func TestDetectTargetsUsesEnryShebangForExtensionlessScript(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "scripts", "deploy"), "#!/usr/bin/env bash\necho deploy\n")

	result, err := DetectTargets(root, []string{filepath.Join("scripts", "deploy")})
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "bash") {
		t.Fatalf("expected bash language from shebang, got %#v", result.Languages)
	}
}

func TestDetectUsesEnryForAdditionalLanguages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "Sources", "App.swift"), "import Foundation\nlet message = \"hello\"\nprint(message)\n")

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "swift") {
		t.Fatalf("expected swift language from enry, got %#v", result.Languages)
	}
}

func TestDetectDoesNotUseExtensionFallbackForSourceLanguages(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "main", "kotlin", "App.kt"), "package example\n\nfun main() {\n    println(\"hello\")\n}\n")

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "kotlin") {
		t.Fatalf("expected kotlin language from enry, got %#v", result.Languages)
	}
	if !hasSignal(result.Languages, "jvm") {
		t.Fatalf("expected jvm language from enry normalization, got %#v", result.Languages)
	}
	if hasSignal(result.Languages, "java") {
		t.Fatalf("expected no java source-extension fallback, got %#v", result.Languages)
	}
}

func TestDetectKeepsDotnetProjectFileSignal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "src", "App.csproj"), `<Project Sdk="Microsoft.NET.Sdk"></Project>`)

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasSignal(result.Languages, "csharp") {
		t.Fatalf("expected csharp language from project file, got %#v", result.Languages)
	}
	if !hasSignal(result.Languages, "dotnet") {
		t.Fatalf("expected dotnet language from project file, got %#v", result.Languages)
	}
}

func TestDetectSkipsGeneratedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "generated.pb.go"), "// Code generated by protoc-gen-go. DO NOT EDIT.\npackage example\n")

	result, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if hasSignal(result.Languages, "go") {
		t.Fatalf("expected generated file to be skipped, got %#v", result.Languages)
	}
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasSignal(signals []Signal, name string) bool {
	for _, signal := range signals {
		if signal.Name == name {
			return true
		}
	}
	return false
}
