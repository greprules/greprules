package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "greprules-cli-test-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("GREPRULES_STATE_HOME", filepath.Join(root, "state"))
	_ = os.Setenv("GREPRULES_CACHE_HOME", filepath.Join(root, "cache"))
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}
