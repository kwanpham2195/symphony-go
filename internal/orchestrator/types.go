package orchestrator

import "time"

// RunAttempt tracks one execution attempt for an issue.
type RunAttempt struct {
	IssueID         string    `json:"issue_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	Attempt         *int      `json:"attempt,omitempty"`
	WorkspacePath   string    `json:"workspace_path"`
	StartedAt       time.Time `json:"started_at"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
}

// RetryEntry is a scheduled retry for an issue.
type RetryEntry struct {
	IssueID    string      `json:"issue_id"`
	Identifier string      `json:"identifier"`
	Attempt    int         `json:"attempt"`
	DueAt      time.Time   `json:"due_at"`
	Error      string      `json:"error,omitempty"`
	Timer      *time.Timer `json:"-"`
}

// Snapshot is a point-in-time view of orchestrator state for dashboards and
// the status API.
type Snapshot struct {
	Running     []RunningRow   `json:"running"`
	Retrying    []RetryRow     `json:"retrying"`
	CodexTotals CodexTotals    `json:"codex_totals"`
	RateLimits  map[string]any `json:"rate_limits,omitempty"`
}

// RunningRow is a single row in the snapshot running list.
type RunningRow struct {
	IssueID         string    `json:"issue_id"`
	IssueIdentifier string    `json:"issue_identifier"`
	SessionID       string    `json:"session_id"`
	TurnCount       int       `json:"turn_count"`
	StartedAt       time.Time `json:"started_at"`
}

// RetryRow is a single row in the snapshot retry list.
type RetryRow struct {
	IssueID    string    `json:"issue_id"`
	Identifier string    `json:"identifier"`
	Attempt    int       `json:"attempt"`
	DueAt      time.Time `json:"due_at"`
	Error      string    `json:"error,omitempty"`
}

// CodexTotals holds aggregate token and runtime totals.
type CodexTotals struct {
	InputTokens    int     `json:"input_tokens"`
	OutputTokens   int     `json:"output_tokens"`
	TotalTokens    int     `json:"total_tokens"`
	SecondsRunning float64 `json:"seconds_running"`
}
