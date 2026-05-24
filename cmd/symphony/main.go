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
//	--once           Run a single poll cycle then exit.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/codex"
	"github.com/kwanpham2195/symphony-go/internal/codex/tools"
	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/observability"
	"github.com/kwanpham2195/symphony-go/internal/orchestrator"
	"github.com/kwanpham2195/symphony-go/internal/runner"
	"github.com/kwanpham2195/symphony-go/internal/server"
	linearClient "github.com/kwanpham2195/symphony-go/internal/tracker/linear"
	"github.com/kwanpham2195/symphony-go/internal/workflow"
	"github.com/kwanpham2195/symphony-go/internal/workspace"
)

// Set by goreleaser ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	args := parseArgs(os.Args[1:])
	logger := observability.NewTextLogger()

	// Load workflow
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

	// Build components
	tracker := linearClient.NewClient(cfg.Tracker.Endpoint, cfg.Tracker.APIKey, cfg.Tracker.ProjectSlug, cfg.Tracker.ActiveStates)
	wsMgr := workspace.NewManager(cfg, logger)
	codexClient := codex.NewClient(cfg, logger)

	// Register dynamic tools
	codexClient.RegisterTool(tools.NewLinearGraphQL(tracker))

	agentRunner := runner.New(cfg, wsMgr, codexClient, wf.PromptTemplate, logger)

	orch := orchestrator.New(orchestrator.Deps{
		Tracker:    tracker,
		Workspace:  wsMgr,
		Runner:     agentRunner,
		Config:     cfg,
		Logger:     logger,
		WorkerPool: orchestrator.NewWorkerPool(cfg.Worker.SSHHosts, cfg.Worker.MaxConcurrentAgentsPerHost),
	})

	if args.once {
		logger.Info("running single poll cycle", "workflow", path)
		ctx := context.Background()
		orch.Tick(ctx)
		logger.Info("single poll cycle complete")
		os.Exit(0)
	}

	// Start workflow file watcher for dynamic reload
	wfWatcher, err := workflow.NewWatcher(path, func(newWF *internal.Workflow) {
		newCfg, err := config.FromMap(newWF.Config)
		if err != nil {
			logger.Error("workflow reload config error; keeping last good", "error", err)
			return
		}
		if err := newCfg.Validate(); err != nil {
			logger.Error("workflow reload validation error; keeping last good", "error", err)
			return
		}
		// Preserve CLI overrides
		if args.port > 0 {
			newCfg.Server.Port = args.port
		}
		// Update components atomically
		wsMgr.UpdateConfig(newCfg)
		codexClient.UpdateConfig(newCfg)
		agentRunner.UpdatePrompt(newWF.PromptTemplate)
		logger.Info("config reloaded from workflow")
	}, logger)
	if err != nil {
		logger.Warn("workflow file watcher failed to start; dynamic reload disabled", "error", err)
	} else {
		defer wfWatcher.Close()
	}

	// Start orchestrator with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigCh
		logger.Info("received signal; shutting down", "signal", sig)
		cancel()
	}()

	logger.Info("starting symphony",
		"workflow", path,
		"tracker", cfg.Tracker.Kind,
		"project", cfg.Tracker.ProjectSlug,
		"poll_interval_ms", cfg.Polling.IntervalMS,
		"max_agents", cfg.Agent.MaxConcurrentAgents,
	)

	// Start HTTP server if configured
	if cfg.Server.Port > 0 {
		srv := server.New(orch, orch, server.Options{
			Port: cfg.Server.Port,
			Host: cfg.Server.Host,
		}, logger)

		go func() {
			if err := srv.ListenAndServe(ctx); err != nil {
				logger.Error("http server error", "error", err)
			}
		}()
	}

	if err := orch.Start(ctx); err != nil && !errors.Is(err, context.Canceled) {
		fatal("orchestrator: %v", err)
	}
}

type cliArgs struct {
	validateOnly bool
	once         bool
	port         int
	workflowPath string
}

func parseArgs(args []string) cliArgs {
	var result cliArgs
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--validate-only":
			result.validateOnly = true
		case "--once":
			result.once = true
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
		case "--version", "-v":
			fmt.Printf("symphony %s (commit %s, built %s)\n", version, commit, date)
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
  --once           Run a single poll cycle, then exit.
  --port PORT      Enable HTTP server on PORT.
  --version, -v    Show version.
  --help, -h       Show this help.

If workflow-path is omitted, WORKFLOW.md in the current directory is used.`)
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "symphony: "+format+"\n", args...)
	os.Exit(1)
}
