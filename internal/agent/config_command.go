package agent

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/rules"
	"gopkg.in/yaml.v3"
)

func RunConfigCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: greprules agent-config inspect|set")
	}
	switch args[0] {
	case "inspect":
		return runConfigInspect(args[1:])
	case "set":
		return RunConfigSet(args[1:])
	default:
		return fmt.Errorf("unknown agent-config command: %s", args[0])
	}
}

func runConfigInspect(args []string) error {
	fs := flag.NewFlagSet("agent-config inspect", flag.ContinueOnError)
	format := fs.String("format", "text", "output format: text or json")
	rootFlag := fs.String("root", ".", "repo root or child path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := rules.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		return err
	}
	if *format == "json" {
		return cmdutil.PrintJSON(resolution)
	}
	fmt.Println("registry:", resolution.Config.Registry)
	fmt.Printf("opengrep: mode=%s version=%s includeDefaultRules=%t", resolution.Config.OpenGrep.Mode, resolution.Config.OpenGrep.Version, resolution.Config.OpenGrep.IncludeDefaultRules)
	if resolution.Config.OpenGrep.Path != "" {
		fmt.Printf(" path=%s", resolution.Config.OpenGrep.Path)
	}
	fmt.Println()
	for _, source := range resolution.Sources {
		if source.Path == "" {
			fmt.Printf("source %s loaded=%t\n", source.Scope, source.Loaded)
		} else {
			fmt.Printf("source %s loaded=%t path=%s\n", source.Scope, source.Loaded, source.Path)
		}
	}
	for _, warning := range resolution.Warnings {
		fmt.Println("warning:", warning)
	}
	return nil
}

func RunConfigSet(args []string) error {
	fs := flag.NewFlagSet("agent-config set", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	global := fs.Bool("global", false, "write user-level config")
	local := fs.Bool("local", false, "write repo-local config")
	repo := fs.Bool("repo", false, "write shared repo config")
	if err := fs.Parse(normalizeConfigSetArgs(args)); err != nil {
		return err
	}
	if fs.NArg() != 2 {
		return errors.New("usage: greprules agent-config set <key> <value> [--global|--local|--repo]")
	}
	scope := "global"
	if *local {
		scope = "local"
	}
	if *repo {
		scope = "repo"
	}
	if *global {
		scope = "global"
	}
	key := fs.Arg(0)
	value := fs.Arg(1)
	root, err := rules.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	var path string
	var format string
	var schemaVersion string
	switch scope {
	case "global":
		path, err = config.UserConfigPath()
		if err != nil {
			return err
		}
		format = "json"
		schemaVersion = config.UserConfigSchemaVersion
	case "local":
		path = config.LocalConfigPath(root)
		format = "json"
		schemaVersion = config.LocalConfigSchemaVersion
	case "repo":
		path = config.ConfigPath(root)
		format = "yaml"
		schemaVersion = config.ConfigSchemaVersion
	default:
		return fmt.Errorf("unsupported config scope: %s", scope)
	}
	if err := writeConfigPatch(path, format, schemaVersion, map[string]string{key: value}); err != nil {
		return err
	}
	fmt.Println("updated", path)
	return nil
}

func writeConfigPatch(path string, format string, schemaVersion string, values map[string]string) error {
	document, err := readConfigDocument(path, format)
	if err != nil {
		return err
	}
	if _, ok := document["schemaVersion"]; !ok {
		document["schemaVersion"] = schemaVersion
	}
	for key, raw := range values {
		value, err := parseConfigValue(key, raw)
		if err != nil {
			return err
		}
		if err := setConfigDocumentValue(document, key, value); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var data []byte
	switch format {
	case "json":
		data, err = json.MarshalIndent(document, "", "  ")
	case "yaml":
		data, err = yaml.Marshal(document)
	default:
		return fmt.Errorf("unsupported config format: %s", format)
	}
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func normalizeConfigSetArgs(args []string) []string {
	flags := []string{}
	values := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--global" || arg == "--local" || arg == "--repo":
			flags = append(flags, arg)
		case arg == "--root":
			flags = append(flags, arg)
			if i+1 < len(args) {
				flags = append(flags, args[i+1])
				i++
			}
		case strings.HasPrefix(arg, "--root="):
			flags = append(flags, arg)
		default:
			values = append(values, arg)
		}
	}
	return append(flags, values...)
}

func readConfigDocument(path string, format string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return map[string]any{}, nil
	}
	document := map[string]any{}
	switch format {
	case "json":
		err = json.Unmarshal(data, &document)
	case "yaml":
		err = yaml.Unmarshal(data, &document)
	default:
		err = fmt.Errorf("unsupported config format: %s", format)
	}
	if err != nil {
		return nil, err
	}
	if document == nil {
		document = map[string]any{}
	}
	return document, nil
}

func parseConfigValue(key string, raw string) (any, error) {
	switch key {
	case "languages", "frameworks", "packs":
		if strings.HasPrefix(strings.TrimSpace(raw), "[") {
			var values []string
			if err := json.Unmarshal([]byte(raw), &values); err != nil {
				return nil, err
			}
			return values, nil
		}
		value := strings.TrimSpace(raw)
		if value == "" {
			return []string{}, nil
		}
		return []string{value}, nil
	case "scan.changedDefault", "scan.sarif", "scan.agentJson", "opengrep.includeDefaultRules":
		return strconv.ParseBool(raw)
	}
	return raw, nil
}

func setConfigDocumentValue(document map[string]any, key string, value any) error {
	if !validConfigKey(key) {
		return fmt.Errorf("unsupported config key: %s", key)
	}
	parts := strings.Split(key, ".")
	current := document
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = map[string]any{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
	return nil
}

func validConfigKey(key string) bool {
	switch key {
	case "registry",
		"mode",
		"languages",
		"frameworks",
		"packs",
		"cacheDir",
		"outputDir",
		"scan.changedDefault",
		"scan.sarif",
		"scan.agentJson",
		"opengrep.includeDefaultRules",
		"opengrep.version":
		return true
	default:
		return false
	}
}
