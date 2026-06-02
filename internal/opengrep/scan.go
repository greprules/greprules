package opengrep

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

type ScanOptions struct {
	WorkingDir string
	Configs    []string
	Targets    []string
	OutputPath string
	Format     string
	Stdout     io.Writer
	Stderr     io.Writer
}

func RunScan(ctx context.Context, runtimeInfo Runtime, options ScanOptions) error {
	if runtimeInfo.Path == "" {
		return fmt.Errorf("OpenGrep runtime path is empty")
	}
	if _, err := os.Stat(runtimeInfo.Path); err != nil {
		return err
	}
	if len(options.Configs) == 0 {
		return fmt.Errorf("OpenGrep scan config list is empty")
	}
	args := []string{"scan"}
	for _, config := range options.Configs {
		if config == "" {
			return fmt.Errorf("OpenGrep scan config is empty")
		}
		args = append(args, "--config", config)
	}
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
	cmd.Stdout = options.Stdout
	if cmd.Stdout == nil {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = options.Stderr
	if cmd.Stderr == nil {
		cmd.Stderr = os.Stderr
	}
	return cmd.Run()
}

func ExitCode(err error) (int, bool) {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), true
	}
	return 0, false
}
