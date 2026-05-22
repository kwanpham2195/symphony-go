// Package server implements the optional HTTP dashboard and JSON status API.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/kwanpham2195/symphony-go/internal/domain"
)

// SnapshotProvider returns a point-in-time orchestrator snapshot.
type SnapshotProvider interface {
	Snapshot() domain.Snapshot
}

// RefreshRequester triggers an immediate poll cycle.
type RefreshRequester interface {
	Tick(ctx context.Context)
}

// Options configures the HTTP server.
type Options struct {
	Port int
	Host string
}

// Server is the HTTP dashboard and API server.
type Server struct {
	snap    SnapshotProvider
	refresh RefreshRequester
	opts    Options
	logger  *slog.Logger
	mux     *http.ServeMux
}

// New creates a server.
func New(snap SnapshotProvider, refresh RefreshRequester, opts Options, logger *slog.Logger) *Server {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if logger == nil {
		logger = slog.Default()
	}

	s := &Server{
		snap:    snap,
		refresh: refresh,
		opts:    opts,
		logger:  logger,
		mux:     http.NewServeMux(),
	}

	s.mux.HandleFunc("/", s.handleDashboard)
	s.mux.HandleFunc("/api/v1/state", s.handleState)
	s.mux.HandleFunc("/api/v1/refresh", s.handleRefresh)
	// Issue-specific endpoint uses a prefix match
	s.mux.HandleFunc("/api/v1/issues/", s.handleIssue)

	return s
}

// ListenAndServe starts the HTTP server and blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	addr := fmt.Sprintf("%s:%d", s.opts.Host, s.opts.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("server listen: %w", err)
	}

	// If port was 0, log the actual bound port
	actualAddr := listener.Addr().String()
	s.logger.Info("http server started", "addr", actualAddr)

	srv := &http.Server{
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	case err := <-errCh:
		return err
	}
}

// Handler returns the mux for testing.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// --- Handlers ---

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	snap := s.snap.Snapshot()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Symphony Dashboard</title>
<meta http-equiv="refresh" content="5">
<style>
body { font-family: monospace; background: #1a1a2e; color: #e0e0e0; padding: 20px; }
h1 { color: #0f3460; }
table { border-collapse: collapse; margin: 10px 0; }
th, td { border: 1px solid #333; padding: 6px 12px; text-align: left; }
th { background: #16213e; }
.section { margin: 20px 0; }
.metric { display: inline-block; margin: 0 20px; }
.metric-value { font-size: 1.4em; font-weight: bold; color: #e94560; }
</style></head><body>
<h1>Symphony Dashboard</h1>
<div class="section">
<div class="metric"><div>Running</div><div class="metric-value">%d</div></div>
<div class="metric"><div>Retrying</div><div class="metric-value">%d</div></div>
<div class="metric"><div>Total Tokens</div><div class="metric-value">%d</div></div>
<div class="metric"><div>Runtime</div><div class="metric-value">%.0fs</div></div>
</div>
`, len(snap.Running), len(snap.Retrying), snap.CodexTotals.TotalTokens, snap.CodexTotals.SecondsRunning)

	if len(snap.Running) > 0 {
		fmt.Fprintf(w, `<div class="section"><h2>Running Sessions</h2>
<table><tr><th>Issue</th><th>Session</th><th>Turns</th><th>Started</th></tr>
`)
		for _, row := range snap.Running {
			fmt.Fprintf(w, "<tr><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>\n",
				row.IssueIdentifier, row.SessionID, row.TurnCount,
				row.StartedAt.Format("15:04:05"))
		}
		fmt.Fprintf(w, "</table></div>\n")
	}

	if len(snap.Retrying) > 0 {
		fmt.Fprintf(w, `<div class="section"><h2>Retry Queue</h2>
<table><tr><th>Issue</th><th>Attempt</th><th>Due</th><th>Error</th></tr>
`)
		for _, row := range snap.Retrying {
			fmt.Fprintf(w, "<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td></tr>\n",
				row.Identifier, row.Attempt,
				row.DueAt.Format("15:04:05"), row.Error)
		}
		fmt.Fprintf(w, "</table></div>\n")
	}

	fmt.Fprintf(w, "</body></html>")
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	snap := s.snap.Snapshot()
	writeJSON(w, http.StatusOK, snap)
}

func (s *Server) handleIssue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}

	// Extract identifier from /api/v1/issues/{identifier}
	prefix := "/api/v1/issues/"
	identifier := strings.TrimPrefix(r.URL.Path, prefix)
	if identifier == "" {
		http.NotFound(w, r)
		return
	}

	snap := s.snap.Snapshot()

	// Search running
	for _, row := range snap.Running {
		if row.IssueIdentifier == identifier {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":  "running",
				"running": row,
			})
			return
		}
	}

	// Search retrying
	for _, row := range snap.Retrying {
		if row.Identifier == identifier {
			writeJSON(w, http.StatusOK, map[string]any{
				"status":   "retrying",
				"retrying": row,
			})
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{
		"error": "issue not found",
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}

	go s.refresh.Tick(context.Background())

	writeJSON(w, http.StatusOK, map[string]any{
		"queued":       true,
		"requested_at": time.Now().UTC(),
		"operations":   []string{"poll", "reconcile"},
	})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}

func methodNotAllowed(w http.ResponseWriter) {
	w.Header().Set("Allow", "GET")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
