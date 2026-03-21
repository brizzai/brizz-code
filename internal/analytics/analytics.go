package analytics

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/amplitude/analytics-go/amplitude"
	"github.com/brizzai/brizz-code/internal/debuglog"
)

const amplitudeAPIKey = "399db841c10e7de722315b1d16c6b609"

var (
	global   *Client
	globalMu sync.Mutex
)

// Client wraps the Amplitude SDK with anonymized device tracking.
type Client struct {
	amp      amplitude.Client
	deviceID string
	disabled bool
}

// Init creates the global analytics client.
// If telemetry is disabled (config or env), returns a no-op client.
func Init(telemetryEnabled bool) {
	globalMu.Lock()
	defer globalMu.Unlock()

	if global != nil {
		return
	}

	disabled := !telemetryEnabled || isOptedOut()
	if disabled {
		global = &Client{disabled: true}
		debuglog.Logger.Info("analytics disabled")
		return
	}

	deviceID := getOrCreateDeviceID()

	cfg := amplitude.NewConfig(amplitudeAPIKey)
	cfg.FlushQueueSize = 20
	cfg.Logger = silentLogger{}
	amp := amplitude.NewClient(cfg)

	global = &Client{
		amp:      amp,
		deviceID: deviceID,
	}

	debuglog.Logger.Info("analytics initialized", "device_id", deviceID[:8]+"...")
}

// Track sends an event with optional properties.
func Track(eventType string, properties map[string]interface{}) {
	globalMu.Lock()
	c := global
	globalMu.Unlock()

	if c == nil || c.disabled {
		return
	}

	event := amplitude.Event{
		EventType:       eventType,
		DeviceID:        c.deviceID,
		EventProperties: properties,
		EventOptions: amplitude.EventOptions{
			Platform: "macOS",
			OSName:   "macOS",
		},
	}

	c.amp.Track(event)
}

// SetUserProperties sends an identify event to set user properties.
func SetUserProperties(props map[string]interface{}) {
	globalMu.Lock()
	c := global
	globalMu.Unlock()

	if c == nil || c.disabled {
		return
	}

	identify := amplitude.Identify{}
	for k, v := range props {
		identify.Set(k, v)
	}

	c.amp.Identify(identify, amplitude.EventOptions{
		DeviceID: c.deviceID,
	})
}

// Shutdown flushes pending events and shuts down the client.
func Shutdown() {
	globalMu.Lock()
	c := global
	global = nil
	globalMu.Unlock()

	if c == nil || c.disabled {
		return
	}

	c.amp.Shutdown()
	debuglog.Logger.Info("analytics shutdown")
}

// isOptedOut checks environment variables for telemetry opt-out.
func isOptedOut() bool {
	if os.Getenv("BRIZZ_TELEMETRY_DISABLED") == "1" {
		return true
	}
	if os.Getenv("DO_NOT_TRACK") == "1" {
		return true
	}
	return false
}

// getOrCreateDeviceID returns a stable anonymous device ID.
// Cached in ~/.config/brizz-code/device_id after first generation.
func getOrCreateDeviceID() string {
	home, _ := os.UserHomeDir()
	idPath := filepath.Join(home, ".config", "brizz-code", "device_id")

	// Try reading cached ID.
	if data, err := os.ReadFile(idPath); err == nil {
		id := strings.TrimSpace(string(data))
		if len(id) > 0 {
			return id
		}
	}

	// Generate from hardware UUID.
	id := generateDeviceID()

	// Cache for next time.
	_ = os.MkdirAll(filepath.Dir(idPath), 0700)
	_ = os.WriteFile(idPath, []byte(id), 0600)

	return id
}

// generateDeviceID creates a SHA256 hash of the macOS hardware UUID.
func generateDeviceID() string {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		// Fallback: use hostname + arch as seed.
		hostname, _ := os.Hostname()
		seed := hostname + runtime.GOARCH
		h := sha256.Sum256([]byte(seed))
		return fmt.Sprintf("%x", h)
	}

	// Extract IOPlatformUUID from ioreg output.
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid := strings.TrimSpace(parts[1])
				uuid = strings.Trim(uuid, "\"")
				h := sha256.Sum256([]byte(uuid))
				return fmt.Sprintf("%x", h)
			}
		}
	}

	// Fallback.
	hostname, _ := os.Hostname()
	h := sha256.Sum256([]byte(hostname))
	return fmt.Sprintf("%x", h)
}

// osVersion returns the macOS version string.
func osVersion() string {
	out, err := exec.Command("sw_vers", "-productVersion").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

// TrackAppStarted tracks app launch with user properties.
func TrackAppStarted(version string, sessionCount, repoCount int, theme, enterMode string, autoName, copyClaudeSettings bool) {
	SetUserProperties(map[string]interface{}{
		"app_version":         version,
		"os_version":          osVersion(),
		"arch":                runtime.GOARCH,
		"theme":               theme,
		"enter_mode":          enterMode,
		"auto_name_sessions":  autoName,
		"copy_claude_settings": copyClaudeSettings,
	})

	Track(EventAppStarted, map[string]interface{}{
		"version":       version,
		"session_count": sessionCount,
		"repo_count":    repoCount,
	})
}

// silentLogger suppresses all Amplitude SDK output to avoid corrupting the TUI.
type silentLogger struct{}

func (silentLogger) Debugf(string, ...interface{}) {}
func (silentLogger) Infof(string, ...interface{})  {}
func (silentLogger) Warnf(string, ...interface{})  {}
func (silentLogger) Errorf(string, ...interface{}) {}
