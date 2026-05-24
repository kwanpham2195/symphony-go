// Package workflow provides a file watcher for dynamic WORKFLOW.md reload.
package workflow

import (
	"log/slog"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/kwanpham2195/symphony-go/internal"
)

// Watcher watches a workflow file for changes and calls a reload callback.
type Watcher struct {
	path     string
	logger   *slog.Logger
	watcher  *fsnotify.Watcher
	onReload func(*internal.Workflow)
	stopCh   chan struct{}
	once     sync.Once
}

// NewWatcher creates a watcher for the given workflow file path.
// onReload is called with the new workflow whenever the file changes.
func NewWatcher(path string, onReload func(*internal.Workflow), logger *slog.Logger) (*Watcher, error) {
	if logger == nil {
		logger = slog.Default()
	}

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := fsw.Add(path); err != nil {
		_ = fsw.Close()
		return nil, err
	}

	w := &Watcher{
		path:     path,
		logger:   logger,
		watcher:  fsw,
		onReload: onReload,
		stopCh:   make(chan struct{}),
	}

	go w.loop()
	return w, nil
}

func (w *Watcher) loop() {
	for {
		select {
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				w.reload()
			}

		case err, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("workflow watcher error", "error", err)

		case <-w.stopCh:
			return
		}
	}
}

func (w *Watcher) reload() {
	wf, err := Load(w.path)
	if err != nil {
		w.logger.Error("workflow reload failed; keeping last good config",
			"path", w.path,
			"error", err,
		)
		return
	}

	w.logger.Info("workflow reloaded", "path", w.path)
	w.onReload(wf)
}

// Close stops the watcher.
func (w *Watcher) Close() {
	w.once.Do(func() {
		close(w.stopCh)
		_ = w.watcher.Close()
	})
}
