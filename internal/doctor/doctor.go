package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/rules"
)

type Options struct {
	Debug           bool
	EngineMode      string
	OpenGrepPath    string
	OpenGrepVersion string
}

type Report struct {
	SchemaVersion       string                  `json:"schemaVersion"`
	Status              string                  `json:"status"`
	Root                string                  `json:"root"`
	Config              config.ConfigResolution `json:"config"`
	Registry            CheckStatus             `json:"registry"`
	Lock                LockStatus              `json:"lock"`
	OpenGrep            OpenGrepStatus          `json:"opengrep"`
	RecommendedCommands []string                `json:"recommendedCommands,omitempty"`
	Warnings            []string                `json:"warnings,omitempty"`
}

type CheckStatus struct {
	OK    bool   `json:"ok"`
	URL   string `json:"url,omitempty"`
	Error string `json:"error,omitempty"`
}

type LockStatus struct {
	Exists    bool         `json:"exists"`
	Path      string       `json:"path"`
	PackCount int          `json:"packCount,omitempty"`
	Error     string       `json:"error,omitempty"`
	Message   string       `json:"message,omitempty"`
	Value     *config.Lock `json:"value,omitempty"`
}

type OpenGrepStatus struct {
	Managed RuntimeCheck `json:"managed"`
	System  RuntimeCheck `json:"system"`
	Active  RuntimeCheck `json:"active"`
}

type RuntimeCheck struct {
	OK      bool              `json:"ok"`
	Runtime *opengrep.Runtime `json:"runtime,omitempty"`
	Error   string            `json:"error,omitempty"`
}

func Build(ctx context.Context, root string, options Options) (Report, error) {
	resolution, err := config.LoadEffectiveConfig(root)
	if err != nil {
		return Report{}, err
	}
	cfg := resolution.Config
	report := Report{
		SchemaVersion:       "greprules.status.v1",
		Root:                root,
		Config:              resolution,
		RecommendedCommands: []string{},
		Warnings:            append([]string{}, resolution.Warnings...),
	}
	if _, err := rules.NewRegistry(cfg.Registry).ListPacks(ctx); err != nil {
		report.Registry = CheckStatus{OK: false, Error: err.Error(), URL: cfg.Registry}
		report.RecommendedCommands = append(report.RecommendedCommands, "check registry URL or run with --registry")
	} else {
		report.Registry = CheckStatus{OK: true, URL: cfg.Registry}
	}
	lockPath, lockPathErr := config.LockPath(root)
	if lockPathErr != nil {
		report.Lock = LockStatus{
			Exists:  false,
			Error:   lockPathErr.Error(),
			Message: "rule-pack lock path could not be resolved",
		}
		report.RecommendedCommands = append(report.RecommendedCommands, "check user state directory permissions")
	} else if lock, err := config.LoadLock(root); err != nil {
		report.Lock = LockStatus{
			Exists:  false,
			Path:    lockPath,
			Message: "rule packs are not fetched yet; scan commands can fetch them automatically when the registry is reachable",
		}
		if !errors.Is(err, os.ErrNotExist) {
			report.Lock.Error = err.Error()
			report.Lock.Message = "rule-pack lockfile could not be read; run greprules scan to refresh selected rule packs or greprules fetch <slug> for explicit packs"
			report.RecommendedCommands = append(report.RecommendedCommands, "greprules scan", "greprules fetch <slug>")
		}
	} else {
		report.Lock = LockStatus{Exists: true, Path: lockPath, PackCount: len(lock.Packs)}
		if missing := config.MissingLockRulePaths(root, lock); len(missing) > 0 {
			report.Lock.Error = "locked rule pack artifacts are missing: " + strings.Join(missing, ", ")
			report.Lock.Message = "rule-pack artifacts are missing from user cache; run greprules scan to refresh selected rule packs or greprules fetch <slug> for explicit packs"
			report.RecommendedCommands = append(report.RecommendedCommands, "greprules scan", "greprules fetch <slug>")
		}
		if options.Debug {
			report.Lock.Value = &lock
		}
	}
	if runtimeInfo, err := opengrep.Installed(cfg.OpenGrep.Version, ""); err != nil {
		report.OpenGrep.Managed = RuntimeCheck{OK: false, Error: err.Error()}
	} else {
		report.OpenGrep.Managed = RuntimeCheck{OK: true, Runtime: &runtimeInfo}
	}
	if runtimeInfo, err := opengrep.Resolve(opengrep.ResolveOptions{Mode: "system"}); err != nil {
		report.OpenGrep.System = RuntimeCheck{OK: false, Error: err.Error()}
	} else {
		report.OpenGrep.System = RuntimeCheck{OK: true, Runtime: &runtimeInfo}
	}
	if runtimeInfo, err := opengrep.ResolveFromConfig(config.Lock{}, cfg, opengrep.ConfigOverrides{
		Mode:    options.EngineMode,
		Path:    options.OpenGrepPath,
		Version: options.OpenGrepVersion,
	}); err != nil {
		report.OpenGrep.Active = RuntimeCheck{OK: false, Error: err.Error()}
	} else {
		report.OpenGrep.Active = RuntimeCheck{OK: true, Runtime: &runtimeInfo}
	}
	AddOpenGrepRecommendations(&report, cfg)
	report.Status = "ok"
	if !report.Registry.OK || report.Lock.Error != "" || !report.OpenGrep.Active.OK {
		report.Status = "needs_attention"
	}
	return report, nil
}

func AddOpenGrepRecommendations(report *Report, cfg config.Config) {
	if report.OpenGrep.Active.OK {
		return
	}

	switch cfg.OpenGrep.Mode {
	case "managed":
		addRecommendedCommand(report, "greprules setup-opengrep")
	case "system":
		addRecommendedCommand(report, "greprules setup-opengrep")
	case "path":
		addRecommendedCommand(report, "greprules setup-opengrep")
	default:
		addRecommendedCommand(report, "greprules agent-status --format json")
	}
}

func addRecommendedCommand(report *Report, command string) {
	for _, existing := range report.RecommendedCommands {
		if existing == command {
			return
		}
	}
	report.RecommendedCommands = append(report.RecommendedCommands, command)
}

func PrintText(writer io.Writer, report Report, debug bool) {
	fmt.Fprintln(writer, "repo:", report.Root)
	for _, source := range report.Config.Sources {
		if source.Path == "" {
			fmt.Fprintf(writer, "config %s: loaded=%t\n", source.Scope, source.Loaded)
		} else {
			fmt.Fprintf(writer, "config %s: loaded=%t path=%s\n", source.Scope, source.Loaded, source.Path)
		}
	}
	cfg := report.Config.Config
	fmt.Fprintf(writer, "opengrep config: mode=%s version=%s", cfg.OpenGrep.Mode, cfg.OpenGrep.Version)
	if cfg.OpenGrep.Path != "" {
		fmt.Fprintf(writer, " path=%s", cfg.OpenGrep.Path)
	}
	fmt.Fprintln(writer)
	if report.Registry.OK {
		fmt.Fprintln(writer, "registry: ok")
	} else {
		fmt.Fprintln(writer, "registry:", report.Registry.Error)
	}
	if report.Lock.Exists {
		if report.Lock.Error != "" {
			if report.Lock.Message != "" {
				fmt.Fprintln(writer, "rule packs:", report.Lock.Message)
			} else {
				fmt.Fprintln(writer, "rule packs:", report.Lock.Error)
			}
		} else {
			fmt.Fprintf(writer, "rule packs: %d fetched pack(s)\n", report.Lock.PackCount)
		}
		if debug && report.Lock.Value != nil {
			printJSON(writer, report.Lock.Value)
		}
	} else {
		if report.Lock.Message != "" {
			fmt.Fprintln(writer, "rule packs:", report.Lock.Message)
		} else {
			fmt.Fprintln(writer, "rule packs: not fetched yet")
		}
	}
	printRuntimeText(writer, "opengrep managed", report.OpenGrep.Managed)
	printRuntimeText(writer, "opengrep system", report.OpenGrep.System)
	printRuntimeText(writer, "opengrep active", report.OpenGrep.Active)
	for _, warning := range report.Warnings {
		fmt.Fprintln(writer, "warning:", warning)
	}
	for _, command := range report.RecommendedCommands {
		fmt.Fprintln(writer, "recommended:", command)
	}
}

func printRuntimeText(writer io.Writer, label string, check RuntimeCheck) {
	if !check.OK {
		fmt.Fprintln(writer, label+":", "missing")
		return
	}
	fmt.Fprintf(writer, "%s: mode=%s version=%s path=%s\n", label, check.Runtime.Mode, check.Runtime.Version, check.Runtime.Path)
}

func printJSON(writer io.Writer, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return
	}
	fmt.Fprintln(writer, string(data))
}
