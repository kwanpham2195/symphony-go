package internal

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
