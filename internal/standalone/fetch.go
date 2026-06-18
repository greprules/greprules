package standalone

import (
	"context"
	"errors"
	"flag"
	"os"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/rules"
)

func RunFetch(ctx context.Context, args []string) error {
	return RunFetchWithOptions(ctx, args, rules.FetchOptions{Stdout: os.Stdout})
}

func RunFetchWithOptions(ctx context.Context, args []string, options rules.FetchOptions) error {
	fs := flag.NewFlagSet("fetch", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	packIDs, err := cmdutil.ParseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(packIDs) == 0 {
		return errors.New("usage: greprules fetch <PACK> [PACK...] [--root PATH]")
	}
	root, err := cmdutil.ResolveCommandRoot(*rootFlag, false)
	if err != nil {
		return err
	}
	cfg, err := config.LoadEffectiveOrDefault(root)
	if err != nil {
		return err
	}
	client := rules.NewRegistry(cfg.Registry)
	return rules.FetchAndLock(ctx, root, cfg, client, packIDs, options)
}
