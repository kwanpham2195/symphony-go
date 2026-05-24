package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/kwanpham2195/symphony-go/internal/orchestrator"
)

// --- Fakes ---

type fakeSnapshotProvider struct {
	snapshot orchestrator.Snapshot
}

func (f *fakeSnapshotProvider) Snapshot() orchestrator.Snapshot {
	return f.snapshot
}

type fakeRefresher struct {
	mu     sync.Mutex
	called bool
}

func (f *fakeRefresher) Tick(_ context.Context) {
	f.mu.Lock()
	f.called = true
	f.mu.Unlock()
}

func testServer(snap orchestrator.Snapshot) *Server {
	return New(
		&fakeSnapshotProvider{snapshot: snap},
		&fakeRefresher{},
		Options{Port: 0, Host: "127.0.0.1"},
		nil,
	)
}

// --- Dashboard tests ---

func TestDashboard_GET(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{
		Running: []orchestrator.RunningRow{
			{IssueID: "id-1", IssueIdentifier: "SYM-1", SessionID: "sess-1", TurnCount: 2, StartedAt: time.Now()},
		},
		Retrying: []orchestrator.RetryRow{
			{IssueID: "id-2", Identifier: "SYM-2", Attempt: 3, DueAt: time.Now().Add(10 * time.Second)},
		},
		CodexTotals: orchestrator.CodexTotals{TotalTokens: 1000, SecondsRunning: 120},
	})

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content-type = %q", ct)
	}
	body := w.Body.String()
	if len(body) < 100 {
		t.Error("dashboard body too short")
	}
}

func TestDashboard_MethodNotAllowed(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})
	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- State API tests ---

func TestState_GET(t *testing.T) {
	snap := orchestrator.Snapshot{
		Running: []orchestrator.RunningRow{
			{IssueID: "id-1", IssueIdentifier: "SYM-1"},
		},
		CodexTotals: orchestrator.CodexTotals{InputTokens: 100, OutputTokens: 200, TotalTokens: 300},
	}
	srv := testServer(snap)

	req := httptest.NewRequest("GET", "/api/v1/state", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var result orchestrator.Snapshot
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if len(result.Running) != 1 {
		t.Errorf("running = %d", len(result.Running))
	}
	if result.CodexTotals.TotalTokens != 300 {
		t.Errorf("total_tokens = %d", result.CodexTotals.TotalTokens)
	}
}

func TestState_MethodNotAllowed(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})
	req := httptest.NewRequest("POST", "/api/v1/state", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- Issue API tests ---

func TestIssue_Found_Running(t *testing.T) {
	snap := orchestrator.Snapshot{
		Running: []orchestrator.RunningRow{
			{IssueID: "id-1", IssueIdentifier: "SYM-1", SessionID: "sess-1"},
		},
	}
	srv := testServer(snap)

	req := httptest.NewRequest("GET", "/api/v1/issues/SYM-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "running" {
		t.Errorf("status = %v", result["status"])
	}
}

func TestIssue_Found_Retrying(t *testing.T) {
	snap := orchestrator.Snapshot{
		Retrying: []orchestrator.RetryRow{
			{IssueID: "id-2", Identifier: "SYM-2", Attempt: 2},
		},
	}
	srv := testServer(snap)

	req := httptest.NewRequest("GET", "/api/v1/issues/SYM-2", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["status"] != "retrying" {
		t.Errorf("status = %v", result["status"])
	}
}

func TestIssue_NotFound(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})

	req := httptest.NewRequest("GET", "/api/v1/issues/UNKNOWN-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestIssue_MethodNotAllowed(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})
	req := httptest.NewRequest("DELETE", "/api/v1/issues/SYM-1", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
}

// --- Refresh API tests ---

func TestRefresh_POST(t *testing.T) {
	refresher := &fakeRefresher{}
	srv := New(
		&fakeSnapshotProvider{},
		refresher,
		Options{Port: 0},
		nil,
	)

	req := httptest.NewRequest("POST", "/api/v1/refresh", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}

	var result map[string]any
	json.NewDecoder(w.Body).Decode(&result)
	if result["queued"] != true {
		t.Errorf("queued = %v", result["queued"])
	}

	// Give goroutine time
	time.Sleep(50 * time.Millisecond)
	refresher.mu.Lock()
	called := refresher.called
	refresher.mu.Unlock()
	if !called {
		t.Error("expected refresh to be called")
	}
}

func TestRefresh_MethodNotAllowed(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})
	req := httptest.NewRequest("GET", "/api/v1/refresh", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "POST" {
		t.Errorf("Allow header = %q, want POST", allow)
	}
}

func TestState_MethodNotAllowed_AllowHeader(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})
	req := httptest.NewRequest("POST", "/api/v1/state", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 405 {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if allow := w.Header().Get("Allow"); allow != "GET" {
		t.Errorf("Allow header = %q, want GET", allow)
	}
}

// --- 404 for unknown paths ---

func TestUnknownPath_404(t *testing.T) {
	srv := testServer(orchestrator.Snapshot{})
	req := httptest.NewRequest("GET", "/unknown", nil)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, req)

	if w.Code != 404 {
		t.Errorf("status = %d, want 404", w.Code)
	}
}
