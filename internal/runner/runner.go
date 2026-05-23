// Package runner implements the AgentRunner that wires workspace, prompt, and
// codex client into a single run lifecycle for the orchestrator.
package runner

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/kwanpham2195/symphony-go/internal/codex"
	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/domain"
	"github.com/kwanpham2195/symphony-go/internal/workflow"
	"github.com/kwanpham2195/symphony-go/internal/workspace"
)

// Runner implements the orchestrator.AgentRunner interface.
type Runner struct {
	cfg    *config.Config
	wsMgr  *workspace.Manager
	codexC *codex.Client
	logger *slog.Logger

	mu     sync.RWMutex
	prompt string // current workflow prompt template
}

// New creates an agent runner.
func New(cfg *config.Config, wsMgr *workspace.Manager, codexC *codex.Client, prompt string, logger *slog.Logger) *Runner {
	if logger == nil {
		logger = slog.Default()
	}
	return &Runner{
		cfg:    cfg,
		wsMgr:  wsMgr,
		codexC: codexC,
		logger: logger,
		prompt: prompt,
	}
}

// UpdatePrompt updates the prompt template (for dynamic reload).
func (r *Runner) UpdatePrompt(prompt string) {
	r.mu.Lock()
	r.prompt = prompt
	r.mu.Unlock()
}

// getPrompt safely reads the current prompt template.
func (r *Runner) getPrompt() string {
	r.mu.RLock()
	p := r.prompt
	r.mu.RUnlock()
	return p
}

// Run executes the full agent lifecycle for one issue:
// 1. Create/reuse workspace
// 2. Run before_run hook
// 3. Start codex session
// 4. Render prompt and run turns (up to max_turns)
// 5. Run after_run hook (best effort)
func (r *Runner) Run(ctx context.Context, issue domain.Issue, attempt *int, updates chan<- domain.AgentUpdate) error {
	// 1. Create workspace
	ws, err := r.wsMgr.CreateForIssue(ctx, issue)
	if err != nil {
		return fmt.Errorf("workspace creation failed: %w", err)
	}

	// 2. Before-run hook
	if err := r.wsMgr.RunBeforeRunHook(ctx, ws, issue); err != nil {
		return fmt.Errorf("before_run hook failed: %w", err)
	}

	// Ensure after_run runs regardless
	defer r.wsMgr.RunAfterRunHook(ctx, ws, issue)

	// 3. Start codex session
	sess, err := r.codexC.StartSession(ctx, ws.Path)
	if err != nil {
		return fmt.Errorf("codex session start failed: %w", err)
	}
	defer r.codexC.StopSession(sess)

	// 4. Render prompt
	rendered, err := workflow.RenderPrompt(r.getPrompt(), issue, attempt)
	if err != nil {
		return fmt.Errorf("prompt render failed: %w", err)
	}

	// Run turns
	maxTurns := r.cfg.Agent.MaxTurns
	for turn := 0; turn < maxTurns; turn++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		prompt := rendered
		if turn > 0 {
			// Continuation turn: send guidance instead of full prompt
			prompt = continuationPrompt(issue)
		}

		result, err := r.codexC.RunTurn(ctx, sess, issue, prompt, func(u domain.AgentUpdate) {
			select {
			case updates <- u:
			default:
			}
		})
		if err != nil {
			return fmt.Errorf("turn %d failed: %w", turn+1, err)
		}

		switch result.Status {
		case "completed":
			r.logger.Info("turn completed",
				"issue_identifier", issue.Identifier,
				"turn", turn+1,
				"session_id", result.SessionID,
			)
			// Continue to next turn if issue is still active
			// (orchestrator reconciliation handles stopping)

		case "failed", "cancelled", "timeout", "exit":
			return fmt.Errorf("turn %d ended: %s", turn+1, result.Status)

		case "input_required":
			return fmt.Errorf("turn %d: user input required (non-interactive)", turn+1)
		}
	}

	r.logger.Info("max turns reached",
		"issue_identifier", issue.Identifier,
		"max_turns", maxTurns,
	)
	return nil
}

func continuationPrompt(issue domain.Issue) string {
	return strings.TrimSpace(fmt.Sprintf(
		"Continue working on %s: %s. The issue is still in an active state. "+
			"Resume from current workspace state.",
		issue.Identifier, issue.Title,
	))
}
