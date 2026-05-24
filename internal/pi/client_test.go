package pi

import (
	"context"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

func testdataPath(name string) string {
	return filepath.Join("..", "..", "testdata", "fake-pi", name)
}

func testConfig(fakeScript string) *config.Config {
	absScript, _ := filepath.Abs(testdataPath(fakeScript))
	return &config.Config{
		Runner: "pi",
		Pi: config.PiConfig{
			Command:       absScript,
			TurnTimeoutMS: 10000,
			ReadTimeoutMS: 5000,
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

	if sess.pid == 0 {
		t.Error("pid should be non-zero")
	}
	if sess.workspace != workspace {
		t.Errorf("workspace = %q, want %q", sess.workspace, workspace)
	}
}

func TestSendPrompt_Success(t *testing.T) {
	cfg := testConfig("success.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	var updates []internal.AgentUpdate
	var mu sync.Mutex
	onUpdate := func(u internal.AgentUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	}

	result, err := c.SendPrompt(context.Background(), sess, "test prompt", onUpdate)
	if err != nil {
		t.Fatalf("SendPrompt error: %v", err)
	}

	if result.Status != "completed" {
		t.Errorf("Status = %q, want completed", result.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	var gotStart, gotEnd bool
	for _, u := range updates {
		switch u.Event {
		case "session_started":
			gotStart = true
		case "turn_completed":
			gotEnd = true
		}
	}
	if !gotStart {
		t.Error("missing session_started event")
	}
	if !gotEnd {
		t.Error("missing turn_completed event")
	}
}

func TestSendPrompt_ExtractsUsage(t *testing.T) {
	cfg := testConfig("success.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	var updates []internal.AgentUpdate
	var mu sync.Mutex
	onUpdate := func(u internal.AgentUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	}

	_, err = c.SendPrompt(context.Background(), sess, "test", onUpdate)
	if err != nil {
		t.Fatalf("SendPrompt error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	var foundUsage bool
	for _, u := range updates {
		if u.Usage != nil {
			foundUsage = true
			if u.Usage.InputTokens != 100 {
				t.Errorf("InputTokens = %d, want 100", u.Usage.InputTokens)
			}
			if u.Usage.OutputTokens != 50 {
				t.Errorf("OutputTokens = %d, want 50", u.Usage.OutputTokens)
			}
			if u.Usage.TotalTokens != 150 {
				t.Errorf("TotalTokens = %d, want 150 (input+output)", u.Usage.TotalTokens)
			}
			break
		}
	}
	if !foundUsage {
		t.Error("no update contained token usage")
	}
}

func TestSendPrompt_PromptRejected(t *testing.T) {
	cfg := testConfig("fail.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	_, err = c.SendPrompt(context.Background(), sess, "test", nil)
	if err == nil {
		t.Fatal("expected error for rejected prompt")
	}
}

func TestSendPrompt_CompactionEvents(t *testing.T) {
	cfg := testConfig("compaction.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	var updates []internal.AgentUpdate
	var mu sync.Mutex
	onUpdate := func(u internal.AgentUpdate) {
		mu.Lock()
		updates = append(updates, u)
		mu.Unlock()
	}

	result, err := c.SendPrompt(context.Background(), sess, "test", onUpdate)
	if err != nil {
		t.Fatalf("SendPrompt error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want completed", result.Status)
	}

	mu.Lock()
	defer mu.Unlock()
	var gotCompactStart, gotCompactEnd bool
	for _, u := range updates {
		switch u.Event {
		case "compaction_started":
			gotCompactStart = true
		case "compaction_ended":
			gotCompactEnd = true
		}
	}
	if !gotCompactStart {
		t.Error("missing compaction_started event")
	}
	if !gotCompactEnd {
		t.Error("missing compaction_ended event")
	}
}

func TestSendPrompt_ExtensionUIIgnored(t *testing.T) {
	cfg := testConfig("extension_ui.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	result, err := c.SendPrompt(context.Background(), sess, "test", nil)
	if err != nil {
		t.Fatalf("SendPrompt error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want completed", result.Status)
	}
}

func TestStartSession_ProcessExit(t *testing.T) {
	cfg := testConfig("exit.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		return // exit on startup is acceptable
	}
	defer c.StopSession(sess)

	_, err = c.SendPrompt(context.Background(), sess, "test", nil)
	if err == nil {
		t.Fatal("expected error when process exits")
	}
}

func TestStopSession_Idempotent(t *testing.T) {
	cfg := testConfig("success.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}

	if err := c.StopSession(sess); err != nil {
		t.Fatalf("first StopSession error: %v", err)
	}
	if err := c.StopSession(sess); err != nil {
		t.Fatalf("second StopSession should be safe: %v", err)
	}
}

func TestStopSession_Nil(t *testing.T) {
	c := NewClient(testConfig("success.sh"), nil)
	if err := c.StopSession(nil); err != nil {
		t.Fatalf("StopSession(nil) error: %v", err)
	}
}

func TestSendPrompt_DialogUIAutoCancelled(t *testing.T) {
	cfg := testConfig("dialog_ui.sh")
	c := NewClient(cfg, nil)

	workspace := t.TempDir()
	sess, err := c.StartSession(context.Background(), workspace)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	defer c.StopSession(sess)

	result, err := c.SendPrompt(context.Background(), sess, "test", nil)
	if err != nil {
		t.Fatalf("SendPrompt error: %v", err)
	}
	if result.Status != "completed" {
		t.Errorf("Status = %q, want completed (dialog should be auto-cancelled)", result.Status)
	}
}
