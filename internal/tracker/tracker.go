// Package tracker defines the interface for issue tracker adapters.
package tracker

import (
	"context"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
)

// Tracker is the read-only interface for issue tracker adapters. Symphony is a
// scheduler/runner and tracker reader; ticket writes are handled by the coding
// agent through tools.
type Tracker interface {
	// FetchCandidateIssues returns issues in active states for the configured
	// project. Used by the orchestrator to find work to dispatch.
	FetchCandidateIssues(ctx context.Context) ([]internal.Issue, error)

	// FetchIssuesByStates returns issues in the given states for the configured
	// project. Used for startup terminal cleanup.
	FetchIssuesByStates(ctx context.Context, states []string) ([]internal.Issue, error)

	// FetchIssueStatesByIDs returns current issue data for specific IDs. Used
	// for active-run reconciliation.
	FetchIssueStatesByIDs(ctx context.Context, ids []string) ([]internal.Issue, error)

	// FetchRecentComments returns comments created after since for the given
	// issue IDs. The returned map is keyed by issue ID.
	FetchRecentComments(ctx context.Context, issueIDs []string, since time.Time) (map[string][]internal.Comment, error)
}
