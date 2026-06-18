package standalone

import (
	"context"
	"errors"
	"flag"
	"fmt"

	"github.com/greprules/greprules/internal/cmdutil"
	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/rules"
)

func RunSetupOpenGrep(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("setup-opengrep", flag.ContinueOnError)
	force := fs.Bool("force", false, "redownload even when installed")
	rootFlag := fs.String("root", ".", "repo root or child path for lockfile update")
	positionals, err := cmdutil.ParseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(positionals) > 1 {
		return errors.New("usage: greprules setup-opengrep [VERSION] [--force]")
	}
	version := "latest"
	if len(positionals) == 1 {
		version = positionals[0]
	}
	runtimeInfo, err := opengrep.Setup(ctx, opengrep.SetupOptions{Version: version, Force: *force})
	if err != nil {
		return err
	}
	fmt.Printf("installed OpenGrep %s at %s\n", runtimeInfo.Version, runtimeInfo.Path)
	root, rootErr := rules.FindRepoRoot(*rootFlag)
	if rootErr == nil {
		if lock, err := config.LoadLock(root); err == nil {
			lock.Engine = &config.LockedEngine{
				Name:            runtimeInfo.Name,
				Mode:            runtimeInfo.Mode,
				Version:         runtimeInfo.Version,
				Path:            runtimeInfo.Path,
				Source:          runtimeInfo.Source,
				SHA256:          runtimeInfo.SHA256,
				Managed:         true,
				SignaturePath:   runtimeInfo.SignaturePath,
				CertificatePath: runtimeInfo.CertificatePath,
				DownloadedAt:    runtimeInfo.DownloadedAt,
			}
			return config.SaveLock(root, lock)
		}
	}
	return nil
}
