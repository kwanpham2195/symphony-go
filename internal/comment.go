package internal

import "time"

// Comment is a normalized tracker comment used by comment-triggered dispatch.
type Comment struct {
	ID        string    `json:"id"`
	Body      string    `json:"body"`
	IssueID   string    `json:"issue_id"`
	UserID    string    `json:"user_id"`
	UserName  string    `json:"user_name"`
	BotActor  bool      `json:"bot_actor,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	ParentID  string    `json:"parent_id,omitempty"`
}
