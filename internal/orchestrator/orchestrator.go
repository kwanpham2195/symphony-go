// Package orchestrator implements the Symphony scheduling loop.
//
// It owns dispatch, retry, reconciliation, and runtime state. It depends on
// interfaces for tracker, workspace, and agent runner so tests can use
// deterministic fakes.
package orchestrator

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/domain"
)

const (
	continuationRetryDelay = 1 * time.Second
	failureRetryBase       = 10 * time.Second
)

// Tracker is the issue tracker read interface.
type Tracker interface {
	FetchCandidateIssues(ctx context.Context) ([]domain.Issue, error)
	FetchIssuesByStates(ctx context.Context, states []string) ([]domain.Issue, error)
	FetchIssueStatesByIDs(ctx context.Context, ids []string) ([]domain.Issue, error)
}

// WorkspaceManager handles workspace lifecycle.
type WorkspaceManager interface {
	CreateForIssue(ctx context.Context, issue domain.Issue) (domain.Workspace, error)
	RemoveIssueWorkspace(ctx context.Context, identifier string) error
	RunBeforeRunHook(ctx context.Context, ws domain.Workspace, issue domain.Issue) error
	RunAfterRunHook(ctx context.Context, ws domain.Workspace, issue domain.Issue)
}

// AgentRunner runs a codex session for an issue.
type AgentRunner interface {
	Run(ctx context.Context, issue domain.Issue, attempt *int, updates chan<- domain.AgentUpdate) error
}

// runningEntry tracks an active worker.
type runningEntry struct {
	Issue              domain.Issue
	Attempt            *int
	StartedAt          time.Time
	SessionID          string
	LastCodexEvent     string
	LastCodexTimestamp *time.Time
	CodexInputTokens   int
	CodexOutputTokens  int
	CodexTotalTokens   int
	TurnCount          int
	WorkerHost         string // empty for local
	cancel             context.CancelFunc
}

// retryEntry tracks a scheduled retry.
type retryEntry struct {
	IssueID    string
	Identifier string
	Attempt    int
	DueAt      time.Time
	Error      string
	Timer      *time.Timer
}

// Deps holds the orchestrator dependencies.
type Deps struct {
	Tracker    Tracker
	Workspace  WorkspaceManager
	Runner     AgentRunner
	Config     *config.Config
	Logger     *slog.Logger
	WorkerPool *WorkerPool // optional; nil = local only
}

// WorkerPool tracks SSH host capacity. Nil means local-only mode.
type WorkerPool struct {
	hosts                []string
	maxConcurrentPerHost int
	mu                   sync.Mutex
	running              map[string]int // host -> count
}

// NewWorkerPool creates a pool from config. Returns nil if no SSH hosts.
func NewWorkerPool(hosts []string, maxPerHost int) *WorkerPool {
	if len(hosts) == 0 {
		return nil
	}
	return &WorkerPool{
		hosts:                hosts,
		maxConcurrentPerHost: maxPerHost,
		running:              make(map[string]int),
	}
}

func (p *WorkerPool) selectHost(preferred string) (string, bool) {
	if p == nil {
		return "", true // local mode
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try preferred host first
	if preferred != "" {
		for _, h := range p.hosts {
			if h == preferred && p.hostHasCapacity(h) {
				return h, true
			}
		}
	}

	// Least-loaded with capacity
	var best string
	bestCount := -1
	for _, h := range p.hosts {
		if !p.hostHasCapacity(h) {
			continue
		}
		c := p.running[h]
		if best == "" || c < bestCount {
			best = h
			bestCount = c
		}
	}
	if best == "" {
		return "", false
	}
	return best, true
}

func (p *WorkerPool) acquire(host string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.running[host]++
	p.mu.Unlock()
}

func (p *WorkerPool) release(host string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.running[host] > 0 {
		p.running[host]--
	}
	p.mu.Unlock()
}

func (p *WorkerPool) hostHasCapacity(host string) bool {
	if p.maxConcurrentPerHost <= 0 {
		return true
	}
	return p.running[host] < p.maxConcurrentPerHost
}

func (p *WorkerPool) hasCapacity() bool {
	if p == nil {
		return true
	}
	_, ok := p.selectHost("")
	return ok
}

// Orchestrator owns scheduling state.
type Orchestrator struct {
	deps   Deps
	logger *slog.Logger

	mu            sync.Mutex
	running       map[string]*runningEntry // issue_id -> entry
	claimed       map[string]bool          // issue_id -> true
	retryAttempts map[string]*retryEntry   // issue_id -> entry
	completed     map[string]bool          // issue_id -> true
	codexTotals   domain.CodexTotals
	rateLimits    map[string]any
	endedSeconds  float64
	stopCh        chan struct{}
	stopped       bool
	ctx           context.Context // lifecycle context; cancelled on shutdown
	cancelCtx     context.CancelFunc
}

// New creates an orchestrator from deps.
func New(deps Deps) *Orchestrator {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Orchestrator{
		deps:          deps,
		logger:        deps.Logger,
		running:       make(map[string]*runningEntry),
		claimed:       make(map[string]bool),
		retryAttempts: make(map[string]*retryEntry),
		completed:     make(map[string]bool),
		stopCh:        make(chan struct{}),
		ctx:           ctx,
		cancelCtx:     cancel,
	}
}

// Start runs the poll loop until ctx is cancelled.
func (o *Orchestrator) Start(ctx context.Context) error {
	o.logger.Info("orchestrator starting",
		"poll_interval_ms", o.deps.Config.Polling.IntervalMS,
		"max_concurrent_agents", o.deps.Config.Agent.MaxConcurrentAgents,
	)

	o.startupCleanup(ctx)

	// Immediate first tick
	o.Tick(ctx)

	interval := time.Duration(o.deps.Config.Polling.IntervalMS) * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			o.logger.Info("orchestrator stopping")
			o.stopAll()
			return ctx.Err()
		case <-o.stopCh:
			o.logger.Info("orchestrator stopped")
			return nil
		case <-ticker.C:
			o.Tick(ctx)
			// Re-apply interval in case config changed
			newInterval := time.Duration(o.deps.Config.Polling.IntervalMS) * time.Millisecond
			if newInterval != interval {
				ticker.Reset(newInterval)
				interval = newInterval
			}
		}
	}
}

// Tick runs one poll cycle: reconcile, validate, fetch, dispatch.
func (o *Orchestrator) Tick(ctx context.Context) {
	o.reconcile(ctx)

	if err := o.deps.Config.Validate(); err != nil {
		o.logger.Error("dispatch skipped: config validation failed", "error", err)
		return
	}

	issues, err := o.deps.Tracker.FetchCandidateIssues(ctx)
	if err != nil {
		o.logger.Error("dispatch skipped: fetch candidate issues failed", "error", err)
		return
	}

	sorted := sortForDispatch(issues)
	o.dispatch(ctx, sorted)
}

// Snapshot returns a point-in-time view of the orchestrator state.
func (o *Orchestrator) Snapshot() domain.Snapshot {
	o.mu.Lock()
	defer o.mu.Unlock()

	now := time.Now()
	running := make([]domain.RunningRow, 0, len(o.running))
	for _, e := range o.running {
		running = append(running, domain.RunningRow{
			IssueID:         e.Issue.ID,
			IssueIdentifier: e.Issue.Identifier,
			SessionID:       e.SessionID,
			TurnCount:       e.TurnCount,
			StartedAt:       e.StartedAt,
		})
	}

	retrying := make([]domain.RetryRow, 0, len(o.retryAttempts))
	for _, r := range o.retryAttempts {
		retrying = append(retrying, domain.RetryRow{
			IssueID:    r.IssueID,
			Identifier: r.Identifier,
			Attempt:    r.Attempt,
			DueAt:      r.DueAt,
			Error:      r.Error,
		})
	}

	// Compute total seconds: ended + active
	totalSeconds := o.endedSeconds
	for _, e := range o.running {
		totalSeconds += now.Sub(e.StartedAt).Seconds()
	}

	return domain.Snapshot{
		Running:  running,
		Retrying: retrying,
		CodexTotals: domain.CodexTotals{
			InputTokens:    o.codexTotals.InputTokens,
			OutputTokens:   o.codexTotals.OutputTokens,
			TotalTokens:    o.codexTotals.TotalTokens,
			SecondsRunning: totalSeconds,
		},
		RateLimits: o.rateLimits,
	}
}

// --- internal ---

func (o *Orchestrator) startupCleanup(ctx context.Context) {
	issues, err := o.deps.Tracker.FetchIssuesByStates(ctx, o.deps.Config.Tracker.TerminalStates)
	if err != nil {
		o.logger.Warn("startup terminal cleanup skipped", "error", err)
		return
	}
	for _, issue := range issues {
		if issue.Identifier != "" {
			if rmErr := o.deps.Workspace.RemoveIssueWorkspace(ctx, issue.Identifier); rmErr != nil {
				o.logger.Warn("cleanup workspace failed",
					"issue_identifier", issue.Identifier,
					"error", rmErr,
				)
			}
		}
	}
}

func (o *Orchestrator) reconcile(ctx context.Context) {
	o.reconcileStalls()
	o.reconcileStates(ctx)
}

func (o *Orchestrator) reconcileStalls() {
	stallTimeoutMS := o.deps.Config.Codex.StallTimeoutMS
	if stallTimeoutMS <= 0 {
		return
	}

	o.mu.Lock()
	defer o.mu.Unlock()

	now := time.Now()
	timeout := time.Duration(stallTimeoutMS) * time.Millisecond

	for issueID, entry := range o.running {
		var lastActivity time.Time
		if entry.LastCodexTimestamp != nil {
			lastActivity = *entry.LastCodexTimestamp
		} else {
			lastActivity = entry.StartedAt
		}

		elapsed := now.Sub(lastActivity)
		if elapsed > timeout {
			o.logger.Warn("issue stalled",
				"issue_id", issueID,
				"issue_identifier", entry.Issue.Identifier,
				"session_id", entry.SessionID,
				"elapsed_ms", elapsed.Milliseconds(),
			)
			o.terminateRunningLocked(issueID, false)
			nextAttempt := nextAttemptFrom(entry.Attempt)
			o.scheduleRetryLocked(issueID, entry.Issue.Identifier, nextAttempt,
				fmt.Sprintf("stalled for %dms", elapsed.Milliseconds()))
		}
	}
}

func (o *Orchestrator) reconcileStates(ctx context.Context) {
	o.mu.Lock()
	ids := make([]string, 0, len(o.running))
	for id := range o.running {
		ids = append(ids, id)
	}
	o.mu.Unlock()

	if len(ids) == 0 {
		return
	}

	issues, err := o.deps.Tracker.FetchIssueStatesByIDs(ctx, ids)
	if err != nil {
		o.logger.Debug("state refresh failed; keeping active workers", "error", err)
		return
	}

	issueMap := make(map[string]domain.Issue, len(issues))
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	activeSet := stateSet(o.deps.Config.Tracker.ActiveStates)
	terminalSet := stateSet(o.deps.Config.Tracker.TerminalStates)

	o.mu.Lock()
	defer o.mu.Unlock()

	for _, id := range ids {
		entry, ok := o.running[id]
		if !ok {
			continue
		}

		issue, found := issueMap[id]
		if !found {
			o.logger.Info("issue no longer visible; stopping agent",
				"issue_id", id,
				"issue_identifier", entry.Issue.Identifier,
			)
			o.terminateRunningLocked(id, false)
			continue
		}

		normState := strings.ToLower(strings.TrimSpace(issue.State))

		switch {
		case terminalSet[normState]:
			o.logger.Info("issue terminal; stopping agent and cleaning workspace",
				"issue_id", id,
				"issue_identifier", issue.Identifier,
				"state", issue.State,
			)
			o.terminateRunningLocked(id, true)

		case activeSet[normState]:
			// Still active: update issue snapshot
			entry.Issue = issue

		default:
			o.logger.Info("issue non-active; stopping agent",
				"issue_id", id,
				"issue_identifier", issue.Identifier,
				"state", issue.State,
			)
			o.terminateRunningLocked(id, false)
		}
	}
}

func (o *Orchestrator) dispatch(ctx context.Context, issues []domain.Issue) {
	activeSet := stateSet(o.deps.Config.Tracker.ActiveStates)
	terminalSet := stateSet(o.deps.Config.Tracker.TerminalStates)

	for _, issue := range issues {
		if !o.shouldDispatch(issue, activeSet, terminalSet) {
			continue
		}
		o.dispatchIssue(ctx, issue, nil)
	}
}

func (o *Orchestrator) shouldDispatch(issue domain.Issue, activeSet, terminalSet map[string]bool) bool {
	o.mu.Lock()
	defer o.mu.Unlock()

	if issue.ID == "" || issue.Identifier == "" || issue.Title == "" || issue.State == "" {
		return false
	}

	normState := strings.ToLower(strings.TrimSpace(issue.State))

	if !activeSet[normState] || terminalSet[normState] {
		return false
	}

	if o.claimed[issue.ID] {
		return false
	}
	if _, running := o.running[issue.ID]; running {
		return false
	}

	if o.availableSlots() <= 0 {
		return false
	}

	if !o.stateSlotAvailable(issue) {
		return false
	}

	// Worker pool capacity check
	if o.deps.WorkerPool != nil && !o.deps.WorkerPool.hasCapacity() {
		return false
	}

	// Todo blocker rule
	if strings.ToLower(strings.TrimSpace(issue.State)) == "todo" {
		for _, b := range issue.BlockedBy {
			bState := strings.ToLower(strings.TrimSpace(b.State))
			if !terminalSet[bState] {
				return false
			}
		}
	}

	return true
}

func (o *Orchestrator) stateSlotAvailable(issue domain.Issue) bool {
	normState := strings.ToLower(strings.TrimSpace(issue.State))

	limit, hasLimit := o.deps.Config.Agent.MaxConcurrentAgentsByState[normState]
	if !hasLimit {
		return true // no per-state limit
	}

	count := 0
	for _, e := range o.running {
		if strings.ToLower(strings.TrimSpace(e.Issue.State)) == normState {
			count++
		}
	}
	return count < limit
}

func (o *Orchestrator) availableSlots() int {
	max := o.deps.Config.Agent.MaxConcurrentAgents
	used := len(o.running)
	if max-used > 0 {
		return max - used
	}
	return 0
}

func (o *Orchestrator) dispatchIssue(ctx context.Context, issue domain.Issue, attempt *int) {
	// Select worker host
	var workerHost string
	if o.deps.WorkerPool != nil {
		var ok bool
		workerHost, ok = o.deps.WorkerPool.selectHost("")
		if !ok {
			o.logger.Debug("no SSH worker capacity", "issue_identifier", issue.Identifier)
			return
		}
		o.deps.WorkerPool.acquire(workerHost)
	}

	o.mu.Lock()
	o.claimed[issue.ID] = true
	// Remove from retry if being dispatched
	if r, ok := o.retryAttempts[issue.ID]; ok {
		if r.Timer != nil {
			r.Timer.Stop()
		}
		delete(o.retryAttempts, issue.ID)
	}

	workerCtx, cancel := context.WithCancel(ctx)
	entry := &runningEntry{
		Issue:      issue,
		Attempt:    attempt,
		StartedAt:  time.Now(),
		WorkerHost: workerHost,
		cancel:     cancel,
	}
	o.running[issue.ID] = entry
	o.mu.Unlock()

	o.logger.Info("dispatching issue",
		"issue_id", issue.ID,
		"issue_identifier", issue.Identifier,
		"attempt", attempt,
		"worker_host", workerHost,
	)

	// Run agent in goroutine
	go o.runAgent(workerCtx, issue, attempt, workerHost)
}

func (o *Orchestrator) runAgent(ctx context.Context, issue domain.Issue, attempt *int, workerHost string) {
	// Release worker pool slot when done
	defer func() {
		if o.deps.WorkerPool != nil && workerHost != "" {
			o.deps.WorkerPool.release(workerHost)
		}
	}()

	updates := make(chan domain.AgentUpdate, 64)

	// Process updates in background
	go func() {
		for update := range updates {
			o.handleAgentUpdate(issue.ID, update)
		}
	}()

	err := o.deps.Runner.Run(ctx, issue, attempt, updates)
	close(updates)

	o.mu.Lock()
	defer o.mu.Unlock()

	entry, ok := o.running[issue.ID]
	if !ok {
		return // was cancelled/removed
	}

	// Record runtime
	o.endedSeconds += time.Since(entry.StartedAt).Seconds()

	delete(o.running, issue.ID)

	if err == nil {
		// Normal exit: schedule continuation retry
		o.logger.Info("agent completed; scheduling continuation check",
			"issue_id", issue.ID,
			"issue_identifier", issue.Identifier,
		)
		o.completed[issue.ID] = true
		o.scheduleRetryLocked(issue.ID, issue.Identifier, 1, "")
	} else {
		// Abnormal exit: schedule exponential backoff retry
		o.logger.Warn("agent failed; scheduling retry",
			"issue_id", issue.ID,
			"issue_identifier", issue.Identifier,
			"error", err,
		)
		nextAttempt := nextAttemptFrom(attempt)
		o.scheduleRetryLocked(issue.ID, issue.Identifier, nextAttempt, err.Error())
	}
}

func (o *Orchestrator) handleAgentUpdate(issueID string, update domain.AgentUpdate) {
	o.mu.Lock()
	defer o.mu.Unlock()

	entry, ok := o.running[issueID]
	if !ok {
		return
	}

	entry.LastCodexEvent = update.Event
	now := update.Timestamp
	entry.LastCodexTimestamp = &now

	if update.SessionID != "" {
		if entry.SessionID == "" {
			// First session event: this is turn 1
			entry.TurnCount = 1
		} else if update.SessionID != entry.SessionID {
			// New turn started
			entry.TurnCount++
		}
		entry.SessionID = update.SessionID
	}

	if update.Usage != nil {
		entry.CodexInputTokens += update.Usage.InputTokens
		entry.CodexOutputTokens += update.Usage.OutputTokens
		entry.CodexTotalTokens += update.Usage.TotalTokens
		o.codexTotals.InputTokens += update.Usage.InputTokens
		o.codexTotals.OutputTokens += update.Usage.OutputTokens
		o.codexTotals.TotalTokens += update.Usage.TotalTokens
	}
}

func (o *Orchestrator) scheduleRetryLocked(issueID, identifier string, attempt int, errMsg string) {
	// Cancel existing retry
	if old, ok := o.retryAttempts[issueID]; ok {
		if old.Timer != nil {
			old.Timer.Stop()
		}
	}

	delay := retryDelay(attempt, errMsg == "", o.deps.Config.Agent.MaxRetryBackoffMS)
	dueAt := time.Now().Add(delay)

	entry := &retryEntry{
		IssueID:    issueID,
		Identifier: identifier,
		Attempt:    attempt,
		DueAt:      dueAt,
		Error:      errMsg,
	}

	entry.Timer = time.AfterFunc(delay, func() {
		o.handleRetry(issueID, attempt)
	})

	o.retryAttempts[issueID] = entry

	if errMsg != "" {
		o.logger.Warn("retry scheduled",
			"issue_id", issueID,
			"issue_identifier", identifier,
			"attempt", attempt,
			"delay_ms", delay.Milliseconds(),
			"error", errMsg,
		)
	} else {
		o.logger.Info("continuation check scheduled",
			"issue_id", issueID,
			"issue_identifier", identifier,
			"delay_ms", delay.Milliseconds(),
		)
	}
}

func (o *Orchestrator) handleRetry(issueID string, attempt int) {
	o.mu.Lock()
	if o.stopped {
		o.mu.Unlock()
		return
	}
	retry, ok := o.retryAttempts[issueID]
	if !ok || retry.Attempt != attempt {
		o.mu.Unlock()
		return
	}
	identifier := retry.Identifier
	delete(o.retryAttempts, issueID)
	o.mu.Unlock()

	ctx := o.ctx

	// Fetch candidates to check if issue is still active
	issues, err := o.deps.Tracker.FetchCandidateIssues(ctx)
	if err != nil {
		o.logger.Warn("retry poll failed; rescheduling",
			"issue_id", issueID,
			"issue_identifier", identifier,
			"error", err,
		)
		o.mu.Lock()
		o.scheduleRetryLocked(issueID, identifier, attempt+1, "retry poll failed: "+err.Error())
		o.mu.Unlock()
		return
	}

	var found *domain.Issue
	for i := range issues {
		if issues[i].ID == issueID {
			found = &issues[i]
			break
		}
	}

	if found == nil {
		o.logger.Info("issue no longer active; releasing claim",
			"issue_id", issueID,
			"issue_identifier", identifier,
		)
		o.mu.Lock()
		delete(o.claimed, issueID)
		o.mu.Unlock()
		return
	}

	// Check if slots available
	o.mu.Lock()
	hasSlots := o.availableSlots() > 0
	o.mu.Unlock()

	if hasSlots {
		o.dispatchIssue(ctx, *found, &attempt)
	} else {
		o.mu.Lock()
		o.scheduleRetryLocked(issueID, identifier, attempt+1, "no available orchestrator slots")
		o.mu.Unlock()
	}
}

// terminateRunningLocked terminates a running issue. Must hold o.mu.
func (o *Orchestrator) terminateRunningLocked(issueID string, cleanupWorkspace bool) {
	entry, ok := o.running[issueID]
	if !ok {
		delete(o.claimed, issueID)
		return
	}

	// Record runtime
	o.endedSeconds += time.Since(entry.StartedAt).Seconds()

	if entry.cancel != nil {
		entry.cancel()
	}

	if cleanupWorkspace {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if rmErr := o.deps.Workspace.RemoveIssueWorkspace(ctx, entry.Issue.Identifier); rmErr != nil {
				o.logger.Warn("workspace cleanup failed",
					"issue_identifier", entry.Issue.Identifier,
					"error", rmErr,
				)
			}
		}()
	}

	delete(o.running, issueID)
	delete(o.claimed, issueID)
	if r, ok := o.retryAttempts[issueID]; ok {
		if r.Timer != nil {
			r.Timer.Stop()
		}
		delete(o.retryAttempts, issueID)
	}
}

func (o *Orchestrator) stopAll() {
	o.mu.Lock()
	defer o.mu.Unlock()

	for id, entry := range o.running {
		if entry.cancel != nil {
			entry.cancel()
		}
		delete(o.running, id)
	}
	for id, r := range o.retryAttempts {
		if r.Timer != nil {
			r.Timer.Stop()
		}
		delete(o.retryAttempts, id)
	}
	o.stopped = true
	o.cancelCtx()
}

// --- sorting ---

func sortForDispatch(issues []domain.Issue) []domain.Issue {
	sorted := make([]domain.Issue, len(issues))
	copy(sorted, issues)
	sort.SliceStable(sorted, func(i, j int) bool {
		pi := priorityRank(sorted[i].Priority)
		pj := priorityRank(sorted[j].Priority)
		if pi != pj {
			return pi < pj
		}
		ti := createdAtKey(sorted[i].CreatedAt)
		tj := createdAtKey(sorted[j].CreatedAt)
		if ti != tj {
			return ti < tj
		}
		return sorted[i].Identifier < sorted[j].Identifier
	})
	return sorted
}

func priorityRank(p *int) int {
	if p == nil {
		return 5
	}
	if *p >= 1 && *p <= 4 {
		return *p
	}
	return 5
}

func createdAtKey(t *time.Time) int64 {
	if t == nil {
		return math.MaxInt64
	}
	return t.UnixMicro()
}

// --- helpers ---

func stateSet(states []string) map[string]bool {
	m := make(map[string]bool, len(states))
	for _, s := range states {
		norm := strings.ToLower(strings.TrimSpace(s))
		if norm != "" {
			m[norm] = true
		}
	}
	return m
}

func retryDelay(attempt int, isContinuation bool, maxBackoffMS int) time.Duration {
	if isContinuation && attempt == 1 {
		return continuationRetryDelay
	}
	power := attempt - 1
	if power > 10 {
		power = 10
	}
	delay := failureRetryBase * time.Duration(1<<power)
	maxDelay := time.Duration(maxBackoffMS) * time.Millisecond
	if delay > maxDelay {
		delay = maxDelay
	}
	return delay
}

func nextAttemptFrom(attempt *int) int {
	if attempt != nil && *attempt > 0 {
		return *attempt + 1
	}
	return 1
}
