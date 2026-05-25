package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

// --- Fakes ---

type fakeTracker struct {
	mu         sync.Mutex
	candidates []internal.Issue
	byStates   []internal.Issue
	byIDs      []internal.Issue
	fetchErr   error
}

func (f *fakeTracker) FetchCandidateIssues(_ context.Context) ([]internal.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	return f.candidates, nil
}

func (f *fakeTracker) FetchIssuesByStates(_ context.Context, _ []string) ([]internal.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.byStates, nil
}

func (f *fakeTracker) FetchIssueStatesByIDs(_ context.Context, ids []string) ([]internal.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fetchErr != nil {
		return nil, f.fetchErr
	}
	// Return matching issues from byIDs or candidates
	source := f.byIDs
	if source == nil {
		source = f.candidates
	}
	var out []internal.Issue
	for _, id := range ids {
		for _, issue := range source {
			if issue.ID == id {
				out = append(out, issue)
			}
		}
	}
	return out, nil
}

func (f *fakeTracker) FetchRecentComments(_ context.Context, _ []string, _ time.Time) (map[string][]internal.Comment, error) {
	return nil, nil
}

type fakeWorkspace struct {
	mu      sync.Mutex
	created []string
	removed []string
	hookErr error
}

func (f *fakeWorkspace) CreateForIssue(_ context.Context, issue internal.Issue) (internal.Workspace, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, issue.Identifier)
	return internal.Workspace{Path: "/tmp/ws/" + issue.Identifier, WorkspaceKey: issue.Identifier, CreatedNow: true}, nil
}

func (f *fakeWorkspace) RemoveIssueWorkspace(_ context.Context, identifier string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, identifier)
	return nil
}

func (f *fakeWorkspace) RunBeforeRunHook(_ context.Context, _ internal.Workspace, _ internal.Issue) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hookErr
}

func (f *fakeWorkspace) RunAfterRunHook(_ context.Context, _ internal.Workspace, _ internal.Issue) {}

type fakeRunner struct {
	mu      sync.Mutex
	runs    []string // issue IDs
	runErr  error
	delay   time.Duration
	blockCh chan struct{} // if non-nil, blocks until closed (after sending update)
}

func (f *fakeRunner) Run(ctx context.Context, issue internal.Issue, _ *int, updates chan<- internal.AgentUpdate) error {
	f.mu.Lock()
	f.runs = append(f.runs, issue.ID)
	delay := f.delay
	runErr := f.runErr
	blockCh := f.blockCh
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	updates <- internal.AgentUpdate{
		Event:     internal.EventSessionStarted,
		Timestamp: time.Now().UTC(),
		SessionID: "test-session",
	}

	if blockCh != nil {
		select {
		case <-blockCh:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if runErr != nil {
		return runErr
	}
	return nil
}

func testCfg() *config.Config {
	return &config.Config{
		Tracker: config.TrackerConfig{
			Kind:           "linear",
			APIKey:         "tok",
			ProjectSlug:    "test",
			ActiveStates:   []string{"Todo", "In Progress"},
			TerminalStates: []string{"Done", "Closed", "Cancelled"},
		},
		Polling:   config.PollingConfig{IntervalMS: 30000},
		Workspace: config.WorkspaceConfig{Root: "/tmp/test_ws"},
		Agent: config.AgentConfig{
			MaxConcurrentAgents:        3,
			MaxTurns:                   20,
			MaxRetryBackoffMS:          300000,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Codex: config.CodexConfig{
			Command:        "codex app-server",
			TurnTimeoutMS:  3600000,
			ReadTimeoutMS:  5000,
			StallTimeoutMS: 300000,
		},
	}
}

func intPtr(n int) *int { return &n }

func makeIssue(id, identifier, state string, priority *int, createdAt *time.Time) internal.Issue {
	return internal.Issue{
		ID:         id,
		Identifier: identifier,
		Title:      identifier + " title",
		State:      state,
		Priority:   priority,
		CreatedAt:  createdAt,
	}
}

// --- Sorting tests ---

func TestSortForDispatch(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)

	issues := []internal.Issue{
		makeIssue("c", "C-3", "Todo", intPtr(3), &t1),
		makeIssue("a", "A-1", "Todo", intPtr(1), &t2),
		makeIssue("b", "B-1", "Todo", intPtr(1), &t1),
		makeIssue("d", "D-0", "Todo", nil, nil),
	}

	sorted := sortForDispatch(issues)

	// Priority 1, oldest first
	if sorted[0].ID != "b" {
		t.Errorf("first = %q, want b (p1, oldest)", sorted[0].ID)
	}
	if sorted[1].ID != "a" {
		t.Errorf("second = %q, want a (p1, newer)", sorted[1].ID)
	}
	if sorted[2].ID != "c" {
		t.Errorf("third = %q, want c (p3)", sorted[2].ID)
	}
	if sorted[3].ID != "d" {
		t.Errorf("fourth = %q, want d (no priority)", sorted[3].ID)
	}
}

// --- Dispatch tests ---

func TestTick_DispatchesEligibleIssues(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
			makeIssue("id-2", "SYM-2", "In Progress", intPtr(2), nil),
		},
	}
	ws := &fakeWorkspace{}
	runner := &fakeRunner{}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: ws,
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())

	// Give goroutines time to start
	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.runs) != 2 {
		t.Fatalf("expected 2 runs, got %d: %v", len(runner.runs), runner.runs)
	}
}

func TestTick_RespectsMaxConcurrency(t *testing.T) {
	cfg := testCfg()
	cfg.Agent.MaxConcurrentAgents = 1

	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
			makeIssue("id-2", "SYM-2", "Todo", intPtr(2), nil),
		},
	}
	runner := &fakeRunner{delay: 200 * time.Millisecond}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    cfg,
	})

	o.Tick(context.Background())

	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	runCount := len(runner.runs)
	runner.mu.Unlock()

	if runCount != 1 {
		t.Errorf("expected 1 run (max concurrency = 1), got %d", runCount)
	}
}

func TestTick_NoDuplicateDispatch(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	runner := &fakeRunner{delay: 500 * time.Millisecond}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)
	o.Tick(context.Background()) // Second tick should not re-dispatch

	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.runs) != 1 {
		t.Errorf("expected 1 run (no dup), got %d", len(runner.runs))
	}
}

func TestTick_SkipsTerminalIssues(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Done", intPtr(1), nil),
		},
	}
	runner := &fakeRunner{}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.runs) != 0 {
		t.Errorf("expected 0 runs for terminal issues, got %d", len(runner.runs))
	}
}

func TestTick_TodoBlockerRule(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			{
				ID:         "id-1",
				Identifier: "SYM-1",
				Title:      "Blocked",
				State:      "Todo",
				Priority:   intPtr(1),
				BlockedBy: []internal.Blocker{
					{ID: "blocker-1", State: "In Progress"}, // non-terminal
				},
			},
		},
	}
	runner := &fakeRunner{}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.runs) != 0 {
		t.Errorf("expected 0 runs (blocked Todo), got %d", len(runner.runs))
	}
}

func TestTick_TodoBlockerTerminalAllowed(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			{
				ID:         "id-1",
				Identifier: "SYM-1",
				Title:      "Unblocked",
				State:      "Todo",
				Priority:   intPtr(1),
				BlockedBy: []internal.Blocker{
					{ID: "blocker-1", State: "Done"}, // terminal
				},
			},
		},
	}
	runner := &fakeRunner{}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())
	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.runs) != 1 {
		t.Errorf("expected 1 run (all blockers terminal), got %d", len(runner.runs))
	}
}

func TestTick_PerStateConcurrency(t *testing.T) {
	cfg := testCfg()
	cfg.Agent.MaxConcurrentAgentsByState = map[string]int{"todo": 1}

	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
			makeIssue("id-2", "SYM-2", "Todo", intPtr(2), nil),
		},
	}
	runner := &fakeRunner{delay: 200 * time.Millisecond}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    cfg,
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	runCount := len(runner.runs)
	runner.mu.Unlock()

	if runCount != 1 {
		t.Errorf("expected 1 run (per-state limit todo=1), got %d", runCount)
	}
}

// --- Reconciliation tests ---

func TestReconcile_TerminalStopsAndCleansWorkspace(t *testing.T) {
	cfg := testCfg()
	cfg.Codex.StallTimeoutMS = 0 // disable stall

	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	ws := &fakeWorkspace{}
	runner := &fakeRunner{delay: 2 * time.Second}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: ws,
		Runner:    runner,
		Config:    cfg,
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	// Now change tracker state to terminal
	tracker.mu.Lock()
	tracker.candidates = []internal.Issue{
		makeIssue("id-1", "SYM-1", "Done", intPtr(1), nil),
	}
	tracker.mu.Unlock()

	o.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	o.mu.Lock()
	_, stillRunning := o.running["id-1"]
	o.mu.Unlock()

	if stillRunning {
		t.Error("expected issue to be stopped after terminal state")
	}

	// Check workspace cleanup was requested
	ws.mu.Lock()
	defer ws.mu.Unlock()
	found := false
	for _, r := range ws.removed {
		if r == "SYM-1" {
			found = true
		}
	}
	if !found {
		t.Error("expected workspace cleanup for terminal issue")
	}
}

func TestReconcile_NonActiveStopsWithoutCleanup(t *testing.T) {
	cfg := testCfg()
	cfg.Codex.StallTimeoutMS = 0

	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "In Progress", intPtr(1), nil),
		},
	}
	ws := &fakeWorkspace{}
	runner := &fakeRunner{delay: 2 * time.Second}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: ws,
		Runner:    runner,
		Config:    cfg,
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	// Change to non-active, non-terminal
	tracker.mu.Lock()
	tracker.candidates = []internal.Issue{
		makeIssue("id-1", "SYM-1", "Human Review", intPtr(1), nil),
	}
	tracker.mu.Unlock()

	o.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	o.mu.Lock()
	_, stillRunning := o.running["id-1"]
	o.mu.Unlock()

	if stillRunning {
		t.Error("expected issue to be stopped")
	}

	// Workspace should NOT be cleaned up
	ws.mu.Lock()
	defer ws.mu.Unlock()
	for _, r := range ws.removed {
		if r == "SYM-1" {
			t.Error("workspace should not be cleaned for non-active non-terminal")
		}
	}
}

// --- Retry tests ---

func TestRetryDelay_Continuation(t *testing.T) {
	d := retryDelay(1, true, 300000)
	if d != continuationRetryDelay {
		t.Errorf("got %v, want %v", d, continuationRetryDelay)
	}
}

func TestRetryDelay_Exponential(t *testing.T) {
	d1 := retryDelay(1, false, 300000)
	d2 := retryDelay(2, false, 300000)
	d3 := retryDelay(3, false, 300000)

	if d1 != 10*time.Second {
		t.Errorf("attempt 1 = %v", d1)
	}
	if d2 != 20*time.Second {
		t.Errorf("attempt 2 = %v", d2)
	}
	if d3 != 40*time.Second {
		t.Errorf("attempt 3 = %v", d3)
	}
}

func TestRetryDelay_Capped(t *testing.T) {
	d := retryDelay(20, false, 300000)
	if d != 300*time.Second {
		t.Errorf("expected cap at 300s, got %v", d)
	}
}

// --- Snapshot test ---

func TestSnapshot_Empty(t *testing.T) {
	o := New(Deps{
		Tracker:   &fakeTracker{},
		Workspace: &fakeWorkspace{},
		Runner:    &fakeRunner{},
		Config:    testCfg(),
	})

	snap := o.Snapshot()
	if len(snap.Running) != 0 {
		t.Errorf("expected 0 running, got %d", len(snap.Running))
	}
	if len(snap.Retrying) != 0 {
		t.Errorf("expected 0 retrying, got %d", len(snap.Retrying))
	}
}

// --- TurnCount test ---

func TestHandleAgentUpdate_TurnCountStartsAtOne(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	blockCh := make(chan struct{})
	runner := &fakeRunner{blockCh: blockCh}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())
	// Wait for update to be processed
	time.Sleep(100 * time.Millisecond)

	o.mu.Lock()
	entry, ok := o.running["id-1"]
	turnCount := 0
	if ok {
		turnCount = entry.TurnCount
	}
	o.mu.Unlock()

	// Unblock the runner
	close(blockCh)

	if !ok {
		t.Fatal("expected running entry for id-1")
	}
	// After first session_started event, TurnCount should be 1
	if turnCount != 1 {
		t.Errorf("TurnCount = %d after first turn, want 1", turnCount)
	}
}

// --- Shutdown safety test ---

func TestHandleRetry_RespectsShutdown(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	blockCh := make(chan struct{})
	runner := &fakeRunner{blockCh: blockCh}

	cfg := testCfg()
	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    cfg,
	})

	ctx, cancel := context.WithCancel(context.Background())
	o.Tick(ctx)
	time.Sleep(50 * time.Millisecond)

	// Simulate agent completing (unblock) -> schedules retry
	close(blockCh)
	time.Sleep(100 * time.Millisecond)

	// Verify retry is scheduled
	o.mu.Lock()
	hasRetry := len(o.retryAttempts) > 0
	o.mu.Unlock()
	if !hasRetry {
		t.Fatal("expected retry to be scheduled after completion")
	}

	// Shutdown: cancel context and stop all
	cancel()
	o.stopAll()

	// Wait for retry timer to fire (continuation delay is 1s)
	time.Sleep(1500 * time.Millisecond)

	// After shutdown, no new dispatch should have happened
	runner.mu.Lock()
	runCount := len(runner.runs)
	runner.mu.Unlock()

	// Should have exactly 1 run (the original), not 2 (no retry after shutdown)
	if runCount != 1 {
		t.Errorf("expected 1 run (no retry after shutdown), got %d", runCount)
	}
}

func TestHandleRetry_StoppedGuard(t *testing.T) {
	// Directly test that handleRetry does not dispatch after stopped=true.
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	runner := &fakeRunner{}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	// Plant a retry entry manually
	o.mu.Lock()
	o.retryAttempts["id-1"] = &RetryEntry{
		IssueID:    "id-1",
		Identifier: "SYM-1",
		Attempt:    1,
		DueAt:      time.Now(),
	}
	// Mark as stopped
	o.stopped = true
	o.mu.Unlock()

	// Simulate the timer firing after shutdown
	o.handleRetry("id-1", 1)

	time.Sleep(100 * time.Millisecond)

	runner.mu.Lock()
	runCount := len(runner.runs)
	runner.mu.Unlock()

	if runCount != 0 {
		t.Errorf("expected 0 runs after stopped, got %d", runCount)
	}
}

func TestAgentFailure_SchedulesRetry(t *testing.T) {
	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	runner := &fakeRunner{runErr: fmt.Errorf("model_error")}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    testCfg(),
	})

	o.Tick(context.Background())
	time.Sleep(200 * time.Millisecond)

	o.mu.Lock()
	defer o.mu.Unlock()

	if _, ok := o.retryAttempts["id-1"]; !ok {
		t.Error("expected retry to be scheduled after failure")
	}
	if _, ok := o.running["id-1"]; ok {
		t.Error("issue should not be running after failure")
	}
}

// --- Tracker error does not crash ---

func TestTick_TrackerError_SkipsDispatch(t *testing.T) {
	tracker := &fakeTracker{fetchErr: fmt.Errorf("network error")}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    &fakeRunner{},
		Config:    testCfg(),
	})

	// Should not panic
	o.Tick(context.Background())
}

// --- Config validation error skips dispatch ---

func TestTick_InvalidConfig_SkipsDispatch(t *testing.T) {
	cfg := testCfg()
	cfg.Tracker.Kind = "" // invalid

	tracker := &fakeTracker{
		candidates: []internal.Issue{
			makeIssue("id-1", "SYM-1", "Todo", intPtr(1), nil),
		},
	}
	runner := &fakeRunner{}

	o := New(Deps{
		Tracker:   tracker,
		Workspace: &fakeWorkspace{},
		Runner:    runner,
		Config:    cfg,
	})

	o.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	runner.mu.Lock()
	defer runner.mu.Unlock()
	if len(runner.runs) != 0 {
		t.Errorf("expected 0 runs with invalid config, got %d", len(runner.runs))
	}
}
