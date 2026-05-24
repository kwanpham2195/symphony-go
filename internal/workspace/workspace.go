// Package workspace manages per-issue workspaces: creation, reuse, removal,
// path safety, and lifecycle hooks.
package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

// unsafeChars matches anything not in [A-Za-z0-9._-].
var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// Manager handles workspace lifecycle for local workers.
type Manager struct {
	cfg    atomic.Pointer[config.Config]
	logger *slog.Logger
}

// NewManager creates a workspace manager.
func NewManager(cfg *config.Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{logger: logger}
	m.cfg.Store(cfg)
	return m
}

// UpdateConfig replaces the config (for dynamic reload).
func (m *Manager) UpdateConfig(cfg *config.Config) {
	m.cfg.Store(cfg)
}

// config returns the current config snapshot.
func (m *Manager) config() *config.Config {
	return m.cfg.Load()
}

// CreateForIssue creates or reuses a workspace directory for an issue.
// It returns an internal.Workspace with CreatedNow=true if newly created.
// If the workspace was newly created and after_create hook is configured,
// it runs the hook. Hook failure is fatal: the workspace is removed.
func (m *Manager) CreateForIssue(ctx context.Context, issue internal.Issue) (internal.Workspace, error) {
	cfg := m.config()
	key := SafeIdentifier(issue.Identifier)
	root := cfg.Workspace.Root

	wsPath := filepath.Join(root, key)

	// Validate path safety before creating anything
	if err := ValidatePath(wsPath, root); err != nil {
		return internal.Workspace{}, fmt.Errorf("workspace path validation: %w", err)
	}

	// Ensure root exists
	if err := os.MkdirAll(root, 0o755); err != nil {
		return internal.Workspace{}, fmt.Errorf("create workspace root: %w", err)
	}

	createdNow := false
	info, err := os.Lstat(wsPath)
	switch {
	case err == nil && info.IsDir():
		// Existing directory: reuse
		createdNow = false
	case err == nil && !info.IsDir():
		// Stale non-directory: remove and recreate
		if removeErr := os.RemoveAll(wsPath); removeErr != nil {
			return internal.Workspace{}, fmt.Errorf("remove stale workspace path: %w", removeErr)
		}
		if mkErr := os.MkdirAll(wsPath, 0o755); mkErr != nil {
			return internal.Workspace{}, fmt.Errorf("create workspace dir: %w", mkErr)
		}
		createdNow = true
	case os.IsNotExist(err):
		if mkErr := os.MkdirAll(wsPath, 0o755); mkErr != nil {
			return internal.Workspace{}, fmt.Errorf("create workspace dir: %w", mkErr)
		}
		createdNow = true
	default:
		return internal.Workspace{}, fmt.Errorf("stat workspace path: %w", err)
	}

	ws := internal.Workspace{
		Path:         wsPath,
		WorkspaceKey: key,
		CreatedNow:   createdNow,
	}

	// Run after_create hook if newly created
	if createdNow && cfg.Hooks.AfterCreate != "" {
		m.logger.Info("running workspace hook",
			"hook", "after_create",
			"issue_identifier", issue.Identifier,
			"workspace", wsPath,
		)
		if err := m.runHook(ctx, cfg.Hooks.AfterCreate, wsPath, issue.Identifier, "after_create"); err != nil {
			// Fatal: remove partially created workspace
			_ = os.RemoveAll(wsPath)
			return internal.Workspace{}, fmt.Errorf("after_create hook: %w", err)
		}
	}

	return ws, nil
}

// RemoveIssueWorkspace removes the workspace for an issue identifier.
// Runs before_remove hook if configured (failure logged and ignored).
func (m *Manager) RemoveIssueWorkspace(ctx context.Context, identifier string) error {
	cfg := m.config()
	key := SafeIdentifier(identifier)
	wsPath := filepath.Join(cfg.Workspace.Root, key)

	if err := ValidatePath(wsPath, cfg.Workspace.Root); err != nil {
		return fmt.Errorf("workspace path validation: %w", err)
	}

	// Run before_remove hook if directory exists
	if info, err := os.Stat(wsPath); err == nil && info.IsDir() {
		if cfg.Hooks.BeforeRemove != "" {
			m.logger.Info("running workspace hook",
				"hook", "before_remove",
				"issue_identifier", identifier,
				"workspace", wsPath,
			)
			if err := m.runHook(ctx, cfg.Hooks.BeforeRemove, wsPath, identifier, "before_remove"); err != nil {
				m.logger.Warn("before_remove hook failed (ignored)",
					"issue_identifier", identifier,
					"error", err,
				)
			}
		}
	}

	return os.RemoveAll(wsPath)
}

// RunBeforeRunHook runs the before_run hook in the workspace.
// Failure is fatal to the current run attempt.
func (m *Manager) RunBeforeRunHook(ctx context.Context, ws internal.Workspace, issue internal.Issue) error {
	cfg := m.config()
	if cfg.Hooks.BeforeRun == "" {
		return nil
	}
	m.logger.Info("running workspace hook",
		"hook", "before_run",
		"issue_identifier", issue.Identifier,
		"workspace", ws.Path,
	)
	return m.runHook(ctx, cfg.Hooks.BeforeRun, ws.Path, issue.Identifier, "before_run")
}

// RunAfterRunHook runs the after_run hook in the workspace.
// Failure is logged and ignored.
func (m *Manager) RunAfterRunHook(ctx context.Context, ws internal.Workspace, issue internal.Issue) {
	cfg := m.config()
	if cfg.Hooks.AfterRun == "" {
		return
	}
	m.logger.Info("running workspace hook",
		"hook", "after_run",
		"issue_identifier", issue.Identifier,
		"workspace", ws.Path,
	)
	if err := m.runHook(ctx, cfg.Hooks.AfterRun, ws.Path, issue.Identifier, "after_run"); err != nil {
		m.logger.Warn("after_run hook failed (ignored)",
			"issue_identifier", issue.Identifier,
			"error", err,
		)
	}
}

// runHook executes a shell script in the workspace directory with a timeout.
func (m *Manager) runHook(ctx context.Context, script, workDir, identifier, hookName string) error {
	cfg := m.config()
	timeoutMS := cfg.Hooks.TimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 60000
	}
	timeout := time.Duration(timeoutMS) * time.Millisecond

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-lc", script)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()

	if ctx.Err() == context.DeadlineExceeded {
		m.logger.Warn("workspace hook timed out",
			"hook", hookName,
			"issue_identifier", identifier,
			"workspace", workDir,
			"timeout_ms", timeoutMS,
		)
		return fmt.Errorf("workspace_hook_timeout: %s: %dms", hookName, timeoutMS)
	}

	if err != nil {
		sanitized := truncateOutput(string(output), 2048)
		m.logger.Warn("workspace hook failed",
			"hook", hookName,
			"issue_identifier", identifier,
			"workspace", workDir,
			"error", err,
			"output", sanitized,
		)
		return fmt.Errorf("workspace_hook_failed: %s: %w", hookName, err)
	}

	return nil
}

// SafeIdentifier sanitizes an issue identifier for use as a directory name.
// Only [A-Za-z0-9._-] are kept; everything else becomes "_".
func SafeIdentifier(identifier string) string {
	if identifier == "" {
		return "issue"
	}
	return unsafeChars.ReplaceAllString(identifier, "_")
}

// ValidatePath checks workspace path safety invariants:
// 1. wsPath must be inside root (prefix check on absolute paths)
// 2. wsPath must not equal root
// 3. No symlink escape
func ValidatePath(wsPath, root string) error {
	absWS, err := filepath.Abs(wsPath)
	if err != nil {
		return fmt.Errorf("workspace_path_unreadable: %w", err)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("workspace_path_unreadable: %w", err)
	}

	// Must not equal root
	if absWS == absRoot {
		return fmt.Errorf("workspace_equals_root: %s", absRoot)
	}

	// Must be under root
	rootPrefix := absRoot + string(filepath.Separator)
	if !strings.HasPrefix(absWS+string(filepath.Separator), rootPrefix) {
		return fmt.Errorf("workspace_outside_root: ws=%s root=%s", absWS, absRoot)
	}

	// Symlink escape check: if path exists, resolve real path and re-check
	realWS, err := filepath.EvalSymlinks(absWS)
	if err == nil && realWS != absWS {
		realRoot, err2 := filepath.EvalSymlinks(absRoot)
		if err2 != nil {
			return fmt.Errorf("workspace_path_unreadable: cannot resolve root: %w", err2)
		}
		realRootPrefix := realRoot + string(filepath.Separator)
		if !strings.HasPrefix(realWS+string(filepath.Separator), realRootPrefix) {
			return fmt.Errorf("workspace_symlink_escape: ws=%s resolves to %s, outside root %s", absWS, realWS, realRoot)
		}
	}

	return nil
}

func truncateOutput(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen] + "... (truncated)"
	}
	return s
}
