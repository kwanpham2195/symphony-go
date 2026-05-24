package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

func testConfig(root string) *config.Config {
	return &config.Config{
		Workspace: config.WorkspaceConfig{Root: root},
		Hooks:     config.HooksConfig{TimeoutMS: 5000},
		Codex:     config.CodexConfig{Command: "codex app-server"},
	}
}

// --- SafeIdentifier tests ---

func TestSafeIdentifier(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ABC-123", "ABC-123"},
		{"SYM 42", "SYM_42"},
		{"feat/branch", "feat_branch"},
		{"hello world!", "hello_world_"},
		{"a.b-c_d", "a.b-c_d"},
		{"", "issue"},
		{"日本語", "___"},
	}
	for _, tt := range tests {
		got := SafeIdentifier(tt.input)
		if got != tt.want {
			t.Errorf("SafeIdentifier(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- ValidatePath tests ---

func TestValidatePath_Valid(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "SYM-1")
	if err := ValidatePath(ws, root); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePath_EqualsRoot(t *testing.T) {
	root := t.TempDir()
	err := ValidatePath(root, root)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "workspace_equals_root") {
		t.Errorf("got: %v", err)
	}
}

func TestValidatePath_OutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "..", "escape")
	err := ValidatePath(outside, root)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "workspace_outside_root") {
		t.Errorf("got: %v", err)
	}
}

func TestValidatePath_SymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// Create a symlink inside root that points outside
	link := filepath.Join(root, "escape-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}

	err := ValidatePath(link, root)
	if err == nil {
		t.Fatal("expected error for symlink escape")
	}
	if !strings.Contains(err.Error(), "workspace_symlink_escape") {
		t.Errorf("got: %v", err)
	}
}

// --- CreateForIssue tests ---

func TestCreateForIssue_NewWorkspace(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	mgr := NewManager(cfg, nil)

	issue := internal.Issue{Identifier: "SYM-1", ID: "id-1"}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !ws.CreatedNow {
		t.Error("expected CreatedNow=true")
	}
	if ws.WorkspaceKey != "SYM-1" {
		t.Errorf("key = %q", ws.WorkspaceKey)
	}
	if ws.Path != filepath.Join(root, "SYM-1") {
		t.Errorf("path = %q", ws.Path)
	}
	// Directory should exist
	info, err := os.Stat(ws.Path)
	if err != nil || !info.IsDir() {
		t.Error("workspace directory does not exist")
	}
}

func TestCreateForIssue_ReuseExisting(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "SYM-2")
	os.MkdirAll(wsPath, 0o755)
	// Write a file to prove reuse preserves data
	os.WriteFile(filepath.Join(wsPath, "data.txt"), []byte("keep"), 0o644)

	issue := internal.Issue{Identifier: "SYM-2"}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if ws.CreatedNow {
		t.Error("expected CreatedNow=false for existing dir")
	}
	// File should still be there
	data, err := os.ReadFile(filepath.Join(ws.Path, "data.txt"))
	if err != nil || string(data) != "keep" {
		t.Error("existing workspace data was not preserved")
	}
}

func TestCreateForIssue_StaleFile(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	mgr := NewManager(cfg, nil)

	// Create a file (not a directory) at the workspace path
	stalePath := filepath.Join(root, "SYM-3")
	os.WriteFile(stalePath, []byte("stale"), 0o644)

	issue := internal.Issue{Identifier: "SYM-3"}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if !ws.CreatedNow {
		t.Error("expected CreatedNow=true after stale removal")
	}
	info, err := os.Stat(ws.Path)
	if err != nil || !info.IsDir() {
		t.Error("should be a directory after stale removal")
	}
}

func TestCreateForIssue_IdentifierSanitized(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	mgr := NewManager(cfg, nil)

	issue := internal.Issue{Identifier: "TEAM/123"}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if ws.WorkspaceKey != "TEAM_123" {
		t.Errorf("key = %q, want TEAM_123", ws.WorkspaceKey)
	}
}

// --- Hook tests ---

func TestCreateForIssue_AfterCreateHook(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.AfterCreate = "echo created > hook_ran.txt"
	mgr := NewManager(cfg, nil)

	issue := internal.Issue{Identifier: "HOOK-1"}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Hook should have created a file
	data, err := os.ReadFile(filepath.Join(ws.Path, "hook_ran.txt"))
	if err != nil || !strings.Contains(string(data), "created") {
		t.Error("after_create hook did not run")
	}
}

func TestCreateForIssue_AfterCreateHookNotOnReuse(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.AfterCreate = "echo should_not_run > hook_ran.txt"
	mgr := NewManager(cfg, nil)

	// Pre-create workspace
	os.MkdirAll(filepath.Join(root, "HOOK-2"), 0o755)

	issue := internal.Issue{Identifier: "HOOK-2"}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Hook should NOT have run
	if _, err := os.Stat(filepath.Join(ws.Path, "hook_ran.txt")); err == nil {
		t.Error("after_create hook should not run on reuse")
	}
}

func TestCreateForIssue_AfterCreateHookFailure(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.AfterCreate = "exit 1"
	mgr := NewManager(cfg, nil)

	issue := internal.Issue{Identifier: "HOOK-FAIL"}
	_, err := mgr.CreateForIssue(context.Background(), issue)
	if err == nil {
		t.Fatal("expected error for hook failure")
	}
	if !strings.Contains(err.Error(), "after_create hook") {
		t.Errorf("error = %v", err)
	}
	// Workspace should be cleaned up
	wsPath := filepath.Join(root, "HOOK-FAIL")
	if _, statErr := os.Stat(wsPath); statErr == nil {
		t.Error("workspace should be removed after hook failure")
	}
}

func TestBeforeRunHook(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.BeforeRun = "echo running > before_run.txt"
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "BR-1")
	os.MkdirAll(wsPath, 0o755)
	ws := internal.Workspace{Path: wsPath}
	issue := internal.Issue{Identifier: "BR-1"}

	err := mgr.RunBeforeRunHook(context.Background(), ws, issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(wsPath, "before_run.txt"))
	if !strings.Contains(string(data), "running") {
		t.Error("before_run hook did not run")
	}
}

func TestBeforeRunHook_Failure(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.BeforeRun = "exit 42"
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "BR-FAIL")
	os.MkdirAll(wsPath, 0o755)
	ws := internal.Workspace{Path: wsPath}
	issue := internal.Issue{Identifier: "BR-FAIL"}

	err := mgr.RunBeforeRunHook(context.Background(), ws, issue)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestAfterRunHook_FailureIgnored(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.AfterRun = "exit 1"
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "AR-1")
	os.MkdirAll(wsPath, 0o755)
	ws := internal.Workspace{Path: wsPath}
	issue := internal.Issue{Identifier: "AR-1"}

	// Should not panic or return error
	mgr.RunAfterRunHook(context.Background(), ws, issue)
}

func TestRemoveIssueWorkspace(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "RM-1")
	os.MkdirAll(wsPath, 0o755)
	os.WriteFile(filepath.Join(wsPath, "file.txt"), []byte("data"), 0o644)

	err := mgr.RemoveIssueWorkspace(context.Background(), "RM-1")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if _, statErr := os.Stat(wsPath); statErr == nil {
		t.Error("workspace should be removed")
	}
}

func TestRemoveIssueWorkspace_BeforeRemoveHook(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.BeforeRemove = "echo removing > before_remove.txt"
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "RM-HOOK")
	os.MkdirAll(wsPath, 0o755)

	err := mgr.RemoveIssueWorkspace(context.Background(), "RM-HOOK")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Directory should be gone (hook ran, then removed)
	if _, statErr := os.Stat(wsPath); statErr == nil {
		t.Error("workspace should be removed")
	}
}

func TestRemoveIssueWorkspace_BeforeRemoveHookFailureIgnored(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.BeforeRemove = "exit 1"
	mgr := NewManager(cfg, nil)

	wsPath := filepath.Join(root, "RM-HOOKFAIL")
	os.MkdirAll(wsPath, 0o755)

	err := mgr.RemoveIssueWorkspace(context.Background(), "RM-HOOKFAIL")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Should still be removed despite hook failure
	if _, statErr := os.Stat(wsPath); statErr == nil {
		t.Error("workspace should be removed even after hook failure")
	}
}

func TestHookTimeout(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	cfg.Hooks.TimeoutMS = 500 // very short
	cfg.Hooks.AfterCreate = "sleep 10"
	mgr := NewManager(cfg, nil)

	issue := internal.Issue{Identifier: "TIMEOUT-1"}
	_, err := mgr.CreateForIssue(context.Background(), issue)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "after_create hook") {
		t.Errorf("error = %v", err)
	}
}

// --- Edge cases ---

func TestCreateForIssue_EmptyIdentifier(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root)
	mgr := NewManager(cfg, nil)

	issue := internal.Issue{Identifier: ""}
	ws, err := mgr.CreateForIssue(context.Background(), issue)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if ws.WorkspaceKey != "issue" {
		t.Errorf("key = %q, want 'issue'", ws.WorkspaceKey)
	}
}
