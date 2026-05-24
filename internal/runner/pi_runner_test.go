package runner

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/pi"
	"github.com/kwanpham2195/symphony-go/internal/workspace"
)

func fakePiScript(name string) string {
	abs, _ := filepath.Abs(filepath.Join("..", "..", "testdata", "fake-pi", name))
	return abs
}

func piTestConfig(script string) *config.Config {
	return &config.Config{
		Runner:    config.RunnerPi,
		Workspace: config.WorkspaceConfig{Root: os.TempDir()},
		Agent:     config.AgentConfig{MaxTurns: 5},
		Pi: config.PiConfig{
			Command:       fakePiScript(script),
			TurnTimeoutMS: 10000,
			ReadTimeoutMS: 5000,
		},
	}
}

func TestPiRunner_Run_Success(t *testing.T) {
	cfg := piTestConfig("success.sh")
	wsMgr := workspace.NewManager(cfg, nil)
	piC := pi.NewClient(cfg, nil)
	r := NewPiRunner(cfg, wsMgr, piC, "You are working on {{ issue.identifier }}.", nil)

	issue := internal.Issue{
		ID:         "issue-1",
		Identifier: "TEST-1",
		Title:      "Test issue",
		State:      "Todo",
	}

	updates := make(chan internal.AgentUpdate, 64)
	err := r.Run(context.Background(), issue, nil, updates)
	close(updates)

	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Collect updates
	var events []internal.AgentEvent
	for u := range updates {
		events = append(events, u.Event)
	}

	// Should have at least session_started and turn_completed
	var gotStart, gotEnd bool
	for _, e := range events {
		switch e {
		case internal.EventSessionStarted:
			gotStart = true
		case internal.EventTurnCompleted:
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

func TestPiRunner_Run_PromptFailure(t *testing.T) {
	cfg := piTestConfig("fail.sh")
	wsMgr := workspace.NewManager(cfg, nil)
	piC := pi.NewClient(cfg, nil)
	r := NewPiRunner(cfg, wsMgr, piC, "test prompt", nil)

	issue := internal.Issue{
		ID:         "issue-2",
		Identifier: "TEST-2",
		Title:      "Failing issue",
		State:      "Todo",
	}

	updates := make(chan internal.AgentUpdate, 64)
	err := r.Run(context.Background(), issue, nil, updates)
	close(updates)

	if err == nil {
		t.Fatal("expected error for rejected prompt")
	}
}

func TestPiRunner_UpdatePrompt_ConcurrentAccess(t *testing.T) {
	cfg := piTestConfig("success.sh")
	r := &PiRunner{}
	r.cfg = cfg
	r.UpdatePrompt("initial")

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			r.UpdatePrompt("updated prompt")
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			p := r.getPrompt()
			if p != "initial" && p != "updated prompt" {
				t.Errorf("unexpected prompt: %q", p)
			}
		}
	}()

	wg.Wait()
}
