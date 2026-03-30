package hooks

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestHookWatcherChangesNotifiesOnProcessFile(t *testing.T) {
	dir := t.TempDir()

	// Write a status file before creating the watcher.
	sf := &StatusFile{
		Status:    "running",
		SessionID: "sess-123",
		Event:     "UserPromptSubmit",
		Timestamp: time.Now().Unix(),
	}
	if err := WriteStatusFile(dir, "test-instance", sf); err != nil {
		t.Fatalf("WriteStatusFile: %v", err)
	}

	w, err := newHookWatcherWithDir(dir)
	if err != nil {
		t.Fatalf("newHookWatcherWithDir: %v", err)
	}
	defer w.Stop()

	// processFile should send a notification on Changes().
	w.processFile(dir + "/test-instance.json")

	select {
	case <-w.Changes():
		// OK — notification received.
	case <-time.After(time.Second):
		t.Fatal("expected notification on Changes() after processFile, got none")
	}

	// Verify the status was stored.
	hs := w.GetStatus("test-instance")
	if hs == nil {
		t.Fatal("expected non-nil HookStatus")
	}
	if hs.Status != "running" {
		t.Errorf("expected status 'running', got %q", hs.Status)
	}
	if hs.SessionID != "sess-123" {
		t.Errorf("expected session ID 'sess-123', got %q", hs.SessionID)
	}
}

func TestHookWatcherChangesCoalescesRapidWrites(t *testing.T) {
	dir := t.TempDir()

	w, err := newHookWatcherWithDir(dir)
	if err != nil {
		t.Fatalf("newHookWatcherWithDir: %v", err)
	}
	defer w.Stop()

	// Write multiple status files rapidly.
	for i := 0; i < 5; i++ {
		sf := &StatusFile{
			Status:    "running",
			Event:     "UserPromptSubmit",
			Timestamp: time.Now().Unix(),
		}
		id := "instance-" + string(rune('a'+i))
		if err := WriteStatusFile(dir, id, sf); err != nil {
			t.Fatalf("WriteStatusFile %d: %v", i, err)
		}
		w.processFile(dir + "/" + id + ".json")
	}

	// Should get at least one notification (buffered channel coalesces extras).
	select {
	case <-w.Changes():
		// OK.
	case <-time.After(time.Second):
		t.Fatal("expected at least one notification after rapid writes")
	}

	// Drain any remaining notification.
	select {
	case <-w.Changes():
	default:
	}

	// Channel should now be empty — no more pending.
	select {
	case <-w.Changes():
		t.Fatal("expected no more notifications after draining")
	default:
		// OK — empty as expected.
	}
}

func TestHookWatcherLoadExistingNotifies(t *testing.T) {
	dir := t.TempDir()

	// Write a status file before creating the watcher.
	sf := &StatusFile{
		Status:    "waiting",
		Event:     "PermissionRequest",
		Timestamp: time.Now().Unix(),
	}
	if err := WriteStatusFile(dir, "pre-existing", sf); err != nil {
		t.Fatalf("WriteStatusFile: %v", err)
	}

	w, err := newHookWatcherWithDir(dir)
	if err != nil {
		t.Fatalf("newHookWatcherWithDir: %v", err)
	}
	defer w.Stop()

	// loadExisting is called during construction; processFile sends notifications.
	// Drain all notifications from loadExisting's processFile calls.
	select {
	case <-w.Changes():
	case <-time.After(time.Second):
		t.Fatal("expected notification after loadExisting")
	}

	// Verify the pre-existing status was loaded.
	hs := w.GetStatus("pre-existing")
	if hs == nil {
		t.Fatal("expected non-nil HookStatus for pre-existing file")
	}
	if hs.Status != "waiting" {
		t.Errorf("expected status 'waiting', got %q", hs.Status)
	}
}

// newHookWatcherWithDir creates a HookWatcher pointing at a custom directory
// (for testing without touching the real hooks dir). It loads existing files
// but does NOT start the fsnotify event loop.
func newHookWatcherWithDir(dir string) (*HookWatcher, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())

	w := &HookWatcher{
		hooksDir: dir,
		statuses: make(map[string]*HookStatus),
		onChange: make(chan struct{}, 1),
		ctx:      ctx,
		cancel:   cancel,
		// watcher left nil — we call processFile directly in tests.
	}
	w.loadExisting()
	return w, nil
}
