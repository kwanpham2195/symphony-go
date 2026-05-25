// GC provides periodic garbage collection for per-issue workspace directories.
//
// It removes workspaces for terminal issues past a TTL, detects orphan
// directories with no matching issue in the tracker, and strips artifact
// patterns (e.g. node_modules) from completed workspaces before full removal.
package workspace

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

// IssueChecker is the minimal tracker interface for garbage collection.
type IssueChecker interface {
	FetchIssuesByStates(ctx context.Context, states []string) ([]internal.Issue, error)
}

// GCResult holds the outcome of a single garbage collection pass.
type GCResult struct {
	RemovedDirs  []string // workspace dirs fully removed (terminal)
	ArtifactDirs []string // workspace dirs where artifacts were cleaned
	Errors       []error
}

// GC performs periodic garbage collection of workspace directories.
type GC struct {
	cfg      atomic.Pointer[config.Config]
	checker  IssueChecker
	lockedFn func() map[string]bool // workspace keys currently in use
	logger   *slog.Logger
}

// NewGC creates a workspace garbage collector.
// lockedFn returns workspace keys (SafeIdentifier values) that the
// orchestrator is currently using. Those workspaces are never touched.
func NewGC(
	cfg *config.Config,
	checker IssueChecker,
	lockedFn func() map[string]bool,
	logger *slog.Logger,
) *GC {
	if logger == nil {
		logger = slog.Default()
	}
	g := &GC{
		checker:  checker,
		lockedFn: lockedFn,
		logger:   logger,
	}
	g.cfg.Store(cfg)
	return g
}

// UpdateConfig replaces the live config (for dynamic reload).
func (g *GC) UpdateConfig(cfg *config.Config) {
	g.cfg.Store(cfg)
}

func (g *GC) config() *config.Config {
	return g.cfg.Load()
}

// minGCInterval is the floor for the GC ticker. Prevents a panic from
// time.NewTicker(0) when the user sets gc.interval_ms to zero or negative.
const minGCInterval = time.Hour

// clampGCInterval returns d if positive, otherwise minGCInterval.
func clampGCInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return minGCInterval
	}
	return d
}

// Start runs the GC loop until ctx is cancelled.
// If GC is disabled in config, it idles but re-checks on each tick so
// a dynamic config reload can enable it without a restart.
func (g *GC) Start(ctx context.Context) {
	cfg := g.config()
	interval := clampGCInterval(time.Duration(cfg.GC.IntervalMS) * time.Millisecond)

	g.logger.Info("workspace gc loop started",
		"interval", interval,
		"enabled", cfg.GC.Enabled,
	)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			g.logger.Info("workspace gc stopped")
			return
		case <-ticker.C:
			curCfg := g.config()
			if !curCfg.GC.Enabled {
				continue
			}

			// Adjust interval if config changed.
			newInterval := clampGCInterval(time.Duration(curCfg.GC.IntervalMS) * time.Millisecond)
			if newInterval != interval {
				ticker.Reset(newInterval)
				interval = newInterval
			}

			result := g.Collect(ctx)
			total := len(result.RemovedDirs) + len(result.ArtifactDirs)
			if total > 0 || len(result.Errors) > 0 {
				g.logger.Info("workspace gc pass complete",
					"removed", len(result.RemovedDirs),
					"artifacts_cleaned", len(result.ArtifactDirs),
					"errors", len(result.Errors),
				)
			}
		}
	}
}

// Collect runs a single GC pass. Exported for testing.
func (g *GC) Collect(ctx context.Context) GCResult {
	var result GCResult
	cfg := g.config()
	root := cfg.Workspace.Root

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return result
		}
		result.Errors = append(result.Errors, err)
		return result
	}

	// Build terminal set from the tracker.
	terminalSet, err := g.buildTerminalSet(ctx, cfg)
	if err != nil {
		result.Errors = append(result.Errors, err)
		return result
	}

	locked := g.lockedWorkspaces()

	now := time.Now()
	ttl := time.Duration(cfg.GC.TTLMS) * time.Millisecond
	artifactTTL := time.Duration(cfg.GC.ArtifactTTLMS) * time.Millisecond

	for _, entry := range entries {
		if ctx.Err() != nil {
			return result
		}
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if locked[name] {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			result.Errors = append(result.Errors, infoErr)
			continue
		}
		age := now.Sub(info.ModTime())
		dirPath := filepath.Join(root, name)

		switch {
		case terminalSet[name]:
			g.handleTerminal(dirPath, name, age, ttl, artifactTTL, cfg, &result)
		default:
			// Active, in-review, or unknown: never touch.
			continue
		}
	}

	return result
}

// buildTerminalSet fetches terminal issues from the tracker and returns a
// set of SafeIdentifier values. Non-terminal workspaces are never touched.
func (g *GC) buildTerminalSet(
	ctx context.Context,
	cfg *config.Config,
) (map[string]bool, error) {
	terminalIssues, err := g.checker.FetchIssuesByStates(ctx, cfg.Tracker.TerminalStates)
	if err != nil {
		g.logger.Warn("gc: fetch terminal issues failed", "error", err)
		return nil, err
	}

	terminal := make(map[string]bool, len(terminalIssues))
	for _, issue := range terminalIssues {
		terminal[SafeIdentifier(issue.Identifier)] = true
	}

	return terminal, nil
}

func (g *GC) lockedWorkspaces() map[string]bool {
	if g.lockedFn == nil {
		return nil
	}
	return g.lockedFn()
}

// handleTerminal removes the workspace if past TTL, or strips artifacts if
// past artifact TTL.
func (g *GC) handleTerminal(
	dirPath, name string,
	age, ttl, artifactTTL time.Duration,
	cfg *config.Config,
	result *GCResult,
) {
	if age >= ttl {
		if err := os.RemoveAll(dirPath); err != nil {
			g.logger.Warn("gc: remove terminal workspace failed",
				"dir", name, "error", err)
			result.Errors = append(result.Errors, err)
		} else {
			g.logger.Info("gc: removed terminal workspace",
				"dir", name, "age_h", age.Hours())
			result.RemovedDirs = append(result.RemovedDirs, name)
		}
		return
	}

	if age >= artifactTTL && len(cfg.GC.ArtifactPatterns) > 0 {
		if g.cleanArtifacts(dirPath, name, cfg.GC.ArtifactPatterns) {
			result.ArtifactDirs = append(result.ArtifactDirs, name)
		}
	}
}

// cleanArtifacts removes matching subdirectories from a workspace.
// Only plain directory names are accepted (no path separators, no ".." ).
func (g *GC) cleanArtifacts(dirPath, name string, patterns []string) bool {
	cleaned := false
	for _, pattern := range patterns {
		if !isSafeArtifactPattern(pattern) {
			continue
		}
		target := filepath.Join(dirPath, pattern)
		info, err := os.Lstat(target)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := os.RemoveAll(target); err != nil {
			g.logger.Warn("gc: remove artifact failed",
				"dir", name, "pattern", pattern, "error", err)
		} else {
			g.logger.Info("gc: removed artifact",
				"dir", name, "pattern", pattern)
			cleaned = true
		}
	}
	return cleaned
}

// isSafeArtifactPattern returns true for simple directory names with no
// path separators or parent-directory references.
func isSafeArtifactPattern(pattern string) bool {
	if pattern == "" || pattern == "." || pattern == ".." {
		return false
	}
	if strings.ContainsAny(pattern, "/\\") {
		return false
	}
	return true
}
