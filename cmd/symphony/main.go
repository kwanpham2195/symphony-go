// Command symphony is the CLI entry point for the Go Symphony service.
//
// Usage:
//
//	symphony [flags] [workflow-path]
//
// Flags:
//
//	--validate-only  Load and validate the workflow, then exit.
//	--port PORT      Enable HTTP server on PORT (overrides server.port in workflow).
package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/matthew-opn/symphony-go/internal/config"
	"github.com/matthew-opn/symphony-go/internal/workflow"
)

func main() {
	args := parseArgs(os.Args[1:])

	path, err := workflow.ResolvePath(args.workflowPath)
	if err != nil {
		fatal("resolve workflow path: %v", err)
	}

	wf, err := workflow.Load(path)
	if err != nil {
		fatal("%v", err)
	}

	cfg, err := config.FromMap(wf.Config)
	if err != nil {
		fatal("config: %v", err)
	}

	// CLI --port overrides workflow server.port.
	if args.port > 0 {
		cfg.Server.Port = args.port
	}

	if err := cfg.Validate(); err != nil {
		fatal("validation: %v", err)
	}

	if args.validateOnly {
		fmt.Printf("workflow %s: valid\n", path)
		fmt.Printf("  tracker: %s (project: %s)\n", cfg.Tracker.Kind, cfg.Tracker.ProjectSlug)
		fmt.Printf("  polling: %dms\n", cfg.Polling.IntervalMS)
		fmt.Printf("  workspace root: %s\n", cfg.Workspace.Root)
		fmt.Printf("  max agents: %d\n", cfg.Agent.MaxConcurrentAgents)
		fmt.Printf("  max turns: %d\n", cfg.Agent.MaxTurns)
		fmt.Printf("  codex command: %s\n", cfg.Codex.Command)
		if cfg.Server.Port > 0 {
			fmt.Printf("  server: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
		}
		os.Exit(0)
	}

	// TODO: start orchestrator loop (Milestone 5+)
	fmt.Println("symphony: orchestrator not yet implemented")
	os.Exit(0)
}

type cliArgs struct {
	validateOnly bool
	port         int
	workflowPath string
}

func parseArgs(args []string) cliArgs {
	var result cliArgs
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--validate-only":
			result.validateOnly = true
		case "--port":
			if i+1 < len(args) {
				i++
				p, err := strconv.Atoi(args[i])
				if err != nil {
					fatal("--port: invalid value %q", args[i])
				}
				result.port = p
			} else {
				fatal("--port requires a value")
			}
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			if args[i] != "" && args[i][0] == '-' {
				fatal("unknown flag: %s", args[i])
			}
			result.workflowPath = args[i]
		}
	}
	return result
}

func printUsage() {
	fmt.Fprintln(os.Stderr, `Usage: symphony [flags] [workflow-path]

Flags:
  --validate-only  Load and validate the workflow, then exit.
  --port PORT      Enable HTTP server on PORT.
  --help, -h       Show this help.

If workflow-path is omitted, WORKFLOW.md in the current directory is used.`)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "symphony: "+format+"\n", args...)
	os.Exit(1)
}
