// Package worker provides local and SSH worker launch abstractions.
//
// A Launcher starts a process (local or remote) and runs hooks. The
// orchestrator uses this to dispatch work to either local or SSH hosts.
package worker

import (
	"context"
	"os/exec"
	"time"
)

// Process represents a running worker process.
type Process struct {
	Cmd  *exec.Cmd
	Host string // empty for local
}

// HookResult holds the outcome of a hook execution.
type HookResult struct {
	ExitCode int
	Output   string
}

// Launcher starts processes and runs hooks on a target host.
type Launcher interface {
	// Start launches a command in a workspace directory.
	Start(ctx context.Context, workspace string, command string) (*Process, error)

	// RunHook executes a script in the workspace with a timeout.
	RunHook(ctx context.Context, workspace string, script string, timeout time.Duration) (*HookResult, error)

	// Host returns the host identifier (empty for local).
	Host() string
}
