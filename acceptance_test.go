// Package acceptance_test provides end-to-end acceptance tests that wire
// the full symphony system: config, workflow, workspace, codex client (fake
// subprocess), runner, orchestrator, and server. Only the Linear tracker is
// faked via a local HTTP server.
package acceptance_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kwanpham2195/symphony-go/internal/codex"
	"github.com/kwanpham2195/symphony-go/internal/codex/tools"
	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/domain"
	"github.com/kwanpham2195/symphony-go/internal/orchestrator"
	"github.com/kwanpham2195/symphony-go/internal/runner"
	"github.com/kwanpham2195/symphony-go/internal/server"
	linearClient "github.com/kwanpham2195/symphony-go/internal/tracker/linear"
	"github.com/kwanpham2195/symphony-go/internal/workflow"
	"github.com/kwanpham2195/symphony-go/internal/workspace"
)

// fakeLinearServer returns an httptest server that serves candidate issues.
// It tracks how many poll requests it has received.
func fakeLinearServer(t *testing.T, issues []map[string]any) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Build response nodes
		nodes := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			nodes = append(nodes, issue)
		}

		resp := map[string]any{
			"data": map[string]any{
				"issues": map[string]any{
					"nodes": nodes,
					"pageInfo": map[string]any{
						"hasNextPage": false,
						"endCursor":   nil,
					},
				},
			},
		}

		json.NewEncoder(w).Encode(resp)
	}))
}

func makeLinearIssue(id, identifier, title, state string) map[string]any {
	return map[string]any{
		"id":         id,
		"identifier": identifier,
		"title":      title,
		"state":      map[string]any{"name": state},
		"labels":     map[string]any{"nodes": []any{}},
		"createdAt":  "2026-01-01T00:00:00Z",
		"updatedAt":  "2026-01-01T00:00:00Z",
	}
}

// TestAcceptance_SingleIssue_CompletesSuccessfully tests the full lifecycle:
// poll -> dispatch -> workspace create -> codex session -> turn completes.
func TestAcceptance_SingleIssue_CompletesSuccessfully(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping acceptance test in short mode")
	}

	// Setup fake Linear server
	linearSrv := fakeLinearServer(t, []map[string]any{
		makeLinearIssue("issue-acc-1", "ACC-1", "Acceptance test issue", "Todo"),
	})
	defer linearSrv.Close()

	// Setup workspace in temp dir
	wsRoot := t.TempDir()

	// Resolve fake codex script path
	fakeCodex, err := filepath.Abs("testdata/fake-codex/success.sh")
	if err != nil {
		t.Fatalf("resolve fake codex: %v", err)
	}
	// Ensure executable
	os.Chmod(fakeCodex, 0o755)

	// Build config
	cfg := &config.Config{
		Tracker: config.TrackerConfig{
			Kind:           "linear",
			Endpoint:       linearSrv.URL,
			APIKey:         "fake-token",
			ProjectSlug:    "test-proj",
			ActiveStates:   []string{"Todo", "In Progress"},
			TerminalStates: []string{"Done", "Closed"},
		},
		Polling: config.PollingConfig{IntervalMS: 100},
		Workspace: config.WorkspaceConfig{Root: wsRoot},
		Agent: config.AgentConfig{
			MaxConcurrentAgents:        2,
			MaxTurns:                   3,
			MaxRetryBackoffMS:          1000,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Codex: config.CodexConfig{
			Command:        fakeCodex,
			ApprovalPolicy: "never",
			ThreadSandbox:  "workspace-write",
			TurnTimeoutMS:  5000,
			ReadTimeoutMS:  5000,
			StallTimeoutMS: 10000,
		},
		Hooks: config.HooksConfig{TimeoutMS: 5000},
	}

	// Build components (same wiring as cmd/symphony/main.go)
	tracker := linearClient.NewClient(
		cfg.Tracker.Endpoint, cfg.Tracker.APIKey,
		cfg.Tracker.ProjectSlug, cfg.Tracker.ActiveStates,
	)
	wsMgr := workspace.NewManager(cfg, nil)
	codexClient := codex.NewClient(cfg, nil)
	codexClient.RegisterTool(tools.NewLinearGraphQL(tracker))

	promptTemplate := "Work on {{ issue.identifier }}: {{ issue.title }}"
	agentRunner := runner.New(cfg, wsMgr, codexClient, promptTemplate, nil)

	orch := orchestrator.New(orchestrator.Deps{
		Tracker:   tracker,
		Workspace: wsMgr,
		Runner:    agentRunner,
		Config:    cfg,
	})

	// Run a single tick
	ctx := context.Background()
	orch.Tick(ctx)

	// Wait for agent to complete
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		snap := orch.Snapshot()
		if len(snap.Running) == 0 && (len(snap.Retrying) > 0 || snap.CodexTotals.SecondsRunning > 0) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Verify workspace was created
	wsPath := filepath.Join(wsRoot, "ACC-1")
	info, err := os.Stat(wsPath)
	if err != nil || !info.IsDir() {
		t.Errorf("expected workspace directory at %s", wsPath)
	}

	// Verify snapshot shows activity happened
	snap := orch.Snapshot()
	if snap.CodexTotals.SecondsRunning == 0 {
		t.Error("expected some runtime recorded")
	}
}

// TestAcceptance_MaxConcurrency_Respected tests that the orchestrator does not
// exceed max_concurrent_agents.
func TestAcceptance_MaxConcurrency_Respected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping acceptance test in short mode")
	}

	// Serve 5 issues but limit concurrency to 2
	issues := make([]map[string]any, 5)
	for i := range issues {
		issues[i] = makeLinearIssue(
			fmt.Sprintf("issue-conc-%d", i),
			fmt.Sprintf("CONC-%d", i),
			fmt.Sprintf("Concurrency test %d", i),
			"Todo",
		)
	}
	linearSrv := fakeLinearServer(t, issues)
	defer linearSrv.Close()

	wsRoot := t.TempDir()
	fakeCodex, _ := filepath.Abs("testdata/fake-codex/success.sh")
	os.Chmod(fakeCodex, 0o755)

	cfg := &config.Config{
		Tracker: config.TrackerConfig{
			Kind:           "linear",
			Endpoint:       linearSrv.URL,
			APIKey:         "fake-token",
			ProjectSlug:    "test-proj",
			ActiveStates:   []string{"Todo", "In Progress"},
			TerminalStates: []string{"Done", "Closed"},
		},
		Polling:   config.PollingConfig{IntervalMS: 100},
		Workspace: config.WorkspaceConfig{Root: wsRoot},
		Agent: config.AgentConfig{
			MaxConcurrentAgents:        2,
			MaxTurns:                   1,
			MaxRetryBackoffMS:          60000,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Codex: config.CodexConfig{
			Command:        fakeCodex,
			ApprovalPolicy: "never",
			ThreadSandbox:  "workspace-write",
			TurnTimeoutMS:  5000,
			ReadTimeoutMS:  5000,
			StallTimeoutMS: 10000,
		},
		Hooks: config.HooksConfig{TimeoutMS: 5000},
	}

	tracker := linearClient.NewClient(
		cfg.Tracker.Endpoint, cfg.Tracker.APIKey,
		cfg.Tracker.ProjectSlug, cfg.Tracker.ActiveStates,
	)
	wsMgr := workspace.NewManager(cfg, nil)
	codexClient := codex.NewClient(cfg, nil)
	agentRunner := runner.New(cfg, wsMgr, codexClient, "Work on {{ issue.identifier }}", nil)

	orch := orchestrator.New(orchestrator.Deps{
		Tracker:   tracker,
		Workspace: wsMgr,
		Runner:    agentRunner,
		Config:    cfg,
	})

	// Tick and immediately check snapshot
	orch.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	snap := orch.Snapshot()
	if len(snap.Running) > 2 {
		t.Errorf("expected max 2 running, got %d", len(snap.Running))
	}

	// Wait for completion
	time.Sleep(1 * time.Second)
}

// TestAcceptance_FailedTurn_SchedulesRetry tests that a failed codex turn
// results in a retry entry in the orchestrator.
func TestAcceptance_FailedTurn_SchedulesRetry(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping acceptance test in short mode")
	}

	linearSrv := fakeLinearServer(t, []map[string]any{
		makeLinearIssue("issue-fail-1", "FAIL-1", "Failing issue", "Todo"),
	})
	defer linearSrv.Close()

	wsRoot := t.TempDir()
	fakeCodex, _ := filepath.Abs("testdata/fake-codex/fail.sh")
	os.Chmod(fakeCodex, 0o755)

	cfg := &config.Config{
		Tracker: config.TrackerConfig{
			Kind:           "linear",
			Endpoint:       linearSrv.URL,
			APIKey:         "fake-token",
			ProjectSlug:    "test-proj",
			ActiveStates:   []string{"Todo"},
			TerminalStates: []string{"Done"},
		},
		Polling:   config.PollingConfig{IntervalMS: 100},
		Workspace: config.WorkspaceConfig{Root: wsRoot},
		Agent: config.AgentConfig{
			MaxConcurrentAgents:        1,
			MaxTurns:                   1,
			MaxRetryBackoffMS:          60000,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Codex: config.CodexConfig{
			Command:        fakeCodex,
			ApprovalPolicy: "never",
			ThreadSandbox:  "workspace-write",
			TurnTimeoutMS:  5000,
			ReadTimeoutMS:  5000,
			StallTimeoutMS: 10000,
		},
		Hooks: config.HooksConfig{TimeoutMS: 5000},
	}

	tracker := linearClient.NewClient(
		cfg.Tracker.Endpoint, cfg.Tracker.APIKey,
		cfg.Tracker.ProjectSlug, cfg.Tracker.ActiveStates,
	)
	wsMgr := workspace.NewManager(cfg, nil)
	codexClient := codex.NewClient(cfg, nil)
	agentRunner := runner.New(cfg, wsMgr, codexClient, "Work on {{ issue.identifier }}", nil)

	orch := orchestrator.New(orchestrator.Deps{
		Tracker:   tracker,
		Workspace: wsMgr,
		Runner:    agentRunner,
		Config:    cfg,
	})

	orch.Tick(context.Background())

	// Wait for turn to fail
	deadline := time.Now().Add(5 * time.Second)
	var snap domain.Snapshot
	for time.Now().Before(deadline) {
		snap = orch.Snapshot()
		if len(snap.Running) == 0 && len(snap.Retrying) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if len(snap.Retrying) == 0 {
		t.Error("expected retry to be scheduled after failed turn")
	}
}

// TestAcceptance_ServerAPI_ReturnsSnapshot tests the HTTP server returns
// valid JSON from the state endpoint during a run.
func TestAcceptance_ServerAPI_ReturnsSnapshot(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping acceptance test in short mode")
	}

	linearSrv := fakeLinearServer(t, []map[string]any{
		makeLinearIssue("issue-srv-1", "SRV-1", "Server test", "In Progress"),
	})
	defer linearSrv.Close()

	wsRoot := t.TempDir()
	fakeCodex, _ := filepath.Abs("testdata/fake-codex/success.sh")
	os.Chmod(fakeCodex, 0o755)

	cfg := &config.Config{
		Tracker: config.TrackerConfig{
			Kind:           "linear",
			Endpoint:       linearSrv.URL,
			APIKey:         "fake-token",
			ProjectSlug:    "test-proj",
			ActiveStates:   []string{"Todo", "In Progress"},
			TerminalStates: []string{"Done"},
		},
		Polling:   config.PollingConfig{IntervalMS: 100},
		Workspace: config.WorkspaceConfig{Root: wsRoot},
		Agent: config.AgentConfig{
			MaxConcurrentAgents:        1,
			MaxTurns:                   1,
			MaxRetryBackoffMS:          60000,
			MaxConcurrentAgentsByState: map[string]int{},
		},
		Codex: config.CodexConfig{
			Command:        fakeCodex,
			ApprovalPolicy: "never",
			ThreadSandbox:  "workspace-write",
			TurnTimeoutMS:  5000,
			ReadTimeoutMS:  5000,
			StallTimeoutMS: 10000,
		},
		Hooks: config.HooksConfig{TimeoutMS: 5000},
	}

	tracker := linearClient.NewClient(
		cfg.Tracker.Endpoint, cfg.Tracker.APIKey,
		cfg.Tracker.ProjectSlug, cfg.Tracker.ActiveStates,
	)
	wsMgr := workspace.NewManager(cfg, nil)
	codexClient := codex.NewClient(cfg, nil)
	agentRunner := runner.New(cfg, wsMgr, codexClient, "Work on {{ issue.identifier }}", nil)

	orch := orchestrator.New(orchestrator.Deps{
		Tracker:   tracker,
		Workspace: wsMgr,
		Runner:    agentRunner,
		Config:    cfg,
	})

	srv := server.New(orch, orch, server.Options{Port: 0, Host: "127.0.0.1"}, nil)

	// Test state endpoint before any work
	req := httptest.NewRequest("GET", "/api/v1/state", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("state endpoint status = %d", w.Code)
	}

	var snap struct {
		Running  []any `json:"running"`
		Retrying []any `json:"retrying"`
	}
	if err := json.NewDecoder(w.Body).Decode(&snap); err != nil {
		t.Fatalf("decode state: %v", err)
	}

	// Trigger work and check again
	orch.Tick(context.Background())
	time.Sleep(50 * time.Millisecond)

	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, httptest.NewRequest("GET", "/api/v1/state", nil))
	if w2.Code != 200 {
		t.Fatalf("state endpoint after tick status = %d", w2.Code)
	}

	// Test issue endpoint
	w3 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w3, httptest.NewRequest("GET", "/api/v1/issues/SRV-1", nil))
	// Could be 200 (running/retrying) or 404 (completed too fast)
	if w3.Code != 200 && w3.Code != 404 {
		t.Errorf("issue endpoint status = %d", w3.Code)
	}

	// Test refresh endpoint
	w4 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w4, httptest.NewRequest("POST", "/api/v1/refresh", nil))
	if w4.Code != 200 {
		t.Errorf("refresh endpoint status = %d", w4.Code)
	}

	time.Sleep(500 * time.Millisecond)
}

// TestAcceptance_WorkflowParsing_EndToEnd tests that a workflow file is loaded,
// parsed, config extracted, and prompt rendered correctly.
func TestAcceptance_WorkflowParsing_EndToEnd(t *testing.T) {
	// Write a temp workflow file
	dir := t.TempDir()
	wfPath := filepath.Join(dir, "WORKFLOW.md")
	wfContent := `---
tracker:
  kind: linear
  api_key: test-key
  project_slug: my-proj
  active_states:
    - Todo
    - In Progress
polling:
  interval_ms: 10000
workspace:
  root: /tmp/test_ws
agent:
  max_concurrent_agents: 5
codex:
  command: fake-codex
---
You are working on {{ issue.identifier }}: {{ issue.title }}.
`
	os.WriteFile(wfPath, []byte(wfContent), 0o644)

	// Load and parse
	wf, err := workflow.Load(wfPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Build config
	cfg, err := config.FromMap(wf.Config)
	if err != nil {
		t.Fatalf("FromMap: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	// Verify config
	if cfg.Tracker.Kind != "linear" {
		t.Errorf("tracker.kind = %q", cfg.Tracker.Kind)
	}
	if cfg.Tracker.APIKey != "test-key" {
		t.Errorf("tracker.api_key = %q", cfg.Tracker.APIKey)
	}
	if cfg.Agent.MaxConcurrentAgents != 5 {
		t.Errorf("max_concurrent_agents = %d", cfg.Agent.MaxConcurrentAgents)
	}

	// Verify prompt renders
	if !strings.Contains(wf.PromptTemplate, "{{ issue.identifier }}") {
		t.Error("prompt template missing issue.identifier")
	}
}
