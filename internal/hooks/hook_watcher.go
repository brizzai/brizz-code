package hooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yuvalhayke/brizz-code/internal/debuglog"
)

const hookDebounce = 100 * time.Millisecond

// HookStatus holds the decoded status from a hook status file.
type HookStatus struct {
	Status      string
	SessionID   string
	Event       string
	UpdatedAt   time.Time
	UserPrompt  string
	PromptCount int
}

// HookWatcher watches ~/.config/brizz-code/hooks/ for status file changes
// and maintains a thread-safe in-memory status map.
type HookWatcher struct {
	hooksDir string
	watcher  *fsnotify.Watcher

	mu       sync.RWMutex
	statuses map[string]*HookStatus // brizz session ID -> latest status

	ctx    context.Context
	cancel context.CancelFunc
}

// GetHooksDir returns the path to the hooks status directory.
func GetHooksDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), ".config", "brizz-code", "hooks")
	}
	return filepath.Join(home, ".config", "brizz-code", "hooks")
}

// NewHookWatcher creates a new watcher for the hooks directory.
func NewHookWatcher() (*HookWatcher, error) {
	hooksDir := GetHooksDir()

	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		debuglog.Logger.Error("hook watcher: failed to create hooks dir", "dir", hooksDir, "err", err)
		return nil, err
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		debuglog.Logger.Error("hook watcher: fsnotify watcher creation failed", "err", err)
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	debuglog.Logger.Info("hook watcher created", "dir", hooksDir)
	return &HookWatcher{
		hooksDir: hooksDir,
		watcher:  watcher,
		statuses: make(map[string]*HookStatus),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start begins watching the hooks directory. Blocks; run in a goroutine.
func (w *HookWatcher) Start() {
	if err := w.watcher.Add(w.hooksDir); err != nil {
		debuglog.Logger.Error("hook watcher: failed to watch hooks dir", "dir", w.hooksDir, "err", err)
		return
	}

	w.loadExisting()

	var debounceTimer *time.Timer
	pendingFiles := make(map[string]bool)
	var pendingMu sync.Mutex

	for {
		select {
		case <-w.ctx.Done():
			return

		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}

			if filepath.Ext(event.Name) != ".json" {
				continue
			}
			if event.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}

			pendingMu.Lock()
			pendingFiles[event.Name] = true
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(hookDebounce, func() {
				pendingMu.Lock()
				files := make([]string, 0, len(pendingFiles))
				for f := range pendingFiles {
					files = append(files, f)
				}
				pendingFiles = make(map[string]bool)
				pendingMu.Unlock()

				for _, f := range files {
					w.processFile(f)
				}
			})
			pendingMu.Unlock()

		case watchErr, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
			debuglog.Logger.Error("hook watcher: fsnotify error", "err", watchErr)
		}
	}
}

// Stop shuts down the watcher.
func (w *HookWatcher) Stop() {
	w.cancel()
	_ = w.watcher.Close()
}

// GetStatus returns the hook status for a session, or nil if not available.
func (w *HookWatcher) GetStatus(sessionID string) *HookStatus {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.statuses[sessionID]
}

// loadExisting reads all current status files on startup.
func (w *HookWatcher) loadExisting() {
	entries, err := os.ReadDir(w.hooksDir)
	if err != nil {
		debuglog.Logger.Error("hook watcher: loadExisting ReadDir failed", "dir", w.hooksDir, "err", err)
		return
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		w.processFile(filepath.Join(w.hooksDir, entry.Name()))
		count++
	}
	debuglog.Logger.Debug("hook watcher: loaded existing status files", "count", count)
}

// processFile reads a status file and updates the internal map.
func (w *HookWatcher) processFile(filePath string) {
	sf, err := ReadStatusFile(filePath)
	if err != nil {
		debuglog.Logger.Error("hook watcher: failed to parse status file", "file", filePath, "err", err)
		return
	}

	base := filepath.Base(filePath)
	instanceID := strings.TrimSuffix(base, ".json")

	hookStatus := &HookStatus{
		Status:      sf.Status,
		SessionID:   sf.SessionID,
		Event:       sf.Event,
		UpdatedAt:   time.Unix(sf.Timestamp, 0),
		UserPrompt:  sf.UserPrompt,
		PromptCount: sf.PromptCount,
	}

	w.mu.Lock()
	w.statuses[instanceID] = hookStatus
	w.mu.Unlock()
}
