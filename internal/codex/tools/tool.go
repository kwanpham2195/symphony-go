package tools

import "context"

// Tool is the interface for dynamic client-side tools advertised to the
// codex app-server session. ToolResult and ContentItem are defined in the
// parent codex package.
type Tool interface {
	Name() string
	Spec() map[string]any
	Execute(ctx context.Context, args any) ToolResult
}

// ToolResult is the outcome of a tool execution.
type ToolResult struct {
	Success      bool          `json:"success"`
	Output       string        `json:"output"`
	ContentItems []ContentItem `json:"contentItems"`
}

// ContentItem is a single content item in a tool result.
type ContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
