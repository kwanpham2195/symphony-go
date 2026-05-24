// Package pi implements the Pi RPC JSON line protocol client.
//
// The client launches a "pi --mode rpc --no-session" subprocess, sends prompt
// commands, and streams events from stdout until agent_end.
package pi

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
	"sync/atomic"
	"time"

	"github.com/kwanpham2195/symphony-go/internal"
	"github.com/kwanpham2195/symphony-go/internal/config"
)

const (
	maxLineBytes    = 10 * 1024 * 1024 // 10 MB
	shutdownTimeout = 5 * time.Second
)

// TurnResult describes how a prompt execution ended.
type TurnResult struct {
	Status  string // "completed", "failed", "timeout", "exit"
	Details map[string]any
}

// Session holds a running Pi subprocess.
type Session struct {
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	stdout    *bufio.Scanner
	stderr    io.ReadCloser
	workspace string
	pid       int

	mu     sync.Mutex
	closed bool
}

// Client manages Pi RPC sessions.
type Client struct {
	cfg    atomic.Pointer[config.Config]
	logger *slog.Logger
	reqSeq int64 // atomic request ID counter
}

// NewClient creates a Pi client.
func NewClient(cfg *config.Config, logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	c := &Client{logger: logger}
	c.cfg.Store(cfg)
	return c
}

// UpdateConfig replaces the config (for dynamic reload).
func (c *Client) UpdateConfig(cfg *config.Config) {
	c.cfg.Store(cfg)
}

func (c *Client) loadConfig() *config.Config {
	return c.cfg.Load()
}

// StartSession launches the Pi subprocess. No handshake needed — the process
// is ready to receive commands immediately.
func (c *Client) StartSession(ctx context.Context, workspace string) (*Session, error) {
	cfg := c.loadConfig()

	cmd := exec.CommandContext(ctx, "bash", "-lc", cfg.Pi.Command)
	cmd.Dir = workspace

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("pi: stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pi: stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("pi: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pi_not_found: %w", err)
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

	go c.drainStderr(sess)

	return sess, nil
}

// SendPrompt sends a prompt to Pi and streams events until agent_end.
func (c *Client) SendPrompt(ctx context.Context, sess *Session, prompt string, onUpdate func(internal.AgentUpdate)) (*TurnResult, error) {
	cfg := c.loadConfig()

	reqID := fmt.Sprintf("req-%d", atomic.AddInt64(&c.reqSeq, 1))

	// Send prompt command
	if err := sess.send(map[string]any{
		"type":    "prompt",
		"message": prompt,
		"id":      reqID,
	}); err != nil {
		return nil, fmt.Errorf("pi: send prompt: %w", err)
	}

	// Await prompt response
	readTimeout := time.Duration(cfg.Pi.ReadTimeoutMS) * time.Millisecond
	resp, err := sess.awaitResponse(reqID, readTimeout)
	if err != nil {
		return nil, fmt.Errorf("pi: prompt response: %w", err)
	}

	if success, ok := resp["success"].(bool); !ok || !success {
		errMsg, _ := resp["error"].(string)
		return nil, fmt.Errorf("pi: prompt rejected: %s", errMsg)
	}

	// Generate session ID
	sessionID := fmt.Sprintf("pi-%d", time.Now().UnixMilli())

	// Stream events until agent_end
	turnTimeout := time.Duration(cfg.Pi.TurnTimeoutMS) * time.Millisecond
	return c.streamEvents(ctx, sess, sessionID, turnTimeout, onUpdate)
}

// StopSession terminates the Pi subprocess gracefully.
func (c *Client) StopSession(sess *Session) error {
	if sess == nil {
		return nil
	}

	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return nil
	}
	sess.closed = true
	sess.mu.Unlock()

	// Try graceful: send abort
	_ = sess.sendRaw(map[string]any{"type": "abort"})

	// Close stdin — Pi exits on EOF
	_ = sess.stdin.Close()

	// Wait briefly for clean exit
	done := make(chan struct{})
	go func() {
		_ = sess.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Clean exit
	case <-time.After(shutdownTimeout):
		// Force kill
		if sess.cmd.Process != nil {
			_ = sess.cmd.Process.Kill()
		}
		<-done
	}
	return nil
}

// --- Session methods ---

func (s *Session) send(msg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("session closed")
	}
	return s.sendLocked(msg)
}

func (s *Session) sendRaw(msg map[string]any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sendLocked(msg)
}

func (s *Session) sendLocked(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = s.stdin.Write(data)
	return err
}

func (s *Session) awaitResponse(requestID string, timeout time.Duration) (map[string]any, error) {
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
			continue
		}

		msgType, _ := msg["type"].(string)

		// Skip non-response events (extension_ui_request, etc.)
		if msgType != "response" {
			continue
		}

		// Check if this is our response
		if id, ok := msg["id"].(string); ok && id == requestID {
			return msg, nil
		}
	}
}

func (s *Session) readLineWithDeadline(deadline time.Time) (string, error) {
	type scanResult struct {
		line string
		err  error
	}
	ch := make(chan scanResult, 1)
	go func() {
		if s.stdout.Scan() {
			ch <- scanResult{line: s.stdout.Text()}
		} else {
			ch <- scanResult{err: fmt.Errorf("port_exit: %w", s.stdout.Err())}
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

// --- Event streaming ---

func (c *Client) streamEvents(ctx context.Context, sess *Session, sessionID string, turnTimeout time.Duration, onUpdate func(internal.AgentUpdate)) (*TurnResult, error) {
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
			continue
		}

		eventType, _ := msg["type"].(string)

		switch eventType {
		case "agent_start":
			c.emitUpdate(onUpdate, "session_started", sessionID, nil)

		case "agent_end":
			c.emitUpdate(onUpdate, "turn_completed", sessionID, nil)
			return &TurnResult{Status: "completed"}, nil

		case "turn_end":
			usage := extractUsage(msg)
			c.emitUpdate(onUpdate, "notification", sessionID, usage)

		case "turn_start":
			c.emitUpdate(onUpdate, "notification", sessionID, nil)

		case "tool_execution_start":
			c.emitUpdate(onUpdate, "notification", sessionID, nil)

		case "tool_execution_end":
			c.emitUpdate(onUpdate, "tool_call_completed", sessionID, nil)

		case "compaction_start":
			c.emitUpdate(onUpdate, "compaction_started", sessionID, nil)

		case "compaction_end":
			c.emitUpdate(onUpdate, "compaction_ended", sessionID, nil)

		case "auto_retry_start":
			c.emitUpdate(onUpdate, "auto_retry_started", sessionID, nil)

		case "auto_retry_end":
			c.emitUpdate(onUpdate, "auto_retry_ended", sessionID, nil)

		case "extension_ui_request":
			method, _ := msg["method"].(string)
			id, _ := msg["id"].(string)
			// Dialog methods block Pi until a response arrives on stdin.
			// Auto-cancel so the headless run continues.
			switch method {
			case "select", "input", "editor":
				_ = sess.send(map[string]any{
					"type":      "extension_ui_response",
					"id":        id,
					"cancelled": true,
				})
				c.logger.Debug("pi extension_ui_request auto-cancelled",
					"method", method, "id", id,
				)
			case "confirm":
				_ = sess.send(map[string]any{
					"type":      "extension_ui_response",
					"id":        id,
					"confirmed": false,
				})
				c.logger.Debug("pi extension_ui_request auto-declined",
					"method", method, "id", id,
				)
			default:
				// Fire-and-forget methods (setStatus, setWidget, etc.):
				// no response needed.
				c.logger.Debug("pi extension_ui_request (fire-and-forget)",
					"method", method,
				)
			}

		case "extension_error":
			c.logger.Warn("pi extension error",
				"extension", msg["extensionPath"],
				"error", msg["error"],
			)
			c.emitUpdate(onUpdate, "notification", sessionID, nil)

		case "response":
			// Late response (e.g., for a previous command). Skip.

		default:
			// message_start, message_end, message_update, queue_update, etc.
			// Too noisy or not relevant. Skip.
		}
	}
}

func (c *Client) drainStderr(sess *Session) {
	scanner := bufio.NewScanner(sess.stderr)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			c.logger.Debug("pi stderr", "line", line)
		}
	}
}

func (c *Client) emitUpdate(onUpdate func(internal.AgentUpdate), event, sessionID string, usage *internal.TokenUsage) {
	if onUpdate == nil {
		return
	}
	onUpdate(internal.AgentUpdate{
		Event:     event,
		Timestamp: time.Now().UTC(),
		SessionID: sessionID,
		Usage:     usage,
	})
}

// --- helpers ---

func extractUsage(msg map[string]any) *internal.TokenUsage {
	message, ok := msg["message"].(map[string]any)
	if !ok {
		return nil
	}
	usage, ok := message["usage"].(map[string]any)
	if !ok {
		return nil
	}

	input := intFromAny(usage["input"])
	output := intFromAny(usage["output"])

	return &internal.TokenUsage{
		InputTokens:  input,
		OutputTokens: output,
		TotalTokens:  input + output,
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
