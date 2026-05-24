// Package config provides typed configuration derived from WORKFLOW.md front
// matter. It applies defaults, resolves $VAR environment indirection for
// secrets and paths, expands ~ in path fields, and validates required fields.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Sentinel validation errors.
var (
	ErrMissingTrackerKind        = errors.New("missing tracker.kind")
	ErrUnsupportedTrackerKind    = errors.New("unsupported tracker.kind")
	ErrMissingTrackerAPIKey      = errors.New("missing tracker.api_key (set $LINEAR_API_KEY or tracker.api_key)")
	ErrMissingTrackerProjectSlug = errors.New("missing tracker.project_slug")
	ErrMissingCodexCommand       = errors.New("missing codex.command")
	ErrMissingPiCommand          = errors.New("missing pi.command")
	ErrUnsupportedRunner         = errors.New("unsupported runner")
)

// Config is the typed runtime configuration for symphony.
type Config struct {
	Runner    string // "codex" (default) or "pi"
	Tracker   TrackerConfig
	Polling   PollingConfig
	Workspace WorkspaceConfig
	Worker    WorkerConfig
	Hooks     HooksConfig
	Agent     AgentConfig
	Codex     CodexConfig
	Pi        PiConfig
	Server    ServerConfig
}

// TrackerConfig holds issue tracker settings.
type TrackerConfig struct {
	Kind           string
	Endpoint       string
	APIKey         string
	ProjectSlug    string
	Assignee       string
	ActiveStates   []string
	TerminalStates []string
}

// PollingConfig holds poll loop settings.
type PollingConfig struct {
	IntervalMS int
}

// WorkspaceConfig holds workspace root path.
type WorkspaceConfig struct {
	Root string
}

// WorkerConfig holds worker pool settings.
type WorkerConfig struct {
	SSHHosts                   []string
	MaxConcurrentAgentsPerHost int
}

// HooksConfig holds workspace lifecycle hook scripts.
type HooksConfig struct {
	AfterCreate  string
	BeforeRun    string
	AfterRun     string
	BeforeRemove string
	TimeoutMS    int
}

// AgentConfig holds agent concurrency and retry settings.
type AgentConfig struct {
	MaxConcurrentAgents        int
	MaxTurns                   int
	MaxRetryBackoffMS          int
	MaxConcurrentAgentsByState map[string]int
}

// CodexConfig holds codex app-server launch settings.
type CodexConfig struct {
	Command           string
	ApprovalPolicy    any
	ThreadSandbox     string
	TurnSandboxPolicy map[string]any
	TurnTimeoutMS     int
	ReadTimeoutMS     int
	StallTimeoutMS    int
}

// ServerConfig holds optional HTTP server settings.
type ServerConfig struct {
	Port int // 0 = disabled, >0 = bind that port
	Host string
}

// PiConfig holds Pi RPC agent launch settings.
type PiConfig struct {
	Command       string
	TurnTimeoutMS int
	ReadTimeoutMS int
}

// FromMap builds a Config from raw front matter map, applying defaults and
// resolving environment variables.
func FromMap(raw map[string]any) (*Config, error) {
	c := &Config{}
	c.applyDefaults()
	c.applyRaw(raw)
	c.resolveEnv()
	c.expandPaths()
	return c, nil
}

// Validate checks required fields and returns the first validation error.
func (c *Config) Validate() error {
	if c.Tracker.Kind == "" {
		return ErrMissingTrackerKind
	}
	if c.Tracker.Kind != "linear" {
		return fmt.Errorf("%w: %q", ErrUnsupportedTrackerKind, c.Tracker.Kind)
	}
	if c.Tracker.APIKey == "" {
		return ErrMissingTrackerAPIKey
	}
	if c.Tracker.ProjectSlug == "" {
		return ErrMissingTrackerProjectSlug
	}
	switch c.Runner {
	case "pi":
		if c.Pi.Command == "" {
			return ErrMissingPiCommand
		}
	case "codex", "":
		if c.Codex.Command == "" {
			return ErrMissingCodexCommand
		}
	default:
		return fmt.Errorf("%w: %q (valid: codex, pi)", ErrUnsupportedRunner, c.Runner)
	}
	return nil
}

// applyDefaults sets spec-defined default values.
func (c *Config) applyDefaults() {
	c.Tracker.Endpoint = "https://api.linear.app/graphql"
	c.Tracker.ActiveStates = []string{"Todo", "In Progress"}
	c.Tracker.TerminalStates = []string{"Closed", "Cancelled", "Canceled", "Duplicate", "Done"}
	c.Polling.IntervalMS = 30000
	c.Workspace.Root = filepath.Join(os.TempDir(), "symphony_workspaces")
	c.Hooks.TimeoutMS = 60000
	c.Agent.MaxConcurrentAgents = 10
	c.Agent.MaxTurns = 20
	c.Agent.MaxRetryBackoffMS = 300000
	c.Agent.MaxConcurrentAgentsByState = map[string]int{}
	c.Codex.Command = "codex app-server"
	c.Codex.ThreadSandbox = "workspace-write"
	c.Codex.TurnTimeoutMS = 3600000
	c.Codex.ReadTimeoutMS = 5000
	c.Codex.StallTimeoutMS = 300000
	c.Server.Host = "127.0.0.1"
	c.Runner = "codex"
	c.Pi.Command = "pi --mode rpc --no-session"
	c.Pi.TurnTimeoutMS = 600000
	c.Pi.ReadTimeoutMS = 30000
}

// applyRaw overlays raw front-matter values onto the config, overriding
// defaults only when the front matter provides a value.
func (c *Config) applyRaw(raw map[string]any) {
	if tracker, ok := getMap(raw, "tracker"); ok {
		if v, ok := getString(tracker, "kind"); ok {
			c.Tracker.Kind = v
		}
		if v, ok := getString(tracker, "endpoint"); ok {
			c.Tracker.Endpoint = v
		}
		if v, ok := getString(tracker, "api_key"); ok {
			c.Tracker.APIKey = v
		}
		if v, ok := getString(tracker, "project_slug"); ok {
			c.Tracker.ProjectSlug = v
		}
		if v, ok := getString(tracker, "assignee"); ok {
			c.Tracker.Assignee = v
		}
		if v, ok := getStringSlice(tracker, "active_states"); ok {
			c.Tracker.ActiveStates = v
		}
		if v, ok := getStringSlice(tracker, "terminal_states"); ok {
			c.Tracker.TerminalStates = v
		}
	}

	if polling, ok := getMap(raw, "polling"); ok {
		if v, ok := getInt(polling, "interval_ms"); ok {
			c.Polling.IntervalMS = v
		}
	}

	if ws, ok := getMap(raw, "workspace"); ok {
		if v, ok := getString(ws, "root"); ok {
			c.Workspace.Root = v
		}
	}

	if worker, ok := getMap(raw, "worker"); ok {
		if v, ok := getStringSlice(worker, "ssh_hosts"); ok {
			c.Worker.SSHHosts = v
		}
		if v, ok := getInt(worker, "max_concurrent_agents_per_host"); ok {
			c.Worker.MaxConcurrentAgentsPerHost = v
		}
	}

	if hooks, ok := getMap(raw, "hooks"); ok {
		if v, ok := getString(hooks, "after_create"); ok {
			c.Hooks.AfterCreate = v
		}
		if v, ok := getString(hooks, "before_run"); ok {
			c.Hooks.BeforeRun = v
		}
		if v, ok := getString(hooks, "after_run"); ok {
			c.Hooks.AfterRun = v
		}
		if v, ok := getString(hooks, "before_remove"); ok {
			c.Hooks.BeforeRemove = v
		}
		if v, ok := getInt(hooks, "timeout_ms"); ok && v > 0 {
			c.Hooks.TimeoutMS = v
		}
	}

	if agent, ok := getMap(raw, "agent"); ok {
		if v, ok := getInt(agent, "max_concurrent_agents"); ok {
			c.Agent.MaxConcurrentAgents = v
		}
		if v, ok := getInt(agent, "max_turns"); ok {
			c.Agent.MaxTurns = v
		}
		if v, ok := getInt(agent, "max_retry_backoff_ms"); ok {
			c.Agent.MaxRetryBackoffMS = v
		}
		if byState, ok := getMap(agent, "max_concurrent_agents_by_state"); ok {
			c.Agent.MaxConcurrentAgentsByState = normalizeStateLimits(byState)
		}
	}

	if codex, ok := getMap(raw, "codex"); ok {
		if v, ok := getString(codex, "command"); ok {
			c.Codex.Command = v
		}
		// approval_policy can be string or map
		if v, exists := codex["approval_policy"]; exists {
			c.Codex.ApprovalPolicy = v
		}
		if v, ok := getString(codex, "thread_sandbox"); ok {
			c.Codex.ThreadSandbox = v
		}
		if v, ok := getMap(codex, "turn_sandbox_policy"); ok {
			c.Codex.TurnSandboxPolicy = v
		}
		if v, ok := getInt(codex, "turn_timeout_ms"); ok {
			c.Codex.TurnTimeoutMS = v
		}
		if v, ok := getInt(codex, "read_timeout_ms"); ok {
			c.Codex.ReadTimeoutMS = v
		}
		if v, ok := getInt(codex, "stall_timeout_ms"); ok {
			c.Codex.StallTimeoutMS = v
		}
	}

	if server, ok := getMap(raw, "server"); ok {
		if v, ok := getInt(server, "port"); ok {
			c.Server.Port = v
		}
		if v, ok := getString(server, "host"); ok {
			c.Server.Host = v
		}
	}

	if v, ok := getString(raw, "runner"); ok {
		c.Runner = v
	}

	if pi, ok := getMap(raw, "pi"); ok {
		if v, ok := getString(pi, "command"); ok {
			c.Pi.Command = v
		}
		if v, ok := getInt(pi, "turn_timeout_ms"); ok {
			c.Pi.TurnTimeoutMS = v
		}
		if v, ok := getInt(pi, "read_timeout_ms"); ok {
			c.Pi.ReadTimeoutMS = v
		}
	}
}

// resolveEnv resolves $VAR references in secret and path fields.
func (c *Config) resolveEnv() {
	c.Tracker.APIKey = resolveEnvSecret(c.Tracker.APIKey, "LINEAR_API_KEY")
	c.Tracker.Assignee = resolveEnvSecret(c.Tracker.Assignee, "LINEAR_ASSIGNEE")
	c.Workspace.Root = resolveEnvPath(c.Workspace.Root)
}

// expandPaths expands ~ prefix in path fields.
func (c *Config) expandPaths() {
	c.Workspace.Root = expandHome(c.Workspace.Root)
}

// resolveEnvSecret resolves a $VAR reference. If the value starts with "$",
// look up the env var. If the value is empty, fall back to the canonical env
// var. Empty resolved values become "".
func resolveEnvSecret(value, canonicalEnv string) string {
	if value == "" {
		return envOrEmpty(canonicalEnv)
	}
	if name, ok := envRefName(value); ok {
		return envOrEmpty(name)
	}
	return value
}

// resolveEnvPath resolves a $VAR reference for path fields.
func resolveEnvPath(value string) string {
	if name, ok := envRefName(value); ok {
		v := os.Getenv(name)
		if v == "" {
			return ""
		}
		return v
	}
	return value
}

// envRefName returns the env var name if value looks like "$VAR_NAME".
func envRefName(value string) (string, bool) {
	if !strings.HasPrefix(value, "$") {
		return "", false
	}
	name := value[1:]
	for _, c := range name {
		if (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return "", false
		}
	}
	if len(name) == 0 {
		return "", false
	}
	// First char must not be digit
	if name[0] >= '0' && name[0] <= '9' {
		return "", false
	}
	return name, true
}

func envOrEmpty(name string) string {
	return os.Getenv(name)
}

// expandHome expands a leading "~" or "~/" to the user's home directory.
func expandHome(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[1:])
	}
	return path
}

// normalizeStateLimits converts a map[string]any to map[string]int with
// lowercased keys. Invalid entries are dropped.
func normalizeStateLimits(raw map[string]any) map[string]int {
	out := make(map[string]int, len(raw))
	for k, v := range raw {
		key := strings.ToLower(k)
		if key == "" {
			continue
		}
		n, ok := toInt(v)
		if !ok || n <= 0 {
			continue
		}
		out[key] = n
	}
	return out
}

// --- helpers for reading map values ---

func getMap(m map[string]any, key string) (map[string]any, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	sub, ok := v.(map[string]any)
	return sub, ok
}

func getString(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func getInt(m map[string]any, key string) (int, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	return toInt(v)
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	default:
		return 0, false
	}
}

func getStringSlice(m map[string]any, key string) ([]string, bool) {
	v, ok := m[key]
	if !ok {
		return nil, false
	}
	arr, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out, true
}
