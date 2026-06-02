package gitutil

import (
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func ChangedFiles(root string) ([]string, error) {
	seen := map[string]bool{}
	var firstErr error
	for _, args := range [][]string{
		{"diff", "--name-only", "--diff-filter=ACMR", "HEAD", "--"},
		{"diff", "--cached", "--name-only", "--diff-filter=ACMR", "--"},
		{"ls-files", "--others", "--exclude-standard"},
	} {
		out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			seen[filepath.Clean(line)] = true
		}
	}
	if len(seen) == 0 && firstErr != nil {
		return nil, firstErr
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files, nil
}
