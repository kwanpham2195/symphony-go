// Package codex implements the Codex app-server JSON line protocol client.
//
// The client launches a subprocess via "bash -lc <command>", sends the
// initialize/initialized/thread/start/turn/start handshake, and processes
// streaming turn events from stdout.
package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/kwanpham2195/symphony-go/internal/codex/tools"
	"github.com/kwanpham2195/symphony-go/internal/config"
	"github.com/kwanpham2195/symphony-go/internal/domain"
)

const (
	initializeID  = 1
	threadStartID = 2
	turnStartID   = 3
	maxLineBytes  = 10 * 1024 * 1024 // 10 MB
)

// TurnResult describes how a turn ended.
type TurnResult struct {
	Status    string         // "completed", "failed", "cancelled", "timeout", "exit", "input_required"
	SessionID string
	ThreadID  string
	TurnID    string
	Details   map[string]any
}

// Session holds a running app-server process and its protocol state.
type Session struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	stderr    io.ReadCloser
	threadID  string
	workspace string
	pid       int

	mu     sync.Mutex
	closed bool
}

// Client manages codex app-server sessions.
type Client struct {
	cfg    *config.Config
	logger *slog.Logger
	tools  map[string]tools.Tool // registered dynamic tools
}

// NewClient creates a codex client.
func NewClient(cfg *config.Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{cfg: cfg, logger: logger, tools: make(map[string]tools.Tool)}
}

// RegisterTool adds a dynamic tool that will be advertised in thread/start
// and dispatched on item/tool/call.
func (c *Client) RegisterTool(t tools.Tool) {
	c.tools[t.Name()] = t
}

// UpdateConfig replaces the config (for dynamic reload).
func (c *Client) UpdateConfig(cfg *config.Config) {
	c.cfg = cfg
}

// StartSession launches the app-server subprocess and completes the handshake
// (initialize, initialized, thread/start).
func (c *Client) StartSession(ctx context.Context, workspace string) (*Session, error) {
	cmd := exec.CommandContext(ctx, "bash", "-lc", c.cfg.Codex.Command)
	cmd.Dir = workspace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("codex: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex_not_found: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)

	sess := &Session{
		cmd:       cmd,
		stdin:     stdin,
		stdout:    scanner,
		stderr:    stderr,
		workspace: workspace,
		pid:       cmd.Process.Pid,
	}

	// Drain stderr in background
	go c.drainStderr(sess)

	// Handshake
	readTimeout := time.Duration(c.cfg.Codex.ReadTimeoutMS) * time.Millisecond

	// 1. initialize
	if err := sess.send(map[string]any{
		"id":     initializeID,
		"method": "initialize",
		"params": map[string]any{
			"capabilities": map[string]any{
				"experimentalApi": true,
			},
			"clientInfo": map[string]any{
				"name":    "symphony-orchestrator",
				"title":   "Symphony Orchestrator",
				"version": "0.1.0",
			},
		},
	}); err != nil {
		sess.Close()
		return nil, fmt.Errorf("codex: send initialize: %w", err)
	}

	if _, err := sess.awaitResponse(initializeID, readTimeout); err != nil {
		sess.Close()
		return nil, fmt.Errorf("response_timeout: initialize: %w", err)
	}

	// 2. initialized notification
	if err := sess.send(map[string]any{
		"method": "initialized",
		"params": map[string]any{},
	}); err != nil {
		sess.Close()
		return nil, fmt.Errorf("codex: send initialized: %w", err)
	}

	// 3. thread/start
	threadParams := map[string]any{
		"approvalPolicy": c.cfg.Codex.ApprovalPolicy,
		"sandbox":        c.cfg.Codex.ThreadSandbox,
		"cwd":            workspace,
	}

	// Advertise dynamic tools
	if len(c.tools) > 0 {
		toolSpecs := make([]map[string]any, 0, len(c.tools))
		for _, t := range c.tools {
			toolSpecs = append(toolSpecs, t.Spec())
		}
		threadParams["dynamicTools"] = toolSpecs
	}

	if err := sess.send(map[string]any{
		"id":     threadStartID,
		"method": "thread/start",
		"params": threadParams,
	}); err != nil {
		sess.Close()
		return nil, fmt.Errorf("codex: send thread/start: %w", err)
	}

	threadResult, err := sess.awaitResponse(threadStartID, readTimeout)
	if err != nil {
		sess.Close()
		return nil, fmt.Errorf("response_timeout: thread/start: %w", err)
	}

	threadID, err := extractThreadID(threadResult)
	if err != nil {
		sess.Close()
		return nil, err
	}
	sess.threadID = threadID

	return sess, nil
}

// RunTurn starts a turn on an existing session and streams events until the
// turn completes, fails, or times out.
func (c *Client) RunTurn(ctx context.Context, sess *Session, issue domain.Issue, prompt string, onUpdate func(domain.AgentUpdate)) (*TurnResult, error) {
	approvalPolicy := c.cfg.Codex.ApprovalPolicy
	autoApprove := isAutoApprove(approvalPolicy)

	turnSandboxPolicy := c.cfg.Codex.TurnSandboxPolicy
	if turnSandboxPolicy == nil {
		turnSandboxPolicy = map[string]any{
			"type":            "workspaceWrite",
			"writableRoots":   []string{sess.workspace},
			"readOnlyAccess":  map[string]any{"type": "fullAccess"},
			"networkAccess":   false,
		}
	}

	// Send turn/start
	if err := sess.send(map[string]any{
		"id":     turnStartID,
		"method": "turn/start",
		"params": map[string]any{
			"threadId": sess.threadID,
			"input": []map[string]any{
				{"type": "text", "text": prompt},
			},
			"cwd":            sess.workspace,
			"title":          fmt.Sprintf("%s: %s", issue.Identifier, issue.Title),
			"approvalPolicy": approvalPolicy,
			"sandboxPolicy":  turnSandboxPolicy,
		},
	}); err != nil {
		return nil, fmt.Errorf("codex: send turn/start: %w", err)
	}

	readTimeout := time.Duration(c.cfg.Codex.ReadTimeoutMS) * time.Millisecond
	turnResult, err := sess.awaitResponse(turnStartID, readTimeout)
	if err != nil {
		return nil, fmt.Errorf("response_timeout: turn/start: %w", err)
	}

	turnID := extractTurnID(turnResult)
	sessionID := sess.threadID + "-" + turnID

	if onUpdate != nil {
		onUpdate(domain.AgentUpdate{
			Event:     "session_started",
			Timestamp: time.Now().UTC(),
			SessionID: sessionID,
		})
	}

	// Stream turn events
	turnTimeout := time.Duration(c.cfg.Codex.TurnTimeoutMS) * time.Millisecond
	result, err := c.streamTurn(ctx, sess, sessionID, turnTimeout, autoApprove, onUpdate)
	if err != nil {
		return nil, err
	}
	result.SessionID = sessionID
	result.ThreadID = sess.threadID
	result.TurnID = turnID
	return result, nil
}

// StopSession terminates the app-server subprocess.
func (c *Client) StopSession(sess *Session) error {
	if sess == nil {
		return nil
	}
	return sess.Close()
}

// --- Session methods ---

func (s *Session) send(msg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.stdin.Write(data)
	return err
}

func (s *Session) awaitResponse(requestID int, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("response_timeout")
		}

		line, err := s.readLineWithDeadline(deadline)
		if err != nil {
			return nil, err
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Non-JSON line (stderr leak or diagnostic), skip
			continue
		}

		// Check if this is our response
		id, _ := msg["id"]
		switch v := id.(type) {
		case float64:
			if int(v) == requestID {
				if errField, ok := msg["error"]; ok {
					return nil, fmt.Errorf("response_error: %v", errField)
				}
				result, _ := msg["result"].(map[string]any)
				return result, nil
			}
		}

		// Not our response, skip (could be a notification)
	}
}

func (s *Session) readLineWithDeadline(deadline time.Time) (string, error) {
	// Use a channel to implement timeout on scanner
	type scanResult struct {
		line string
		err  error
	}
	ch := make(chan scanResult, 1)
	go func() {
		if s.stdout.Scan() {
			ch <- scanResult{line: s.stdout.Text()}
		} else {
			ch <- scanResult{err: fmt.Errorf("port_exit: %v", s.stdout.Err())}
		}
	}()

	remaining := time.Until(deadline)
	if remaining <= 0 {
		return "", fmt.Errorf("response_timeout")
	}

	select {
	case r := <-ch:
		return r.line, r.err
	case <-time.After(remaining):
		return "", fmt.Errorf("response_timeout")
	}
}

// Close terminates the subprocess.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.stdin.Close()
	if s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.cmd.Wait()
	return nil
}

// --- Turn streaming ---

func (c *Client) streamTurn(ctx context.Context, sess *Session, sessionID string, turnTimeout time.Duration, autoApprove bool, onUpdate func(domain.AgentUpdate)) (*TurnResult, error) {
	deadline := time.Now().Add(turnTimeout)

	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return &TurnResult{Status: "timeout"}, nil
		}

		line, err := sess.readLineWithDeadline(deadline)
		if err != nil {
			return &TurnResult{Status: "exit", Details: map[string]any{"error": err.Error()}}, nil
		}

		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			// Non-JSON: stderr diagnostic or malformed
			if strings.TrimSpace(line) != "" && strings.HasPrefix(strings.TrimSpace(line), "{") {
				c.emitUpdate(onUpdate, "malformed", sessionID, nil)
			}
			continue
		}

		method, _ := msg["method"].(string)

		switch method {
		case "turn/completed":
			c.emitUpdate(onUpdate, "turn_completed", sessionID, extractUsage(msg))
			return &TurnResult{Status: "completed"}, nil

		case "turn/failed":
			params, _ := msg["params"].(map[string]any)
			c.emitUpdate(onUpdate, "turn_failed", sessionID, extractUsage(msg))
			return &TurnResult{Status: "failed", Details: params}, nil

		case "turn/cancelled":
			params, _ := msg["params"].(map[string]any)
			c.emitUpdate(onUpdate, "turn_cancelled", sessionID, extractUsage(msg))
			return &TurnResult{Status: "cancelled", Details: params}, nil

		case "item/commandExecution/requestApproval",
			"execCommandApproval",
			"item/fileChange/requestApproval",
			"applyPatchApproval":
			if autoApprove {
				id := msg["id"]
				decision := autoApproveDecision(method)
				_ = sess.send(map[string]any{
					"id":     id,
					"result": map[string]any{"decision": decision},
				})
				c.emitUpdate(onUpdate, "approval_auto_approved", sessionID, nil)
			} else {
				return &TurnResult{Status: "failed", Details: map[string]any{"reason": "approval_required"}}, nil
			}

		case "item/tool/call":
			// Dispatch to registered tool or return unsupported
			params, _ := msg["params"].(map[string]any)
			toolName := toolCallName(params)
			id := msg["id"]

			if t, ok := c.tools[toolName]; ok {
				arguments := toolCallArguments(params)
				result := t.Execute(ctx, arguments)
				_ = sess.send(map[string]any{
					"id": id,
					"result": map[string]any{
						"success":      result.Success,
						"output":       result.Output,
						"contentItems": result.ContentItems,
					},
				})
				if result.Success {
					c.emitUpdate(onUpdate, "tool_call_completed", sessionID, nil)
				} else {
					c.emitUpdate(onUpdate, "tool_call_failed", sessionID, nil)
				}
			} else {
				_ = sess.send(map[string]any{
					"id": id,
					"result": map[string]any{
						"success": false,
						"error":   "unsupported_tool_call",
						"output":  "This tool is not supported in this session.",
						"contentItems": []map[string]any{
							{"type": "inputText", "text": "This tool is not supported in this session."},
						},
					},
				})
				c.emitUpdate(onUpdate, "unsupported_tool_call", sessionID, nil)
			}

		case "item/tool/requestUserInput":
			// High-trust: fail the run on user input request
			c.emitUpdate(onUpdate, "turn_input_required", sessionID, nil)
			return &TurnResult{Status: "input_required"}, nil

		default:
			// Check if this is an input-required method
			if isInputRequired(method, msg) {
				c.emitUpdate(onUpdate, "turn_input_required", sessionID, nil)
				return &TurnResult{Status: "input_required"}, nil
			}

			// Notification: emit and continue
			c.emitUpdate(onUpdate, "notification", sessionID, extractUsage(msg))
		}
	}
}

func (c *Client) drainStderr(sess *Session) {
	scanner := bufio.NewScanner(sess.stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			c.logger.Debug("codex stderr", "line", line)
		}
	}
}

func (c *Client) emitUpdate(onUpdate func(domain.AgentUpdate), event, sessionID string, usage *domain.TokenUsage) {
	if onUpdate == nil {
		return
	}
	onUpdate(domain.AgentUpdate{
		Event:     event,
		Timestamp: time.Now().UTC(),
		SessionID: sessionID,
		Usage:     usage,
	})
}

// --- helpers ---

func extractThreadID(result map[string]any) (string, error) {
	thread, ok := result["thread"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid thread/start response: no thread field")
	}
	id, ok := thread["id"].(string)
	if !ok {
		return "", fmt.Errorf("invalid thread/start response: no thread.id")
	}
	return id, nil
}

func extractTurnID(result map[string]any) string {
	turn, ok := result["turn"].(map[string]any)
	if !ok {
		return "unknown"
	}
	id, _ := turn["id"].(string)
	if id == "" {
		return "unknown"
	}
	return id
}

func extractUsage(msg map[string]any) *domain.TokenUsage {
	usage, ok := msg["usage"].(map[string]any)
	if !ok {
		return nil
	}
	return &domain.TokenUsage{
		InputTokens:  intFromAny(usage["input_tokens"]),
		OutputTokens: intFromAny(usage["output_tokens"]),
		TotalTokens:  intFromAny(usage["total_tokens"]),
	}
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func isAutoApprove(policy any) bool {
	s, ok := policy.(string)
	return ok && s == "never"
}

func autoApproveDecision(method string) string {
	switch method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		return "acceptForSession"
	default:
		return "approved_for_session"
	}
}

func toolCallName(params map[string]any) string {
	if params == nil {
		return ""
	}
	for _, key := range []string{"tool", "name"} {
		if v, ok := params[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func toolCallArguments(params map[string]any) any {
	if params == nil {
		return map[string]any{}
	}
	if v, ok := params["arguments"]; ok {
		return v
	}
	return map[string]any{}
}

func isInputRequired(method string, msg map[string]any) bool {
	if !strings.HasPrefix(method, "turn/") {
		return false
	}
	inputMethods := map[string]bool{
		"turn/input_required":   true,
		"turn/needs_input":      true,
		"turn/need_input":       true,
		"turn/request_input":    true,
		"turn/request_response": true,
		"turn/provide_input":    true,
		"turn/approval_required": true,
	}
	if inputMethods[method] {
		return true
	}
	// Check payload fields
	if checkInputField(msg) {
		return true
	}
	params, _ := msg["params"].(map[string]any)
	return checkInputField(params)
}

func checkInputField(m map[string]any) bool {
	if m == nil {
		return false
	}
	if m["requiresInput"] == true || m["needsInput"] == true ||
		m["input_required"] == true || m["inputRequired"] == true {
		return true
	}
	t, _ := m["type"].(string)
	return t == "input_required" || t == "needs_input"
}
