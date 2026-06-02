package detect

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
