package internal

import "time"

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
