package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/greprules/greprules/internal/agent"
	"github.com/greprules/greprules/internal/auth"
	"github.com/greprules/greprules/internal/standalone"
)

func Execute(args []string, version string) int {
	if len(args) == 0 {
		printUsage()
		return 0
	}
	ctx := context.Background()
	var err error
	switch args[0] {
	case "version", "--version", "-v":
		fmt.Println(version)
	case "help", "--help", "-h":
		printUsage()
	case "fetch":
		err = standalone.RunFetch(ctx, args[1:])
	case "auth":
		err = auth.RunCommand(ctx, args[1:])
	case "setup-opengrep":
		err = standalone.RunSetupOpenGrep(ctx, args[1:])
	case "scan":
		err = standalone.RunScan(ctx, args[1:])
	case "agent-scan":
		err = agent.RunScanCommand(ctx, args[1:])
	case "agent-config":
		err = agent.RunConfigCommand(args[1:])
	case "agent-status":
		err = agent.RunStatusCommand(ctx, args[1:])
	case "agent-feedback":
		err = agent.RunFeedbackCommand(ctx, args[1:], version)
	case "agent-proposal":
		err = agent.RunProposalCommand(ctx, args[1:], version)
	case "cleanup":
		err = standalone.RunCleanup(args[1:])
	default:
		err = fmt.Errorf("unknown command: %s", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func printUsage() {
	fmt.Println(`greprules is a managed OpenGrep rule-pack scanner.

Usage:
  greprules fetch <PACK> [PACK...]
  greprules auth login|status|logout [--agent]
  greprules setup-opengrep [VERSION] [--force]
  greprules scan [PATH_OR_OPENGREP_ARGS...] [--root PATH] [--changed] [--verbose] [--no-prepare] [-- RAW_OPENGREP_ARGS...]
  greprules cleanup [--config|--cache|--opengrep|--plugin-cache|--repo|--all] [--dry-run]`)
}
