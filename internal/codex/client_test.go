package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

func testdataPath(name string) string {
	// Navigate from internal/codex to repo root testdata
	return filepath.Join("..", "..", "testdata", "fake-codex", name)
}

func testConfig(fakeScript string) *config.Config {
	absScript, _ := filepath.Abs(testdataPath(fakeScript))
	return &config.Config{
		Workspace: config.WorkspaceConfig{Root: os.TempDir()},
		Codex: config.CodexConfig{
			Command:        absScript,
			ApprovalPolicy: "never", // high-trust auto-approve
			ThreadSandbox:  "workspace-write",
			TurnTimeoutMS:  10000,
			ReadTimeoutMS:  5000,
			StallTimeoutMS: 300000,
		},
	}
}

func TestStartSession_Success(t *testing.T) {
	cfg := testConfig("success.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	if sess.threadID != "thread-abc" {
		t.Errorf("threadID = %q, want thread-abc", sess.threadID)
	}
	if sess.pid == 0 {
		t.Error("pid should be non-zero")
	}
}

func TestRunTurn_Completed(t *testing.T) {
	cfg := testConfig("success.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-1", Title: "Test"}
	var updates []string
	var mu sync.Mutex
	onUpdate := func(u internal.AgentUpdate) {
		mu.Lock()
		updates = append(updates, u.Event)
		mu.Unlock()
	}

	result, err := c.RunTurn(context.Background(), sess, issue, "Do the work", onUpdate)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed", result.Status)
	}
	if result.ThreadID != "thread-abc" {
		t.Errorf("threadID = %q", result.ThreadID)
	}
	if result.TurnID != "turn-xyz" {
		t.Errorf("turnID = %q", result.TurnID)
	}
	if result.SessionID != "thread-abc-turn-xyz" {
		t.Errorf("sessionID = %q", result.SessionID)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(updates) < 2 {
		t.Fatalf("expected at least 2 updates, got %d", len(updates))
	}
	if updates[0] != "session_started" {
		t.Errorf("first update = %q", updates[0])
	}
	if updates[len(updates)-1] != "turn_completed" {
		t.Errorf("last update = %q", updates[len(updates)-1])
	}
}

func TestRunTurn_DefaultsToDangerFullAccessTurnPolicy(t *testing.T) {
	traceFile := filepath.Join(t.TempDir(), "turn.json")
	t.Setenv("TRACE_FILE", traceFile)

	cfg := testConfig("trace_turn.sh")
	cfg.Codex.ThreadSandbox = "danger-full-access"
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-7", Title: "Trace sandbox policy"}
	result, err := c.RunTurn(context.Background(), sess, issue, "Prompt", nil)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed", result.Status)
	}

	payload, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	if !strings.Contains(string(payload), `"sandboxPolicy":{"type":"dangerFullAccess"}`) {
		t.Fatalf("turn/start payload did not use dangerFullAccess: %s", payload)
	}
}

func TestRunTurn_Failed(t *testing.T) {
	cfg := testConfig("fail.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-2", Title: "Fail test"}
	result, err := c.RunTurn(context.Background(), sess, issue, "Prompt", nil)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("status = %q, want failed", result.Status)
	}
}

func TestRunTurn_Approval(t *testing.T) {
	cfg := testConfig("approval.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-3", Title: "Approval test"}
	var updates []string
	var mu sync.Mutex
	onUpdate := func(u internal.AgentUpdate) {
		mu.Lock()
		updates = append(updates, u.Event)
		mu.Unlock()
	}

	result, err := c.RunTurn(context.Background(), sess, issue, "Prompt", onUpdate)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed (approval auto-approved)", result.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, u := range updates {
		if u == "approval_auto_approved" {
			found = true
		}
	}
	if !found {
		t.Error("expected approval_auto_approved event")
	}
}

func TestRunTurn_UnsupportedTool(t *testing.T) {
	cfg := testConfig("unsupported_tool.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-4", Title: "Tool test"}
	var updates []string
	var mu sync.Mutex
	onUpdate := func(u internal.AgentUpdate) {
		mu.Lock()
		updates = append(updates, u.Event)
		mu.Unlock()
	}

	result, err := c.RunTurn(context.Background(), sess, issue, "Prompt", onUpdate)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed (unsupported tool should not stall)", result.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, u := range updates {
		if u == "unsupported_tool_call" {
			found = true
		}
	}
	if !found {
		t.Error("expected unsupported_tool_call event")
	}
}

func TestRunTurn_InputRequired(t *testing.T) {
	cfg := testConfig("input_required.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-5", Title: "Input test"}
	result, err := c.RunTurn(context.Background(), sess, issue, "Prompt", nil)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "input_required" {
		t.Errorf("status = %q, want input_required", result.Status)
	}
}

func TestRunTurn_ProcessExit(t *testing.T) {
	cfg := testConfig("exit.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	issue := internal.Issue{Identifier: "T-6", Title: "Exit test"}
	result, err := c.RunTurn(context.Background(), sess, issue, "Prompt", nil)
	if err != nil {
		t.Fatalf("RunTurn error: %v", err)
	}
	if result.Status != "exit" {
		t.Errorf("status = %q, want exit", result.Status)
	}
}

func TestHelpers(t *testing.T) {
	t.Run("isAutoApprove", func(t *testing.T) {
		if !isAutoApprove("never") {
			t.Error("expected true for 'never'")
		}
		if isAutoApprove("always") {
			t.Error("expected false for 'always'")
		}
		if isAutoApprove(map[string]any{"reject": true}) {
			t.Error("expected false for map")
		}
	})

	t.Run("autoApproveDecision", func(t *testing.T) {
		if d := autoApproveDecision("item/commandExecution/requestApproval"); d != "acceptForSession" {
			t.Errorf("got %q", d)
		}
		if d := autoApproveDecision("execCommandApproval"); d != "approved_for_session" {
			t.Errorf("got %q", d)
		}
	})

	t.Run("extractUsage", func(t *testing.T) {
		msg := map[string]any{
			"usage": map[string]any{
				"input_tokens":  float64(100),
				"output_tokens": float64(200),
				"total_tokens":  float64(300),
			},
		}
		u := extractUsage(msg)
		if u == nil {
			t.Fatal("expected usage")
		}
		if u.InputTokens != 100 || u.OutputTokens != 200 || u.TotalTokens != 300 {
			t.Errorf("usage = %+v", u)
		}
	})

	t.Run("extractUsage_nil", func(t *testing.T) {
		u := extractUsage(map[string]any{"method": "turn/completed"})
		if u != nil {
			t.Error("expected nil usage")
		}
	})

	t.Run("isInputRequired", func(t *testing.T) {
		if !isInputRequired("turn/input_required", map[string]any{}) {
			t.Error("expected true")
		}
		if isInputRequired("notification", map[string]any{}) {
			t.Error("expected false for non-turn method")
		}
		if !isInputRequired("turn/custom", map[string]any{"requiresInput": true}) {
			t.Error("expected true for requiresInput field")
		}
	})
}
