package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractTarGz(t *testing.T) {
	data := makeTarGz(t, map[string]string{"rules/example.yaml": "rules: []\n"})
	dest := t.TempDir()
	if err := ExtractTarGz(data, dest); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dest, "rules", "example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "rules: []\n" {
		t.Fatalf("unexpected file content: %q", string(content))
	}
}

func TestExtractTarGzRejectsTraversal(t *testing.T) {
	data := makeTarGz(t, map[string]string{"../escape.yaml": "bad"})
	if err := ExtractTarGz(data, t.TempDir()); err == nil {
		t.Fatal("expected path traversal error")
	}
}

func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzipWriter := gzip.NewWriter(&buf)
	tarWriter := tar.NewWriter(gzipWriter)
	for name, content := range files {
		if err := tarWriter.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tarWriter.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
