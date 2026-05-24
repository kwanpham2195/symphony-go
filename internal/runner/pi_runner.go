package runner

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/pi"
	"github.com/kwanpham2195/symphony-go/internal/workflow"
	"github.com/kwanpham2195/symphony-go/internal/workspace"
)

// PiRunner implements the orchestrator.AgentRunner interface using the Pi RPC
// client. It sends one prompt per Run() call — Pi handles tool execution,
// compaction, and retry internally.
type PiRunner struct {
	cfg    *config.Config
	wsMgr  *workspace.Manager
	piC    *pi.Client
	logger *slog.Logger

	mu     sync.RWMutex
	prompt string
}

// NewPiRunner creates an agent runner for Pi.
func NewPiRunner(cfg *config.Config, wsMgr *workspace.Manager, piC *pi.Client, prompt string, logger *slog.Logger) *PiRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &PiRunner{
		cfg:    cfg,
		wsMgr:  wsMgr,
		piC:    piC,
		logger: logger,
		prompt: prompt,
	}
}

// UpdatePrompt updates the prompt template (for dynamic reload).
func (r *PiRunner) UpdatePrompt(prompt string) {
	r.mu.Lock()
	r.prompt = prompt
	r.mu.Unlock()
}

func (r *PiRunner) getPrompt() string {
	r.mu.RLock()
	p := r.prompt
	r.mu.RUnlock()
	return p
}

// Run executes the full agent lifecycle for one issue:
// 1. Create/reuse workspace
// 2. Run before_run hook
// 3. Start Pi session (no handshake)
// 4. Render prompt and send to Pi
// 5. Stream events until agent_end
// 6. Run after_run hook (best effort)
func (r *PiRunner) Run(ctx context.Context, issue internal.Issue, attempt *int, updates chan<- internal.AgentUpdate) error {
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

	// 3. Start Pi session
	sess, err := r.piC.StartSession(ctx, ws.Path)
	if err != nil {
		return fmt.Errorf("pi session start failed: %w", err)
	}
	defer r.piC.StopSession(sess) //nolint:errcheck // best-effort cleanup

	// 4. Render prompt
	rendered, err := workflow.RenderPrompt(r.getPrompt(), issue, attempt)
	if err != nil {
		return fmt.Errorf("prompt render failed: %w", err)
	}

	// 5. Send prompt and stream events
	result, err := r.piC.SendPrompt(ctx, sess, rendered, func(u internal.AgentUpdate) {
		select {
		case updates <- u:
		default:
		}
	})
	if err != nil {
		return fmt.Errorf("pi prompt failed: %w", err)
	}

	switch result.Status {
	case internal.TurnStatusCompleted:
		r.logger.Info("pi run completed",
			"issue_identifier", issue.Identifier,
		)
		return nil
	default:
		return fmt.Errorf("pi run ended: %s", result.Status)
	}
}
