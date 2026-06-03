package runtimeconfig

import (
	"errors"
	"os"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
)

func LoadOrDefaultConfig(root string) (config.Config, error) {
	resolution, err := config.LoadEffectiveConfig(root)
	if err == nil {
		return resolution.Config, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return config.DefaultConfig(), nil
	}
	return config.Config{}, err
}

func FromConfigOrLock(lock config.Lock, cfg config.Config, modeOverride string, pathOverride string, versionOverride string) (opengrep.Runtime, error) {
	mode := cfg.OpenGrep.Mode
	path := cfg.OpenGrep.Path
	version := cfg.OpenGrep.Version
	if modeOverride != "" {
		mode = modeOverride
	}
	if pathOverride != "" {
		path = pathOverride
		if modeOverride == "" {
			mode = "path"
		}
	}
	if versionOverride != "" {
		version = versionOverride
	}
	if mode == "" {
		mode = "managed"
	}
	if mode == "managed" && lock.Engine != nil && lock.Engine.Path != "" && (lock.Engine.Managed || lock.Engine.Mode == "managed") {
		if _, err := os.Stat(lock.Engine.Path); err == nil {
			return opengrep.Runtime{
				Name:            lock.Engine.Name,
				Mode:            firstNonEmpty(lock.Engine.Mode, "managed"),
				Version:         lock.Engine.Version,
				Path:            lock.Engine.Path,
				Source:          lock.Engine.Source,
				SHA256:          lock.Engine.SHA256,
				Managed:         lock.Engine.Managed,
				SignaturePath:   lock.Engine.SignaturePath,
				CertificatePath: lock.Engine.CertificatePath,
				DownloadedAt:    lock.Engine.DownloadedAt,
			}, nil
		}
	}
	return opengrep.Resolve(opengrep.ResolveOptions{
		Mode:    mode,
		Path:    path,
		Version: version,
	})
}

func LockedEngineFromRuntime(runtimeInfo opengrep.Runtime) *config.LockedEngine {
	return &config.LockedEngine{
		Name:            runtimeInfo.Name,
		Mode:            runtimeInfo.Mode,
		Version:         runtimeInfo.Version,
		Path:            runtimeInfo.Path,
		Source:          runtimeInfo.Source,
		SHA256:          runtimeInfo.SHA256,
		Managed:         runtimeInfo.Managed,
		SignaturePath:   runtimeInfo.SignaturePath,
		CertificatePath: runtimeInfo.CertificatePath,
		DownloadedAt:    runtimeInfo.DownloadedAt,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
