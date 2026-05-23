// Package domain defines shared types used across symphony packages.
package domain

import "time"

// Issue is a normalized tracker issue used by orchestration, prompt rendering,
// and observability output.
type Issue struct {
	ID          string     `json:"id"`
	Identifier  string     `json:"identifier"`
	Title       string     `json:"title"`
	Description string     `json:"description,omitempty"`
	Priority    *int       `json:"priority,omitempty"`
	State       string     `json:"state"`
	BranchName  string     `json:"branch_name,omitempty"`
	URL         string     `json:"url,omitempty"`
	Labels      []string   `json:"labels,omitempty"`
	BlockedBy   []Blocker  `json:"blocked_by,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
	UpdatedAt   *time.Time `json:"updated_at,omitempty"`
}

// Blocker is a reference to an issue that blocks dispatch.
type Blocker struct {
	ID         string `json:"id,omitempty"`
	Identifier string `json:"identifier,omitempty"`
	State      string `json:"state,omitempty"`
}

// Workflow holds the parsed WORKFLOW.md content.
type Workflow struct {
	Config         map[string]any `json:"config"`
	PromptTemplate string         `json:"prompt_template"`
}

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

// Workspace represents a per-issue filesystem workspace.
type Workspace struct {
	Path         string `json:"path"`
	WorkspaceKey string `json:"workspace_key"`
	CreatedNow   bool   `json:"created_now"`
}

// AgentUpdate is a structured event emitted by the codex client back to the
// orchestrator.
type AgentUpdate struct {
	Event     string         `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
	SessionID string         `json:"session_id,omitempty"`
	Usage     *TokenUsage    `json:"usage,omitempty"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// TokenUsage holds token counts from a codex event.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// LiveSession tracks metadata for a running codex app-server session.
type LiveSession struct {
	SessionID                string     `json:"session_id"`
	ThreadID                 string     `json:"thread_id"`
	TurnID                   string     `json:"turn_id"`
	CodexPID                 int        `json:"codex_pid,omitempty"`
	LastCodexEvent           string     `json:"last_codex_event,omitempty"`
	LastCodexTimestamp       *time.Time `json:"last_codex_timestamp,omitempty"`
	CodexInputTokens         int        `json:"codex_input_tokens"`
	CodexOutputTokens        int        `json:"codex_output_tokens"`
	CodexTotalTokens         int        `json:"codex_total_tokens"`
	LastReportedInputTokens  int        `json:"last_reported_input_tokens"`
	LastReportedOutputTokens int        `json:"last_reported_output_tokens"`
	LastReportedTotalTokens  int        `json:"last_reported_total_tokens"`
	TurnCount                int        `json:"turn_count"`
}

// RetryEntry is a scheduled retry for an issue.
type RetryEntry struct {
	IssueID    string    `json:"issue_id"`
	Identifier string    `json:"identifier"`
	Attempt    int       `json:"attempt"`
	DueAt      time.Time `json:"due_at"`
	Error      string    `json:"error,omitempty"`
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
