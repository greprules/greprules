package opengrep

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

type ScanOptions struct {
	WorkingDir string
	RulePath   string
	Targets    []string
	OutputPath string
	Format     string
}

func RunScan(ctx context.Context, runtimeInfo Runtime, options ScanOptions) error {
	if runtimeInfo.Path == "" {
		return fmt.Errorf("OpenGrep runtime path is empty")
	}
	if _, err := os.Stat(runtimeInfo.Path); err != nil {
		return err
	}
	args := []string{"scan", "--config", options.RulePath}
	switch options.Format {
	case "json":
		args = append(args, "--json")
	case "sarif":
		args = append(args, "--sarif")
	default:
		return fmt.Errorf("unsupported OpenGrep output format: %s", options.Format)
	}
	if options.OutputPath != "" {
		args = append(args, "--output", options.OutputPath)
	}
	args = append(args, options.Targets...)
	cmd := exec.CommandContext(ctx, runtimeInfo.Path, args...)
	cmd.Dir = options.WorkingDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
