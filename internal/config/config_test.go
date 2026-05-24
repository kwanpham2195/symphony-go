package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromMap_Defaults(t *testing.T) {
	cfg, err := FromMap(map[string]any{})
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Polling.IntervalMS != 30000 {
		t.Errorf("polling.interval_ms = %d, want 30000", cfg.Polling.IntervalMS)
	}
	if cfg.Agent.MaxConcurrentAgents != 10 {
		t.Errorf("agent.max_concurrent_agents = %d, want 10", cfg.Agent.MaxConcurrentAgents)
	}
	if cfg.Agent.MaxTurns != 20 {
		t.Errorf("agent.max_turns = %d, want 20", cfg.Agent.MaxTurns)
	}
	if cfg.Agent.MaxRetryBackoffMS != 300000 {
		t.Errorf("agent.max_retry_backoff_ms = %d, want 300000", cfg.Agent.MaxRetryBackoffMS)
	}
	if cfg.Codex.Command != "codex app-server" {
		t.Errorf("codex.command = %q", cfg.Codex.Command)
	}
	if cfg.Codex.TurnTimeoutMS != 3600000 {
		t.Errorf("codex.turn_timeout_ms = %d", cfg.Codex.TurnTimeoutMS)
	}
	if cfg.Codex.ReadTimeoutMS != 5000 {
		t.Errorf("codex.read_timeout_ms = %d", cfg.Codex.ReadTimeoutMS)
	}
	if cfg.Codex.StallTimeoutMS != 300000 {
		t.Errorf("codex.stall_timeout_ms = %d", cfg.Codex.StallTimeoutMS)
	}
	if cfg.Hooks.TimeoutMS != 60000 {
		t.Errorf("hooks.timeout_ms = %d", cfg.Hooks.TimeoutMS)
	}
	if cfg.Tracker.Endpoint != "https://api.linear.app/graphql" {
		t.Errorf("tracker.endpoint = %q", cfg.Tracker.Endpoint)
	}
	wantStates := []string{"Todo", "In Progress"}
	if len(cfg.Tracker.ActiveStates) != len(wantStates) {
		t.Errorf("active_states = %v", cfg.Tracker.ActiveStates)
	}
	if cfg.Server.Host != "127.0.0.1" {
		t.Errorf("server.host = %q", cfg.Server.Host)
	}
}

func TestFromMap_Overrides(t *testing.T) {
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"project_slug": "my-proj",
			"api_key":      "tok_123",
		},
		"polling": map[string]any{
			"interval_ms": 5000,
		},
		"agent": map[string]any{
			"max_concurrent_agents": 3,
			"max_turns":             5,
		},
		"codex": map[string]any{
			"command":          "custom-codex app-server",
			"stall_timeout_ms": 0,
		},
		"server": map[string]any{
			"port": 8080,
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Tracker.Kind != "linear" {
		t.Errorf("tracker.kind = %q", cfg.Tracker.Kind)
	}
	if cfg.Tracker.ProjectSlug != "my-proj" {
		t.Errorf("tracker.project_slug = %q", cfg.Tracker.ProjectSlug)
	}
	if cfg.Tracker.APIKey != "tok_123" {
		t.Errorf("tracker.api_key = %q", cfg.Tracker.APIKey)
	}
	if cfg.Polling.IntervalMS != 5000 {
		t.Errorf("polling.interval_ms = %d", cfg.Polling.IntervalMS)
	}
	if cfg.Agent.MaxConcurrentAgents != 3 {
		t.Errorf("agent.max_concurrent_agents = %d", cfg.Agent.MaxConcurrentAgents)
	}
	if cfg.Agent.MaxTurns != 5 {
		t.Errorf("agent.max_turns = %d", cfg.Agent.MaxTurns)
	}
	if cfg.Codex.Command != "custom-codex app-server" {
		t.Errorf("codex.command = %q", cfg.Codex.Command)
	}
	if cfg.Codex.StallTimeoutMS != 0 {
		t.Errorf("codex.stall_timeout_ms = %d, want 0", cfg.Codex.StallTimeoutMS)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("server.port = %d", cfg.Server.Port)
	}
}

func TestFromMap_EnvResolution(t *testing.T) {
	t.Setenv("TEST_SYMPHONY_KEY", "resolved_key")
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"project_slug": "proj",
			"api_key":      "$TEST_SYMPHONY_KEY",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Tracker.APIKey != "resolved_key" {
		t.Errorf("api_key = %q, want resolved_key", cfg.Tracker.APIKey)
	}
}

func TestFromMap_EnvFallback(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "env_fallback_key")
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"project_slug": "proj",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Tracker.APIKey != "env_fallback_key" {
		t.Errorf("api_key = %q, want env_fallback_key", cfg.Tracker.APIKey)
	}
}

func TestFromMap_EnvEmptyResolvesToEmpty(t *testing.T) {
	t.Setenv("EMPTY_VAR", "")
	raw := map[string]any{
		"tracker": map[string]any{
			"kind":         "linear",
			"project_slug": "proj",
			"api_key":      "$EMPTY_VAR",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Tracker.APIKey != "" {
		t.Errorf("api_key = %q, want empty", cfg.Tracker.APIKey)
	}
}

func TestFromMap_TildeExpansion(t *testing.T) {
	raw := map[string]any{
		"workspace": map[string]any{
			"root": "~/my_workspaces",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "my_workspaces")
	if cfg.Workspace.Root != want {
		t.Errorf("workspace.root = %q, want %q", cfg.Workspace.Root, want)
	}
}

func TestFromMap_WorkspaceEnvPath(t *testing.T) {
	t.Setenv("TEST_WS_ROOT", "/custom/ws")
	raw := map[string]any{
		"workspace": map[string]any{
			"root": "$TEST_WS_ROOT",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Workspace.Root != "/custom/ws" {
		t.Errorf("workspace.root = %q", cfg.Workspace.Root)
	}
}

func TestFromMap_StateLimitsNormalized(t *testing.T) {
	raw := map[string]any{
		"agent": map[string]any{
			"max_concurrent_agents_by_state": map[string]any{
				"Todo":        3,
				"In Progress": 5,
				"":            1, // blank key ignored
			},
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Agent.MaxConcurrentAgentsByState["todo"] != 3 {
		t.Errorf("by_state[todo] = %d", cfg.Agent.MaxConcurrentAgentsByState["todo"])
	}
	if cfg.Agent.MaxConcurrentAgentsByState["in progress"] != 5 {
		t.Errorf("by_state[in progress] = %d", cfg.Agent.MaxConcurrentAgentsByState["in progress"])
	}
	if _, exists := cfg.Agent.MaxConcurrentAgentsByState[""]; exists {
		t.Error("blank state key should be dropped")
	}
}

func TestFromMap_HooksConfig(t *testing.T) {
	raw := map[string]any{
		"hooks": map[string]any{
			"after_create":  "git clone .",
			"before_run":    "npm install",
			"after_run":     "echo done",
			"before_remove": "echo removing",
			"timeout_ms":    30000,
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Hooks.AfterCreate != "git clone ." {
		t.Errorf("after_create = %q", cfg.Hooks.AfterCreate)
	}
	if cfg.Hooks.BeforeRun != "npm install" {
		t.Errorf("before_run = %q", cfg.Hooks.BeforeRun)
	}
	if cfg.Hooks.TimeoutMS != 30000 {
		t.Errorf("timeout_ms = %d", cfg.Hooks.TimeoutMS)
	}
}

func TestFromMap_HooksTimeoutZeroKeepsDefault(t *testing.T) {
	raw := map[string]any{
		"hooks": map[string]any{
			"timeout_ms": 0,
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Hooks.TimeoutMS != 60000 {
		t.Errorf("timeout_ms = %d, want 60000 (non-positive falls to default)", cfg.Hooks.TimeoutMS)
	}
}

func TestFromMap_IntFromString(t *testing.T) {
	raw := map[string]any{
		"polling": map[string]any{
			"interval_ms": "15000",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Polling.IntervalMS != 15000 {
		t.Errorf("interval_ms = %d, want 15000", cfg.Polling.IntervalMS)
	}
}

func TestFromMap_ApprovalPolicyMap(t *testing.T) {
	raw := map[string]any{
		"codex": map[string]any{
			"approval_policy": map[string]any{
				"reject": map[string]any{
					"sandbox_approval": true,
				},
			},
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	m, ok := cfg.Codex.ApprovalPolicy.(map[string]any)
	if !ok {
		t.Fatalf("approval_policy type = %T, want map", cfg.Codex.ApprovalPolicy)
	}
	if _, ok := m["reject"]; !ok {
		t.Error("approval_policy missing 'reject' key")
	}
}

func TestFromMap_ApprovalPolicyString(t *testing.T) {
	raw := map[string]any{
		"codex": map[string]any{
			"approval_policy": "never",
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	s, ok := cfg.Codex.ApprovalPolicy.(string)
	if !ok || s != "never" {
		t.Errorf("approval_policy = %v (%T)", cfg.Codex.ApprovalPolicy, cfg.Codex.ApprovalPolicy)
	}
}

func TestFromMap_TurnSandboxPolicy(t *testing.T) {
	raw := map[string]any{
		"codex": map[string]any{
			"turn_sandbox_policy": map[string]any{
				"type": "workspaceWrite",
			},
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Codex.TurnSandboxPolicy["type"] != "workspaceWrite" {
		t.Errorf("turn_sandbox_policy.type = %v", cfg.Codex.TurnSandboxPolicy["type"])
	}
}

func TestFromMap_WorkerConfig(t *testing.T) {
	raw := map[string]any{
		"worker": map[string]any{
			"ssh_hosts":                      []any{"host1", "host2"},
			"max_concurrent_agents_per_host": 4,
		},
	}
	cfg, err := FromMap(raw)
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if len(cfg.Worker.SSHHosts) != 2 || cfg.Worker.SSHHosts[0] != "host1" {
		t.Errorf("ssh_hosts = %v", cfg.Worker.SSHHosts)
	}
	if cfg.Worker.MaxConcurrentAgentsPerHost != 4 {
		t.Errorf("max_concurrent_agents_per_host = %d", cfg.Worker.MaxConcurrentAgentsPerHost)
	}
}

// --- Validation tests ---

func TestValidate_Valid(t *testing.T) {
	cfg := validConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestValidate_MissingTrackerKind(t *testing.T) {
	cfg := validConfig()
	cfg.Tracker.Kind = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing tracker.kind") {
		t.Fatalf("expected missing tracker.kind, got: %v", err)
	}
}

func TestValidate_UnsupportedTrackerKind(t *testing.T) {
	cfg := validConfig()
	cfg.Tracker.Kind = "jira"
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "unsupported tracker.kind") {
		t.Fatalf("expected unsupported tracker.kind, got: %v", err)
	}
}

func TestValidate_MissingAPIKey(t *testing.T) {
	cfg := validConfig()
	cfg.Tracker.APIKey = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing tracker.api_key") {
		t.Fatalf("expected missing tracker.api_key, got: %v", err)
	}
}

func TestValidate_MissingProjectSlug(t *testing.T) {
	cfg := validConfig()
	cfg.Tracker.ProjectSlug = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing tracker.project_slug") {
		t.Fatalf("expected missing tracker.project_slug, got: %v", err)
	}
}

func TestValidate_MissingCodexCommand(t *testing.T) {
	cfg := validConfig()
	cfg.Codex.Command = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing codex.command") {
		t.Fatalf("expected missing codex.command, got: %v", err)
	}
}

func TestFromMap_RunnerDefault(t *testing.T) {
	cfg, err := FromMap(map[string]any{})
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Runner != "codex" {
		t.Errorf("Runner = %q, want codex", cfg.Runner)
	}
}

func TestFromMap_RunnerPi(t *testing.T) {
	cfg, err := FromMap(map[string]any{
		"runner": "pi",
		"pi": map[string]any{
			"command":         "pi --mode rpc --no-session",
			"read_timeout_ms": 15000,
			"turn_timeout_ms": 300000,
		},
	})
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Runner != "pi" {
		t.Errorf("Runner = %q, want pi", cfg.Runner)
	}
	if cfg.Pi.Command != "pi --mode rpc --no-session" {
		t.Errorf("Pi.Command = %q, want pi --mode rpc --no-session", cfg.Pi.Command)
	}
	if cfg.Pi.ReadTimeoutMS != 15000 {
		t.Errorf("Pi.ReadTimeoutMS = %d, want 15000", cfg.Pi.ReadTimeoutMS)
	}
	if cfg.Pi.TurnTimeoutMS != 300000 {
		t.Errorf("Pi.TurnTimeoutMS = %d, want 300000", cfg.Pi.TurnTimeoutMS)
	}
}

func TestFromMap_PiDefaults(t *testing.T) {
	cfg, err := FromMap(map[string]any{
		"runner": "pi",
	})
	if err != nil {
		t.Fatalf("FromMap error: %v", err)
	}
	if cfg.Pi.Command != "pi --mode rpc --no-session" {
		t.Errorf("Pi.Command = %q, want default", cfg.Pi.Command)
	}
	if cfg.Pi.ReadTimeoutMS != 30000 {
		t.Errorf("Pi.ReadTimeoutMS = %d, want 30000", cfg.Pi.ReadTimeoutMS)
	}
	if cfg.Pi.TurnTimeoutMS != 600000 {
		t.Errorf("Pi.TurnTimeoutMS = %d, want 600000", cfg.Pi.TurnTimeoutMS)
	}
}

func TestValidate_PiRunner_Valid(t *testing.T) {
	cfg := validPiConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidate_PiRunner_MissingPiCommand(t *testing.T) {
	cfg := validPiConfig()
	cfg.Pi.Command = ""
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "missing pi.command") {
		t.Fatalf("expected missing pi.command, got: %v", err)
	}
}

func TestValidate_PiRunner_DoesNotRequireCodexCommand(t *testing.T) {
	cfg := validPiConfig()
	cfg.Codex.Command = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("pi runner should not require codex.command, got: %v", err)
	}
}

func TestValidate_CodexRunner_DoesNotRequirePiCommand(t *testing.T) {
	cfg := validConfig()
	cfg.Pi.Command = ""
	if err := cfg.Validate(); err != nil {
		t.Fatalf("codex runner should not require pi.command, got: %v", err)
	}
}

// --- helpers ---

func validConfig() *Config {
	return &Config{
		Tracker: TrackerConfig{
			Kind:        "linear",
			APIKey:      "tok_test",
			ProjectSlug: "test-proj",
		},
		Codex: CodexConfig{
			Command: "codex app-server",
		},
	}
}

func validPiConfig() *Config {
	return &Config{
		Runner: "pi",
		Tracker: TrackerConfig{
			Kind:        "linear",
			APIKey:      "tok_test",
			ProjectSlug: "test-proj",
		},
		Pi: PiConfig{
			Command: "pi --mode rpc --no-session",
		},
	}
}
