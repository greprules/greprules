package standalone

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/greprules/greprules/internal/config"
	"github.com/greprules/greprules/internal/opengrep"
	"github.com/greprules/greprules/internal/rules"
)

func RunCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	rootFlag := fs.String("root", ".", "repo root or child path")
	configFlag := fs.Bool("config", false, "remove user-level greprules config")
	cacheFlag := fs.Bool("cache", false, "remove all user-level greprules caches")
	opengrepFlag := fs.Bool("opengrep", false, "remove managed OpenGrep cache")
	pluginCacheFlag := fs.Bool("plugin-cache", false, "remove agent plugin CLI bootstrap caches")
	repoFlag := fs.Bool("repo", false, "remove current project state and repo-local .greprules directory")
	allFlag := fs.Bool("all", false, "remove user config, user caches, current project state, and repo-local .greprules")
	purgeFlag := fs.Bool("purge", false, "remove user config and user caches")
	dryRun := fs.Bool("dry-run", false, "print cleanup targets without deleting")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *allFlag {
		*configFlag = true
		*cacheFlag = true
		*repoFlag = true
	}
	if *purgeFlag {
		*configFlag = true
		*cacheFlag = true
	}

	targets, err := cleanupTargets(*rootFlag, *configFlag, *cacheFlag, *opengrepFlag, *pluginCacheFlag, *repoFlag)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		fmt.Println("no cleanup target selected")
		fmt.Println("use one or more of: --config, --cache, --opengrep, --plugin-cache, --repo, --purge, --all")
		return nil
	}
	for _, target := range targets {
		if *dryRun {
			fmt.Println("would remove", target)
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			return err
		}
		fmt.Println("removed", target)
	}
	return nil
}

func cleanupTargets(rootFlag string, removeConfig bool, removeCache bool, removeOpenGrep bool, removePluginCache bool, removeRepo bool) ([]string, error) {
	seen := map[string]bool{}
	targets := []string{}
	add := func(path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if !seen[clean] {
			seen[clean] = true
			targets = append(targets, clean)
		}
	}

	if removeConfig {
		path, err := config.UserConfigPath()
		if err != nil {
			return nil, err
		}
		add(path)
	}
	if removeCache {
		root, err := userCacheRoot()
		if err != nil {
			return nil, err
		}
		add(root)
	} else {
		if removeOpenGrep {
			root, err := opengrep.DefaultCacheRoot()
			if err != nil {
				return nil, err
			}
			add(root)
		}
		if removePluginCache {
			root, err := PluginCacheRoot()
			if err != nil {
				return nil, err
			}
			add(root)
		}
	}
	if removeRepo {
		root, err := rules.FindRepoRoot(rootFlag)
		if err != nil {
			return nil, err
		}
		stateDir, err := config.ProjectStateDir(root)
		if err != nil {
			return nil, err
		}
		add(stateDir)
		add(filepath.Join(root, ".greprules"))
	}
	sort.Strings(targets)
	return targets, nil
}

func userCacheRoot() (string, error) {
	return config.UserCacheRoot()
}

func PluginCacheRoot() (string, error) {
	root, err := userCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "plugins"), nil
}
