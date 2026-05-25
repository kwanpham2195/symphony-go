package tracker

import "context"

// TrackerWriter provides write operations for issue trackers. Separated from
// Tracker to keep the read interface clean and testable.
type TrackerWriter interface {
	// ViewerID returns the user ID of the API key owner. Used for anti-loop
	// filtering (skip comments authored by the agent's own API key).
	ViewerID(ctx context.Context) (string, error)

	// ResolveStateID maps a human-readable state name (e.g. "In Progress")
	// to a platform-specific ID. Returns an error if the state is not found.
	ResolveStateID(ctx context.Context, stateName string) (string, error)

	// TransitionIssueState moves an issue to the given state ID.
	TransitionIssueState(ctx context.Context, issueID string, stateID string) error
}
