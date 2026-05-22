package worker

import (
	"context"
	"os/exec"
	"time"
)

// LocalLauncher runs processes on the local machine.
type LocalLauncher struct{}

// NewLocalLauncher creates a local launcher.
func NewLocalLauncher() *LocalLauncher {
	return &LocalLauncher{}
}

// Host returns empty string for local.
func (l *LocalLauncher) Host() string {
	return ""
}

// Start launches a command locally in a workspace directory.
func (l *LocalLauncher) Start(ctx context.Context, workspace string, command string) (*Process, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", command)
	cmd.Dir = workspace
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &Process{Cmd: cmd}, nil
}

// RunHook executes a shell script in the workspace with a timeout.
func (l *LocalLauncher) RunHook(ctx context.Context, workspace string, script string, timeout time.Duration) (*HookResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-lc", script)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return &HookResult{ExitCode: -1, Output: string(output)}, ctx.Err()
	}

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, err
		}
	}

	return &HookResult{ExitCode: exitCode, Output: string(output)}, nil
}
