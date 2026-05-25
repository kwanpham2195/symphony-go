package orchestrator

import (
	"context"
	"log/slog"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/tracker"
)

// commentState holds the state for the comment polling loop.
type commentState struct {
	writer             tracker.TrackerWriter
	viewerID           string // API key owner; comments from this user are skipped
	inProgressStateID  string // cached state ID for the target active state
	lastCheck          time.Time
	lastHandledComment map[string]string // issueID -> last handled comment ID
	tickCounter        int
}

// initComments sets up the comment polling state. Called once at startup.
// Returns nil if comments are disabled or setup fails (non-fatal).
func initComments(
	ctx context.Context,
	cfg commentsConfig,
	writer tracker.TrackerWriter,
	logger *slog.Logger,
) *commentState {
	if !cfg.enabled {
		return nil
	}
	if writer == nil {
		logger.Warn("comment trigger: disabled (no TrackerWriter)")
		return nil
	}

	viewerID, err := writer.ViewerID(ctx)
	if err != nil {
		logger.Warn("comment trigger: disabled (ViewerID failed)", "error", err)
		return nil
	}

	// Resolve the target active state ID once.
	activeStateName := cfg.activeState
	if activeStateName == "" {
		activeStateName = "In Progress"
	}
	stateID, err := writer.ResolveStateID(ctx, activeStateName)
	if err != nil {
		logger.Warn("comment trigger: disabled (ResolveStateID failed)",
			"state", activeStateName, "error", err)
		return nil
	}

	lookback := time.Duration(cfg.lookbackMS) * time.Millisecond
	logger.Info("comment trigger: enabled",
		"viewer_id", viewerID,
		"review_state", cfg.reviewState,
		"active_state", activeStateName,
		"poll_interval_ticks", cfg.pollIntervalTicks,
	)

	return &commentState{
		writer:             writer,
		viewerID:           viewerID,
		inProgressStateID:  stateID,
		lastCheck:          time.Now().Add(-lookback),
		lastHandledComment: make(map[string]string),
	}
}

// commentsConfig is the subset of config needed by the comment loop.
// Avoids importing the full config package in this file.
type commentsConfig struct {
	enabled           bool
	pollIntervalTicks int
	lookbackMS        int
	reviewState       string
	activeState       string // first entry from active_states
}

// checkComments polls for new comments on In Review issues and dispatches
// agents. Must be called while NOT holding o.mu.
func (o *Orchestrator) checkComments(ctx context.Context) {
	cs := o.commentState
	if cs == nil {
		return
	}

	cs.tickCounter++
	interval := o.deps.Config.Comments.PollIntervalTicks
	if interval <= 0 {
		interval = 3
	}
	if cs.tickCounter%interval != 0 {
		return
	}

	reviewState := o.deps.Config.Comments.ReviewState
	if reviewState == "" {
		reviewState = "In Review"
	}

	// Fetch issues in the review state.
	issues, err := o.deps.Tracker.FetchIssuesByStates(ctx, []string{reviewState})
	if err != nil {
		o.logger.Debug("comment check: fetch review issues failed", "error", err)
		return
	}
	if len(issues) == 0 {
		return
	}

	// Collect IDs and build a lookup map.
	issueIDs := make([]string, len(issues))
	issueMap := make(map[string]internal.Issue, len(issues))
	for i, issue := range issues {
		issueIDs[i] = issue.ID
		issueMap[issue.ID] = issue
	}

	// Fetch recent comments.
	commentMap, err := o.deps.Tracker.FetchRecentComments(ctx, issueIDs, cs.lastCheck)
	if err != nil {
		o.logger.Debug("comment check: fetch comments failed", "error", err)
		return
	}

	now := time.Now()

	for issueID, comments := range commentMap {
		issue, ok := issueMap[issueID]
		if !ok {
			continue
		}

		// Filter: skip agent-authored and bot comments.
		var filtered []internal.Comment
		for _, c := range comments {
			if c.UserID == cs.viewerID {
				continue
			}
			if c.BotActor {
				continue
			}
			// Skip already-handled comments.
			if cs.lastHandledComment[issueID] != "" && c.ID <= cs.lastHandledComment[issueID] {
				continue
			}
			filtered = append(filtered, c)
		}
		if len(filtered) == 0 {
			continue
		}

		// Skip if already running or claimed.
		o.mu.Lock()
		if o.claimed[issueID] || o.running[issueID] != nil {
			o.mu.Unlock()
			continue
		}
		// Claim before transitioning to prevent race with regular dispatch.
		o.claimed[issueID] = true
		o.mu.Unlock()

		// Transition issue to In Progress.
		if err := cs.writer.TransitionIssueState(ctx, issueID, cs.inProgressStateID); err != nil {
			o.logger.Warn("comment trigger: state transition failed",
				"issue_id", issueID,
				"issue_identifier", issue.Identifier,
				"error", err,
			)
			// Release claim on failure.
			o.mu.Lock()
			delete(o.claimed, issueID)
			o.mu.Unlock()
			continue
		}

		// Attach comments and dispatch.
		issue.TriggerComments = filtered
		issue.State = o.deps.Config.Tracker.ActiveStates[0] // now In Progress

		o.logger.Info("comment trigger: dispatching",
			"issue_id", issueID,
			"issue_identifier", issue.Identifier,
			"comments", len(filtered),
		)

		// Update last handled to the newest comment.
		cs.lastHandledComment[issueID] = filtered[len(filtered)-1].ID

		o.dispatchIssue(ctx, issue, nil)
	}

	cs.lastCheck = now
}
