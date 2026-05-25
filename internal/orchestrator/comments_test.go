package orchestrator

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

// --- comment-specific fakes ---

type commentTracker struct {
	fakeTracker
	comments map[string][]internal.Comment
}

func (ct *commentTracker) FetchRecentComments(_ context.Context, _ []string, _ time.Time) (map[string][]internal.Comment, error) {
	return ct.comments, nil
}

type fakeTrackerWriter struct {
	mu           sync.Mutex
	viewerID     string
	states       map[string]string // name -> id
	transitions  []transitionCall
	transitionFn func(ctx context.Context, issueID, stateID string) error
}

type transitionCall struct {
	IssueID string
	StateID string
}

func (f *fakeTrackerWriter) ViewerID(_ context.Context) (string, error) {
	return f.viewerID, nil
}

func (f *fakeTrackerWriter) ResolveStateID(_ context.Context, name string) (string, error) {
	if id, ok := f.states[name]; ok {
		return id, nil
	}
	return "", nil
}

func (f *fakeTrackerWriter) TransitionIssueState(ctx context.Context, issueID, stateID string) error {
	f.mu.Lock()
	f.transitions = append(f.transitions, transitionCall{issueID, stateID})
	f.mu.Unlock()
	if f.transitionFn != nil {
		return f.transitionFn(ctx, issueID, stateID)
	}
	return nil
}

func commentConfig() *config.Config {
	return &config.Config{
		Tracker: config.TrackerConfig{
			ActiveStates:   []string{"In Progress", "Todo"},
			TerminalStates: []string{"Done", "Canceled"},
		},
		Polling: config.PollingConfig{IntervalMS: 100},
		Agent: config.AgentConfig{
			MaxConcurrentAgents: 5,
			MaxTurns:            5,
			MaxRetryBackoffMS:   1000,
		},
		Comments: config.CommentsConfig{
			Enabled:           true,
			PollIntervalTicks: 1, // check every tick for tests
			LookbackMS:        300000,
			ReviewState:       "In Review",
		},
	}
}

// --- tests ---

func TestCommentTrigger_DispatchesOnNewComment(t *testing.T) {
	cfg := commentConfig()
	issue := internal.Issue{
		ID:         "iss-1",
		Identifier: "CFW-10",
		Title:      "Fix tests",
		State:      "In Review",
	}

	tracker := &commentTracker{
		fakeTracker: fakeTracker{
			byStates: []internal.Issue{issue},
		},
		comments: map[string][]internal.Comment{
			"iss-1": {
				{
					ID:        "c-1",
					Body:      "Please fix the auth test",
					IssueID:   "iss-1",
					UserID:    "human-1",
					UserName:  "Alice",
					CreatedAt: time.Now(),
				},
			},
		},
	}

	writer := &fakeTrackerWriter{
		viewerID: "agent-user",
		states:   map[string]string{"In Progress": "state-ip"},
	}

	runner := &fakeRunner{}

	orch := New(Deps{
		Tracker:       tracker,
		TrackerWriter: writer,
		Workspace:     &fakeWorkspace{},
		Runner:        runner,
		Config:        cfg,
		Logger:        nil,
	})

	// Initialize comment state manually (Start() would block).
	ctx := context.Background()
	orch.commentState = initComments(ctx, commentsConfig{
		enabled:           true,
		pollIntervalTicks: 1,
		lookbackMS:        300000,
		reviewState:       "In Review",
		activeState:       "In Progress",
	}, writer, orch.logger)

	// Run one comment check.
	orch.checkComments(ctx)

	// Verify: state transition was called.
	writer.mu.Lock()
	transitions := writer.transitions
	writer.mu.Unlock()
	if len(transitions) != 1 {
		t.Fatalf("transitions = %d, want 1", len(transitions))
	}
	if transitions[0].IssueID != "iss-1" || transitions[0].StateID != "state-ip" {
		t.Errorf("transition = %+v, want iss-1 -> state-ip", transitions[0])
	}

	// Wait briefly for goroutine dispatch.
	time.Sleep(50 * time.Millisecond)

	// Verify: issue was claimed.
	orch.mu.Lock()
	claimed := orch.claimed["iss-1"]
	orch.mu.Unlock()
	if !claimed {
		t.Error("issue should be claimed")
	}
}

func TestCommentTrigger_SkipsAgentComments(t *testing.T) {
	cfg := commentConfig()
	issue := internal.Issue{
		ID:         "iss-2",
		Identifier: "CFW-11",
		Title:      "Update docs",
		State:      "In Review",
	}

	tracker := &commentTracker{
		fakeTracker: fakeTracker{
			byStates: []internal.Issue{issue},
		},
		comments: map[string][]internal.Comment{
			"iss-2": {
				{
					ID:        "c-2",
					Body:      "Agent comment",
					IssueID:   "iss-2",
					UserID:    "agent-user", // same as viewer
					UserName:  "Symphony",
					CreatedAt: time.Now(),
				},
			},
		},
	}

	writer := &fakeTrackerWriter{
		viewerID: "agent-user",
		states:   map[string]string{"In Progress": "state-ip"},
	}

	orch := New(Deps{
		Tracker:       tracker,
		TrackerWriter: writer,
		Workspace:     &fakeWorkspace{},
		Runner:        &fakeRunner{},
		Config:        cfg,
		Logger:        nil,
	})

	ctx := context.Background()
	orch.commentState = initComments(ctx, commentsConfig{
		enabled:           true,
		pollIntervalTicks: 1,
		lookbackMS:        300000,
		reviewState:       "In Review",
		activeState:       "In Progress",
	}, writer, orch.logger)

	orch.checkComments(ctx)

	// No transitions should have happened.
	writer.mu.Lock()
	transitions := writer.transitions
	writer.mu.Unlock()
	if len(transitions) != 0 {
		t.Errorf("transitions = %d, want 0 (agent comment should be skipped)", len(transitions))
	}
}

func TestCommentTrigger_SkipsBotComments(t *testing.T) {
	cfg := commentConfig()
	issue := internal.Issue{
		ID:         "iss-3",
		Identifier: "CFW-12",
		Title:      "CI report",
		State:      "In Review",
	}

	tracker := &commentTracker{
		fakeTracker: fakeTracker{
			byStates: []internal.Issue{issue},
		},
		comments: map[string][]internal.Comment{
			"iss-3": {
				{
					ID:        "c-3",
					Body:      "Build passed",
					IssueID:   "iss-3",
					BotActor:  true,
					CreatedAt: time.Now(),
				},
			},
		},
	}

	writer := &fakeTrackerWriter{
		viewerID: "agent-user",
		states:   map[string]string{"In Progress": "state-ip"},
	}

	orch := New(Deps{
		Tracker:       tracker,
		TrackerWriter: writer,
		Workspace:     &fakeWorkspace{},
		Runner:        &fakeRunner{},
		Config:        cfg,
		Logger:        nil,
	})

	ctx := context.Background()
	orch.commentState = initComments(ctx, commentsConfig{
		enabled:           true,
		pollIntervalTicks: 1,
		lookbackMS:        300000,
		reviewState:       "In Review",
		activeState:       "In Progress",
	}, writer, orch.logger)

	orch.checkComments(ctx)

	writer.mu.Lock()
	transitions := writer.transitions
	writer.mu.Unlock()
	if len(transitions) != 0 {
		t.Errorf("transitions = %d, want 0 (bot comment should be skipped)", len(transitions))
	}
}

func TestCommentTrigger_SkipsAlreadyClaimed(t *testing.T) {
	cfg := commentConfig()
	issue := internal.Issue{
		ID:         "iss-4",
		Identifier: "CFW-13",
		Title:      "Already claimed",
		State:      "In Review",
	}

	tracker := &commentTracker{
		fakeTracker: fakeTracker{
			byStates: []internal.Issue{issue},
		},
		comments: map[string][]internal.Comment{
			"iss-4": {
				{
					ID:        "c-4",
					Body:      "Review feedback",
					IssueID:   "iss-4",
					UserID:    "human-1",
					UserName:  "Alice",
					CreatedAt: time.Now(),
				},
			},
		},
	}

	writer := &fakeTrackerWriter{
		viewerID: "agent-user",
		states:   map[string]string{"In Progress": "state-ip"},
	}

	orch := New(Deps{
		Tracker:       tracker,
		TrackerWriter: writer,
		Workspace:     &fakeWorkspace{},
		Runner:        &fakeRunner{},
		Config:        cfg,
		Logger:        nil,
	})

	// Pre-claim the issue.
	orch.claimed["iss-4"] = true

	ctx := context.Background()
	orch.commentState = initComments(ctx, commentsConfig{
		enabled:           true,
		pollIntervalTicks: 1,
		lookbackMS:        300000,
		reviewState:       "In Review",
		activeState:       "In Progress",
	}, writer, orch.logger)

	orch.checkComments(ctx)

	writer.mu.Lock()
	transitions := writer.transitions
	writer.mu.Unlock()
	if len(transitions) != 0 {
		t.Errorf("transitions = %d, want 0 (already claimed)", len(transitions))
	}
}

func TestCommentTrigger_MultipleComments(t *testing.T) {
	cfg := commentConfig()
	issue := internal.Issue{
		ID:         "iss-5",
		Identifier: "CFW-14",
		Title:      "Multiple feedback",
		State:      "In Review",
	}

	tracker := &commentTracker{
		fakeTracker: fakeTracker{
			byStates: []internal.Issue{issue},
		},
		comments: map[string][]internal.Comment{
			"iss-5": {
				{
					ID: "c-5", Body: "Fix test A", IssueID: "iss-5",
					UserID: "human-1", UserName: "Alice", CreatedAt: time.Now(),
				},
				{
					ID: "c-6", Body: "Also fix test B", IssueID: "iss-5",
					UserID: "human-2", UserName: "Bob", CreatedAt: time.Now(),
				},
			},
		},
	}

	writer := &fakeTrackerWriter{
		viewerID: "agent-user",
		states:   map[string]string{"In Progress": "state-ip"},
	}

	runner := &fakeRunner{}

	orch := New(Deps{
		Tracker:       tracker,
		TrackerWriter: writer,
		Workspace:     &fakeWorkspace{},
		Runner:        runner,
		Config:        cfg,
		Logger:        nil,
	})

	ctx := context.Background()
	orch.commentState = initComments(ctx, commentsConfig{
		enabled:           true,
		pollIntervalTicks: 1,
		lookbackMS:        300000,
		reviewState:       "In Review",
		activeState:       "In Progress",
	}, writer, orch.logger)

	orch.checkComments(ctx)

	// Wait for dispatch goroutine.
	time.Sleep(100 * time.Millisecond)

	// Verify the issue was dispatched (runner received it).
	runner.mu.Lock()
	runs := runner.runs
	runner.mu.Unlock()

	if len(runs) != 1 || runs[0] != "iss-5" {
		t.Errorf("runs = %v, want [iss-5]", runs)
	}
}
