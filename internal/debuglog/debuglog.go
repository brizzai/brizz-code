package debuglog

import (
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

var (
	mu      sync.Mutex
	logFile *os.File
	Logger  *slog.Logger = slog.New(slog.NewTextHandler(os.Stderr, nil)) // fallback
)

// LogPath returns the debug log file path.
func LogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "brizz-code-debug.log")
	}
	return filepath.Join(home, ".config", "brizz-code", "debug.log")
}

// Init opens the debug log file and configures the global Logger.
func Init() {
	path := LogPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	mu.Lock()
	logFile = f
	Logger = slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mu.Unlock()
}

// Close closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}
