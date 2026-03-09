package chrome

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yuvalhayke/brizz-code/internal/debuglog"
)

const nmhName = "com.brizzcode.tabcontrol"

// nmhManifest is the Native Messaging Host manifest format.
type nmhManifest struct {
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	Path           string   `json:"path"`
	Type           string   `json:"type"`
	AllowedOrigins []string `json:"allowed_origins"`
}

// NMHManifestPath returns the path where the NMH manifest should be installed.
func NMHManifestPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "Application Support", "Google", "Chrome", "NativeMessagingHosts", nmhName+".json")
}

// InstallNativeMessagingHost writes the NMH manifest JSON so Chrome can find the host.
// Uses os.Executable() + EvalSymlinks for a stable binary path (same pattern as hooks.GetHookCommand).
// Returns true if the manifest was written or updated.
func InstallNativeMessagingHost() bool {
	log := debuglog.Logger

	exe, err := os.Executable()
	if err != nil {
		log.Warn("chrome: cannot resolve executable path", "err", err)
		return false
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}

	// The native host command is the binary itself with "chrome-host" subcommand.
	hostPath := resolved

	manifestPath := NMHManifestPath()

	// Check if manifest already exists with the correct path.
	if existing, err := os.ReadFile(manifestPath); err == nil {
		var m nmhManifest
		if json.Unmarshal(existing, &m) == nil && m.Path == hostPath {
			return false // Already up to date.
		}
	}

	manifest := nmhManifest{
		Name:        nmhName,
		Description: "brizz-code Chrome tab control",
		Path:        hostPath,
		Type:        "stdio",
		AllowedOrigins: []string{
			// Extension ID derived from the key in chrome-extension/manifest.json.
			"chrome-extension://haphpcoecelhofejcklinnlbfijgdnih/",
		},
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		log.Warn("chrome: cannot marshal NMH manifest", "err", err)
		return false
	}

	// Ensure directory exists.
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0755); err != nil {
		log.Warn("chrome: cannot create NMH dir", "err", err)
		return false
	}

	if err := os.WriteFile(manifestPath, data, 0644); err != nil {
		log.Warn("chrome: cannot write NMH manifest", "err", err, "path", manifestPath)
		return false
	}

	log.Info("chrome: installed NMH manifest", "path", manifestPath)
	return true
}
