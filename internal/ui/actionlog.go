package ui

import (
	"sync"
	"time"
)

// ActionEntry records a single user action for "steps to reproduce".
type ActionEntry struct {
	Timestamp time.Time
	Action    string // e.g. "open editor", "attach session"
	Detail    string // e.g. session title, error message
	Success   bool
}

// ActionLog is a thread-safe ring buffer of recent user actions.
type ActionLog struct {
	mu      sync.Mutex
	entries []ActionEntry
	maxSize int
}

// NewActionLog creates an ActionLog with the given capacity.
func NewActionLog(maxSize int) *ActionLog {
	return &ActionLog{
		entries: make([]ActionEntry, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add records a new action.
func (l *ActionLog) Add(action, detail string, success bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry := ActionEntry{
		Timestamp: time.Now(),
		Action:    action,
		Detail:    detail,
		Success:   success,
	}
	if len(l.entries) >= l.maxSize {
		// Shift left to drop oldest.
		copy(l.entries, l.entries[1:])
		l.entries[len(l.entries)-1] = entry
	} else {
		l.entries = append(l.entries, entry)
	}
}

// Entries returns all entries, newest first.
func (l *ActionLog) Entries() []ActionEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]ActionEntry, len(l.entries))
	for i, e := range l.entries {
		result[len(l.entries)-1-i] = e
	}
	return result
}
