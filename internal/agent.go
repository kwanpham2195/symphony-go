package internal

import "time"

// AgentEvent identifies the kind of event in an AgentUpdate.
type AgentEvent string

const (
	EventSessionStarted       AgentEvent = "session_started"
	EventTurnCompleted        AgentEvent = "turn_completed"
	EventTurnFailed           AgentEvent = "turn_failed"
	EventTurnCancelled        AgentEvent = "turn_cancelled"
	EventTurnInputRequired    AgentEvent = "turn_input_required"
	EventApprovalAutoApproved AgentEvent = "approval_auto_approved"
	EventToolCallCompleted    AgentEvent = "tool_call_completed"
	EventToolCallFailed       AgentEvent = "tool_call_failed"
	EventUnsupportedToolCall  AgentEvent = "unsupported_tool_call"
	EventNotification         AgentEvent = "notification"
	EventMalformed            AgentEvent = "malformed"
	EventCompactionStarted    AgentEvent = "compaction_started"
	EventCompactionEnded      AgentEvent = "compaction_ended"
	EventAutoRetryStarted     AgentEvent = "auto_retry_started"
	EventAutoRetryEnded       AgentEvent = "auto_retry_ended"
)

// TurnStatus describes how a turn ended.
type TurnStatus string

const (
	TurnStatusCompleted     TurnStatus = "completed"
	TurnStatusFailed        TurnStatus = "failed"
	TurnStatusTimeout       TurnStatus = "timeout"
	TurnStatusExit          TurnStatus = "exit"
	TurnStatusInputRequired TurnStatus = "input_required"
	TurnStatusCancelled     TurnStatus = "cancelled"
)

// AgentUpdate is a structured event emitted by the codex client back to the
// orchestrator.
type AgentUpdate struct {
	Event     AgentEvent     `json:"event"`
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
