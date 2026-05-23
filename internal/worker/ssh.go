package worker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SSHLauncher runs processes on a remote host via SSH.
type SSHLauncher struct {
	host string
}

// NewSSHLauncher creates an SSH launcher for the given host.
func NewSSHLauncher(host string) *SSHLauncher {
	return &SSHLauncher{host: host}
}

// Host returns the SSH host.
func (s *SSHLauncher) Host() string {
	return s.host
}

// Start launches a command on the remote host via SSH.
func (s *SSHLauncher) Start(ctx context.Context, workspace string, command string) (*Process, error) {
	remoteCmd := fmt.Sprintf("cd %s && exec %s", shellEscape(workspace), command)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=accept-new", s.host, remoteCmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("ssh start on %s: %w", s.host, err)
	}
	return &Process{Cmd: cmd, Host: s.host}, nil
}

// RunHook executes a shell script on the remote host via SSH with a timeout.
func (s *SSHLauncher) RunHook(ctx context.Context, workspace string, script string, timeout time.Duration) (*HookResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	remoteCmd := fmt.Sprintf("cd %s && %s", shellEscape(workspace), script)
	cmd := exec.CommandContext(ctx, "ssh", "-o", "StrictHostKeyChecking=accept-new", s.host, remoteCmd)
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		return &HookResult{ExitCode: -1, Output: string(output)}, ctx.Err()
	}

	exitCode := 0
	if err != nil {
		exitErr := &exec.ExitError{}
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("ssh hook on %s: %w", s.host, err)
		}
	}

	return &HookResult{ExitCode: exitCode, Output: string(output)}, nil
}

// shellEscape wraps a value in single quotes for safe shell use.
func shellEscape(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
