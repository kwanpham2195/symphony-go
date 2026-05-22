package worker

import (
	"context"
	"testing"
	"time"
)

// --- Pool tests ---

func TestPool_LocalMode(t *testing.T) {
	pool := NewPool(nil, 0)
	if pool.IsSSH() {
		t.Error("expected local mode")
	}

	host, ok := pool.SelectHost("")
	if !ok || host != "" {
		t.Errorf("expected empty host for local, got %q ok=%v", host, ok)
	}
}

func TestPool_SSHMode_SelectsHost(t *testing.T) {
	pool := NewPool([]string{"host-a", "host-b"}, 2)
	if !pool.IsSSH() {
		t.Error("expected SSH mode")
	}

	host, ok := pool.SelectHost("")
	if !ok {
		t.Fatal("expected available host")
	}
	if host != "host-a" && host != "host-b" {
		t.Errorf("unexpected host %q", host)
	}
}

func TestPool_PreferredHost(t *testing.T) {
	pool := NewPool([]string{"host-a", "host-b"}, 2)

	host, ok := pool.SelectHost("host-b")
	if !ok {
		t.Fatal("expected available")
	}
	if host != "host-b" {
		t.Errorf("expected preferred host-b, got %q", host)
	}
}

func TestPool_CapacityLimit(t *testing.T) {
	pool := NewPool([]string{"host-a"}, 1)

	pool.Acquire("host-a")

	_, ok := pool.SelectHost("")
	if ok {
		t.Error("expected no capacity after 1 acquire with limit 1")
	}
}

func TestPool_CapacityRelease(t *testing.T) {
	pool := NewPool([]string{"host-a"}, 1)

	pool.Acquire("host-a")
	pool.Release("host-a")

	host, ok := pool.SelectHost("")
	if !ok {
		t.Fatal("expected capacity after release")
	}
	if host != "host-a" {
		t.Errorf("host = %q", host)
	}
}

func TestPool_LeastLoaded(t *testing.T) {
	pool := NewPool([]string{"host-a", "host-b"}, 3)

	pool.Acquire("host-a")
	pool.Acquire("host-a")
	pool.Acquire("host-b")

	host, ok := pool.SelectHost("")
	if !ok {
		t.Fatal("expected available")
	}
	if host != "host-b" {
		t.Errorf("expected least loaded host-b (1 vs 2), got %q", host)
	}
}

func TestPool_AllFull(t *testing.T) {
	pool := NewPool([]string{"host-a", "host-b"}, 1)

	pool.Acquire("host-a")
	pool.Acquire("host-b")

	_, ok := pool.SelectHost("")
	if ok {
		t.Error("expected no capacity when all hosts full")
	}
}

func TestPool_PreferredHostFull_FallsBack(t *testing.T) {
	pool := NewPool([]string{"host-a", "host-b"}, 1)

	pool.Acquire("host-a")

	host, ok := pool.SelectHost("host-a")
	if !ok {
		t.Fatal("expected fallback")
	}
	if host != "host-b" {
		t.Errorf("expected fallback to host-b, got %q", host)
	}
}

func TestPool_NoPerHostLimit(t *testing.T) {
	pool := NewPool([]string{"host-a"}, 0) // 0 = no limit

	pool.Acquire("host-a")
	pool.Acquire("host-a")
	pool.Acquire("host-a")

	_, ok := pool.SelectHost("")
	if !ok {
		t.Error("expected no capacity limit when maxConcurrentPerHost=0")
	}
}

func TestPool_HostCounts(t *testing.T) {
	pool := NewPool([]string{"host-a", "host-b"}, 5)

	pool.Acquire("host-a")
	pool.Acquire("host-a")
	pool.Acquire("host-b")

	counts := pool.HostCounts()
	if counts["host-a"] != 2 {
		t.Errorf("host-a = %d", counts["host-a"])
	}
	if counts["host-b"] != 1 {
		t.Errorf("host-b = %d", counts["host-b"])
	}
}

// --- LauncherForHost tests ---

func TestLauncherForHost_Local(t *testing.T) {
	l := LauncherForHost("")
	if l.Host() != "" {
		t.Errorf("expected local launcher, got host=%q", l.Host())
	}
}

func TestLauncherForHost_SSH(t *testing.T) {
	l := LauncherForHost("remote-host")
	if l.Host() != "remote-host" {
		t.Errorf("expected SSH launcher for remote-host, got %q", l.Host())
	}
}

// --- LocalLauncher tests ---

func TestLocalLauncher_RunHook(t *testing.T) {
	l := NewLocalLauncher()
	dir := t.TempDir()

	result, err := l.RunHook(context.Background(), dir, "echo hello", 5*time.Second)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d", result.ExitCode)
	}
}

func TestLocalLauncher_RunHook_Failure(t *testing.T) {
	l := NewLocalLauncher()
	dir := t.TempDir()

	result, err := l.RunHook(context.Background(), dir, "exit 42", 5*time.Second)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if result.ExitCode != 42 {
		t.Errorf("exit code = %d, want 42", result.ExitCode)
	}
}

func TestLocalLauncher_RunHook_Timeout(t *testing.T) {
	l := NewLocalLauncher()
	dir := t.TempDir()

	_, err := l.RunHook(context.Background(), dir, "sleep 30", 200*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
}

func TestLocalLauncher_Start(t *testing.T) {
	l := NewLocalLauncher()
	dir := t.TempDir()

	proc, err := l.Start(context.Background(), dir, "echo started")
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	_ = proc.Cmd.Wait()
}

// --- shellEscape tests ---

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"hello", "'hello'"},
		{"it's", "'it'\"'\"'s'"},
		{"/path/to/dir", "'/path/to/dir'"},
	}
	for _, tt := range tests {
		got := shellEscape(tt.input)
		if got != tt.want {
			t.Errorf("shellEscape(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
