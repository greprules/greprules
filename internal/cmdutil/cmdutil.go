package cmdutil

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/greprules/greprules/internal/rules"
)

type boolFlag interface {
	IsBoolFlag() bool
}

func ParseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	flagArgs := []string{}
	positionals := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if !looksLikeFlag(arg) {
			positionals = append(positionals, arg)
			continue
		}
		flagArgs = append(flagArgs, arg)
		if strings.Contains(arg, "=") {
			continue
		}
		name := flagName(arg)
		if name == "" {
			continue
		}
		defined := fs.Lookup(name)
		if defined == nil || isBoolFlag(defined) {
			continue
		}
		if i+1 < len(args) {
			flagArgs = append(flagArgs, args[i+1])
			i++
		}
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positionals, nil
}

func HasFlag(args []string, name string) bool {
	prefix := "--" + name + "="
	for _, arg := range args {
		if arg == "--"+name || strings.HasPrefix(arg, prefix) {
			return true
		}
	}
	return false
}

func looksLikeFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") && arg != "-"
}

func flagName(arg string) string {
	name := strings.TrimLeft(arg, "-")
	name, _, _ = strings.Cut(name, "=")
	return name
}

func isBoolFlag(flagDef *flag.Flag) bool {
	value, ok := flagDef.Value.(boolFlag)
	return ok && value.IsBoolFlag()
}

func ResolveCommandRoot(rootFlag string, discoverGitRoot bool) (string, error) {
	if discoverGitRoot {
		return rules.FindRepoRoot(rootFlag)
	}
	if rootFlag == "" {
		rootFlag = "."
	}
	return filepath.Abs(rootFlag)
}

func MaybePromoteSingleExternalTargetToRoot(root string, targets []string, targetsFrom string, rootExplicit bool, changed bool) (string, []string, error) {
	if rootExplicit || changed || targetsFrom != "" || len(targets) != 1 {
		return root, targets, nil
	}
	target := strings.TrimSpace(targets[0])
	if target == "" {
		return root, targets, nil
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return "", nil, err
	}
	if rel, err := filepath.Rel(absRoot, absTarget); err == nil && !isOutsideRoot(rel) {
		if rel == "." {
			return absRoot, nil, nil
		}
		return absRoot, targets, nil
	}
	info, err := os.Stat(absTarget)
	if err != nil {
		return absRoot, targets, nil
	}
	if info.IsDir() {
		return absTarget, nil, nil
	}
	parent := filepath.Dir(absTarget)
	if repoRoot, err := rules.FindRepoRoot(parent); err == nil {
		rel, err := filepath.Rel(repoRoot, absTarget)
		if err != nil {
			return "", nil, err
		}
		return repoRoot, []string{rel}, nil
	}
	return parent, []string{filepath.Base(absTarget)}, nil
}

func isOutsideRoot(rel string) bool {
	return rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel)
}

var GreprulesGitignoreEntries = []string{
	".greprules/out/",
	".greprules/plugin-data/",
	".greprules/config.local.json",
}

func EnsureGreprulesGitignore(root string) error {
	if !isGitWorkTree(root) {
		return nil
	}
	path := filepath.Join(root, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	missing := missingGreprulesGitignoreEntries(data)
	if len(missing) == 0 {
		return nil
	}
	var builder strings.Builder
	if len(data) > 0 {
		builder.Write(data)
		if !strings.HasSuffix(string(data), "\n") {
			builder.WriteByte('\n')
		}
	}
	for _, entry := range missing {
		builder.WriteString(entry)
		builder.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(builder.String()), 0o644)
}

func isGitWorkTree(root string) bool {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--is-inside-work-tree").Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

func missingGreprulesGitignoreEntries(data []byte) []string {
	lines := GitignoreEffectiveLines(data)
	if lines[".greprules"] || lines[".greprules/"] || lines[".greprules/**"] {
		return nil
	}
	missing := []string{}
	for _, entry := range GreprulesGitignoreEntries {
		if gitignoreEntryExists(lines, entry) {
			continue
		}
		missing = append(missing, entry)
	}
	return missing
}

func GitignoreEffectiveLines(data []byte) map[string]bool {
	lines := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines[line] = true
	}
	return lines
}

func gitignoreEntryExists(lines map[string]bool, entry string) bool {
	if lines[entry] {
		return true
	}
	if strings.HasSuffix(entry, "/") && lines[strings.TrimSuffix(entry, "/")] {
		return true
	}
	return false
}

func PrintJSON(value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
