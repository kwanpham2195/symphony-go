package internal

// Workspace represents a per-issue filesystem workspace.
type Workspace struct {
	Path         string `json:"path"`
	WorkspaceKey string `json:"workspace_key"`
	CreatedNow   bool   `json:"created_now"`
}
