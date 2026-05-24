package codex

import "time"

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
