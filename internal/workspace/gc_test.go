package workspace

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

// --- fake tracker ---

type fakeIssueChecker struct {
	terminal []internal.Issue
	active   []internal.Issue
	err      error
}

func (f *fakeIssueChecker) FetchIssuesByStates(_ context.Context, _ []string) ([]internal.Issue, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.terminal, nil
}

func (f *fakeIssueChecker) FetchCandidateIssues(_ context.Context) ([]internal.Issue, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.active, nil
}

// --- helpers ---

func gcConfig(root string) *config.Config {
	return &config.Config{
		Workspace: config.WorkspaceConfig{Root: root},
		Tracker: config.TrackerConfig{
			TerminalStates: []string{"Done", "Canceled"},
			ActiveStates:   []string{"Todo", "In Progress"},
		},
		GC: config.GCConfig{
			Enabled:          true,
			IntervalMS:       3600000,
			TTLMS:            86400000,  // 24h
			OrphanTTLMS:      172800000, // 48h
			ArtifactTTLMS:    3600000,   // 1h
			ArtifactPatterns: []string{"node_modules", ".codex"},
		},
	}
}

func makeDir(t *testing.T, path string, age time.Duration) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	// Set modtime to simulate age.
	past := time.Now().Add(-age)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatal(err)
	}
}

func dirExists(t *testing.T, path string) bool {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// --- TTL-based removal for terminal issues ---

func TestGC_RemovesTerminalWorkspacePastTTL(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.TTLMS = 1000 // 1 second

	makeDir(t, filepath.Join(root, "SYM-1"), 2*time.Second)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "SYM-1", State: "Done"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.RemovedDirs) != 1 || result.RemovedDirs[0] != "SYM-1" {
		t.Errorf("RemovedDirs = %v, want [SYM-1]", result.RemovedDirs)
	}
	if dirExists(t, filepath.Join(root, "SYM-1")) {
		t.Error("workspace should be removed")
	}
}

func TestGC_KeepsTerminalWorkspaceBeforeTTL(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.TTLMS = 86400000 // 24 hours

	// Recently modified workspace for a terminal issue.
	makeDir(t, filepath.Join(root, "SYM-2"), 1*time.Minute)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "SYM-2", State: "Done"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.RemovedDirs) != 0 {
		t.Errorf("RemovedDirs = %v, want empty", result.RemovedDirs)
	}
	if !dirExists(t, filepath.Join(root, "SYM-2")) {
		t.Error("workspace should still exist")
	}
}

// --- Orphan detection ---

func TestGC_RemovesOrphanWorkspacePastTTL(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.OrphanTTLMS = 1000

	makeDir(t, filepath.Join(root, "UNKNOWN-99"), 2*time.Second)

	checker := &fakeIssueChecker{} // no issues at all
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.OrphanDirs) != 1 || result.OrphanDirs[0] != "UNKNOWN-99" {
		t.Errorf("OrphanDirs = %v, want [UNKNOWN-99]", result.OrphanDirs)
	}
	if dirExists(t, filepath.Join(root, "UNKNOWN-99")) {
		t.Error("orphan workspace should be removed")
	}
}

func TestGC_KeepsOrphanWorkspaceBeforeTTL(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.OrphanTTLMS = 86400000

	makeDir(t, filepath.Join(root, "UNKNOWN-1"), 1*time.Minute)

	checker := &fakeIssueChecker{}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.OrphanDirs) != 0 {
		t.Errorf("OrphanDirs = %v, want empty", result.OrphanDirs)
	}
	if !dirExists(t, filepath.Join(root, "UNKNOWN-1")) {
		t.Error("orphan workspace should still exist")
	}
}

// --- Artifact cleanup ---

func TestGC_CleansArtifactsFromTerminalWorkspace(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.ArtifactTTLMS = 1000
	cfg.GC.TTLMS = 86400000 // full removal TTL much longer

	wsPath := filepath.Join(root, "SYM-3")
	os.MkdirAll(wsPath, 0o755)
	os.MkdirAll(filepath.Join(wsPath, "node_modules"), 0o755)
	os.MkdirAll(filepath.Join(wsPath, ".codex"), 0o755)
	os.MkdirAll(filepath.Join(wsPath, ".git"), 0o755)
	// Set parent modtime AFTER creating subdirectories.
	past := time.Now().Add(-2 * time.Second)
	os.Chtimes(wsPath, past, past)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "SYM-3", State: "Done"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.ArtifactDirs) != 1 || result.ArtifactDirs[0] != "SYM-3" {
		t.Errorf("ArtifactDirs = %v, want [SYM-3]", result.ArtifactDirs)
	}
	if dirExists(t, filepath.Join(wsPath, "node_modules")) {
		t.Error("node_modules should be removed")
	}
	if dirExists(t, filepath.Join(wsPath, ".codex")) {
		t.Error(".codex should be removed")
	}
	if !dirExists(t, filepath.Join(wsPath, ".git")) {
		t.Error(".git should be preserved")
	}
	if !dirExists(t, wsPath) {
		t.Error("workspace dir itself should still exist")
	}
}

func TestGC_SkipsArtifactsBeforeArtifactTTL(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.ArtifactTTLMS = 86400000 // 24 hours
	cfg.GC.TTLMS = 172800000

	wsPath := filepath.Join(root, "SYM-4")
	makeDir(t, wsPath, 1*time.Minute)
	makeDir(t, filepath.Join(wsPath, "node_modules"), 1*time.Minute)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "SYM-4", State: "Done"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.ArtifactDirs) != 0 {
		t.Errorf("ArtifactDirs = %v, want empty", result.ArtifactDirs)
	}
	if !dirExists(t, filepath.Join(wsPath, "node_modules")) {
		t.Error("node_modules should still exist")
	}
}

// --- Active workspaces never touched ---

func TestGC_NeverTouchesActiveWorkspaces(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.TTLMS = 1
	cfg.GC.OrphanTTLMS = 1
	cfg.GC.ArtifactTTLMS = 1

	wsPath := filepath.Join(root, "SYM-5")
	makeDir(t, wsPath, 10*time.Hour)
	makeDir(t, filepath.Join(wsPath, "node_modules"), 10*time.Hour)

	checker := &fakeIssueChecker{
		active: []internal.Issue{{Identifier: "SYM-5", State: "In Progress"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.RemovedDirs) != 0 || len(result.OrphanDirs) != 0 || len(result.ArtifactDirs) != 0 {
		t.Errorf("active workspace should not be touched: removed=%v orphans=%v artifacts=%v",
			result.RemovedDirs, result.OrphanDirs, result.ArtifactDirs)
	}
	if !dirExists(t, wsPath) {
		t.Error("active workspace should exist")
	}
}

// --- Locked workspaces ---

func TestGC_SkipsLockedWorkspaces(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.TTLMS = 1

	makeDir(t, filepath.Join(root, "SYM-6"), 10*time.Hour)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "SYM-6", State: "Done"}},
	}
	lockedFn := func() map[string]bool {
		return map[string]bool{"SYM-6": true}
	}
	gc := NewGC(cfg, checker, lockedFn, nil)
	result := gc.Collect(context.Background())

	if len(result.RemovedDirs) != 0 {
		t.Errorf("RemovedDirs = %v, want empty", result.RemovedDirs)
	}
	if !dirExists(t, filepath.Join(root, "SYM-6")) {
		t.Error("locked workspace should not be removed")
	}
}

// --- Non-existent root ---

func TestGC_NonExistentRootReturnsEmpty(t *testing.T) {
	cfg := gcConfig("/tmp/nonexistent_gc_test_root_" + t.Name())
	checker := &fakeIssueChecker{}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.Errors) != 0 {
		t.Errorf("Errors = %v, want empty", result.Errors)
	}
	if len(result.RemovedDirs) != 0 || len(result.OrphanDirs) != 0 {
		t.Error("no actions expected for missing root")
	}
}

// --- Tracker errors ---

func TestGC_TrackerErrorReturnsEarly(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	makeDir(t, filepath.Join(root, "SYM-7"), 10*time.Hour)

	checker := &fakeIssueChecker{err: context.DeadlineExceeded}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.Errors) != 1 {
		t.Errorf("Errors count = %d, want 1", len(result.Errors))
	}
	// Should not have removed anything.
	if len(result.RemovedDirs) != 0 || len(result.OrphanDirs) != 0 {
		t.Error("no actions expected when tracker fails")
	}
	if !dirExists(t, filepath.Join(root, "SYM-7")) {
		t.Error("workspace should not be removed on tracker error")
	}
}

// --- Non-directory entries skipped ---

func TestGC_SkipsNonDirectoryEntries(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.OrphanTTLMS = 1

	// Create a file (not dir) in the root.
	filePath := filepath.Join(root, "stale-file")
	if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-10 * time.Hour)
	os.Chtimes(filePath, past, past)

	checker := &fakeIssueChecker{}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.OrphanDirs) != 0 {
		t.Errorf("OrphanDirs = %v, want empty for non-directory", result.OrphanDirs)
	}
}

// --- Identifier sanitization ---

func TestGC_SanitizedIdentifierMatches(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.TTLMS = 1

	// Issue identifier has a slash; SafeIdentifier replaces it.
	makeDir(t, filepath.Join(root, "TEAM_123"), 2*time.Second)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "TEAM/123", State: "Done"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	if len(result.RemovedDirs) != 1 || result.RemovedDirs[0] != "TEAM_123" {
		t.Errorf("RemovedDirs = %v, want [TEAM_123]", result.RemovedDirs)
	}
}

// --- Unsafe artifact patterns ---

func TestGC_RejectsUnsafeArtifactPatterns(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.ArtifactTTLMS = 1
	cfg.GC.TTLMS = 86400000
	cfg.GC.ArtifactPatterns = []string{"../escape", "", ".", "..", "sub/dir"}

	wsPath := filepath.Join(root, "SYM-8")
	makeDir(t, wsPath, 2*time.Second)
	// Create a dir that might be targeted by the unsafe pattern.
	makeDir(t, filepath.Join(wsPath, ".."), 2*time.Second)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{{Identifier: "SYM-8", State: "Done"}},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	// None of the patterns are safe, so nothing should be cleaned.
	if len(result.ArtifactDirs) != 0 {
		t.Errorf("ArtifactDirs = %v, want empty for unsafe patterns", result.ArtifactDirs)
	}
}

// --- Mixed scenario ---

func TestGC_MixedScenario(t *testing.T) {
	root := t.TempDir()
	cfg := gcConfig(root)
	cfg.GC.TTLMS = 3600000         // 1 hour
	cfg.GC.OrphanTTLMS = 7200000   // 2 hours
	cfg.GC.ArtifactTTLMS = 1800000 // 30 min

	// Terminal, past TTL -> remove
	makeDir(t, filepath.Join(root, "SYM-10"), 2*time.Hour)

	// Terminal, past artifact TTL but not full TTL -> clean artifacts
	ws11 := filepath.Join(root, "SYM-11")
	os.MkdirAll(ws11, 0o755)
	os.MkdirAll(filepath.Join(ws11, "node_modules"), 0o755)
	past45 := time.Now().Add(-45 * time.Minute)
	os.Chtimes(ws11, past45, past45)

	// Active -> skip
	makeDir(t, filepath.Join(root, "SYM-12"), 10*time.Hour)

	// Orphan, past orphan TTL -> remove
	makeDir(t, filepath.Join(root, "GONE-1"), 3*time.Hour)

	// Orphan, young -> keep
	makeDir(t, filepath.Join(root, "GONE-2"), 30*time.Minute)

	checker := &fakeIssueChecker{
		terminal: []internal.Issue{
			{Identifier: "SYM-10", State: "Done"},
			{Identifier: "SYM-11", State: "Canceled"},
		},
		active: []internal.Issue{
			{Identifier: "SYM-12", State: "In Progress"},
		},
	}
	gc := NewGC(cfg, checker, nil, nil)
	result := gc.Collect(context.Background())

	// Check terminal removal
	if len(result.RemovedDirs) != 1 || result.RemovedDirs[0] != "SYM-10" {
		t.Errorf("RemovedDirs = %v, want [SYM-10]", result.RemovedDirs)
	}

	// Check artifact cleanup
	if len(result.ArtifactDirs) != 1 || result.ArtifactDirs[0] != "SYM-11" {
		t.Errorf("ArtifactDirs = %v, want [SYM-11]", result.ArtifactDirs)
	}

	// Check orphan removal
	if len(result.OrphanDirs) != 1 || result.OrphanDirs[0] != "GONE-1" {
		t.Errorf("OrphanDirs = %v, want [GONE-1]", result.OrphanDirs)
	}

	// Active workspace preserved
	if !dirExists(t, filepath.Join(root, "SYM-12")) {
		t.Error("active workspace SYM-12 should still exist")
	}

	// Young orphan preserved
	if !dirExists(t, filepath.Join(root, "GONE-2")) {
		t.Error("young orphan GONE-2 should still exist")
	}
}

// --- isSafeArtifactPattern ---

func TestIsSafeArtifactPattern(t *testing.T) {
	tests := []struct {
		pattern string
		want    bool
	}{
		{"node_modules", true},
		{".codex", true},
		{"build", true},
		{"", false},
		{".", false},
		{"..", false},
		{"sub/dir", false},
		{"sub\\dir", false},
		{"../escape", false},
	}
	for _, tt := range tests {
		if got := isSafeArtifactPattern(tt.pattern); got != tt.want {
			t.Errorf("isSafeArtifactPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
		}
	}
}
