package agent

import (
	"context"
	"flag"
	"os"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/doctor"
	"github.com/greprules/greprules/internal/rules"
)

func RunStatusCommand(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("agent-status", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	debug := fs.Bool("debug", false, "print debug details")
	format := fs.String("format", "text", "output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := rules.FindRepoRoot(*rootFlag)
	if err != nil {
		return err
	}
	report, err := doctor.Build(ctx, root, doctor.Options{
		Debug: *debug,
	})
	if err != nil {
		return err
	}
	if *format == "json" {
		return cmdutil.PrintJSON(report)
	}
	doctor.PrintText(os.Stdout, report, *debug)
	return nil
}
