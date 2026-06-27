package engine

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Watcher polls a rules file and hot-reloads the engine whenever the file's
// modification time changes. Reloads are atomic (ReplaceRules), so in-flight
// matching keeps using the previous rule set until the swap completes.
type Watcher struct {
	eng      *Engine
	path     string
	interval time.Duration
	last     time.Time
	onReload func(loaded, failed int, err error)
}

// NewWatcher creates a watcher for path, polling every interval.
func NewWatcher(eng *Engine, path string, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Watcher{
		eng:      eng,
		path:     path,
		interval: interval,
		onReload: func(loaded, failed int, err error) {
			if err != nil {
				fmt.Printf("hot-reload error: %v\n", err)
				return
			}
			fmt.Printf("hot-reload: %d rules active (failed=%d)\n", loaded, failed)
		},
	}
}

// OnReload sets a callback invoked after each reload attempt.
func (w *Watcher) OnReload(fn func(loaded, failed int, err error)) { w.onReload = fn }

// Run blocks, polling until ctx is cancelled. Typically run in a goroutine.
func (w *Watcher) Run(ctx context.Context) error {
	// Record the current mtime so we only reload on subsequent changes.
	if fi, err := os.Stat(w.path); err == nil {
		w.last = fi.ModTime()
	}
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			w.checkOnce()
		}
	}
}

func (w *Watcher) checkOnce() {
	fi, err := os.Stat(w.path)
	if err != nil {
		return // file temporarily missing during write; try again next tick
	}
	if !fi.ModTime().After(w.last) {
		return
	}
	w.last = fi.ModTime()
	loaded, failed, err := w.eng.ReloadFromFile(w.path)
	if w.onReload != nil {
		w.onReload(loaded, failed, err)
	}
}
