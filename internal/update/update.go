package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	repo             = "brizzai/brizz-code"
	apiURL           = "https://api.github.com/repos/" + repo + "/releases/latest"
	checkCacheFile   = "last_update_check"
	checkIntervalSec = 3600 // 1 hour
)

type releaseInfo struct {
	TagName string         `json:"tag_name"`
	Assets  []releaseAsset `json:"assets"`
}

type releaseAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // API URL for downloading
}

// configDir returns ~/.config/brizz-code/.
func configDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "brizz-code")
}

// ShouldCheck returns true if enough time has passed since the last check.
func ShouldCheck() bool {
	path := filepath.Join(configDir(), checkCacheFile)
	data, err := os.ReadFile(path)
	if err != nil {
		return true
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return true
	}
	return time.Now().Unix()-ts >= checkIntervalSec
}

// saveCheckTime writes the current timestamp to the cache file.
func saveCheckTime() {
	path := filepath.Join(configDir(), checkCacheFile)
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte(strconv.FormatInt(time.Now().Unix(), 10)), 0600)
}

// ghToken returns a GitHub token from env or gh CLI.
func ghToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	// Try gh CLI auth token.
	out, err := exec.Command("gh", "auth", "token").Output()
	if err == nil {
		if t := strings.TrimSpace(string(out)); t != "" {
			return t
		}
	}
	return ""
}

// checkLatestRelease fetches the latest release info from GitHub.
func checkLatestRelease() (*releaseInfo, error) {
	client := &http.Client{Timeout: 3 * time.Second}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := ghToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var release releaseInfo
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}
	return &release, nil
}

// CheckLatest returns the latest release tag if it's newer than currentVersion.
// Returns "" if already up to date or on error.
func CheckLatest(currentVersion string) (string, error) {
	release, err := checkLatestRelease()
	if err != nil {
		return "", err
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(currentVersion, "v")

	if latest == current {
		return "", nil
	}

	return release.TagName, nil
}

// Update checks for a newer version and replaces the current binary.
// Returns the new version tag, or "" if already up to date.
func Update(currentVersion string) (string, error) {
	release, err := checkLatestRelease()
	if err != nil {
		return "", err
	}

	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(currentVersion, "v")
	if latest == current {
		saveCheckTime()
		return "", nil
	}

	arch := runtime.GOARCH
	archiveName := fmt.Sprintf("brizz-code_%s_darwin_%s.tar.gz", latest, arch)

	// Find the asset API URL for the archive.
	var assetURL string
	for _, a := range release.Assets {
		if a.Name == archiveName {
			assetURL = a.URL
			break
		}
	}
	if assetURL == "" {
		return "", fmt.Errorf("asset %s not found in release", archiveName)
	}

	// Download via GitHub API (works for both public and private repos).
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", assetURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if token := ghToken(); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// Extract binary from tar.gz.
	binaryData, err := extractBinary(resp.Body)
	if err != nil {
		return "", fmt.Errorf("extract failed: %w", err)
	}

	// Get current binary path.
	exePath, err := os.Executable()
	if err != nil {
		return "", err
	}
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return "", err
	}

	// Write to temp file in same directory, then atomic rename.
	dir := filepath.Dir(exePath)
	tmp, err := os.CreateTemp(dir, "brizz-code-update-*")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(binaryData); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", err
	}
	tmp.Close()

	if err := os.Chmod(tmpPath, 0755); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	if err := os.Rename(tmpPath, exePath); err != nil {
		os.Remove(tmpPath)
		return "", err
	}

	saveCheckTime()
	return release.TagName, nil
}

// extractBinary reads a tar.gz stream and returns the brizz-code binary contents.
func extractBinary(r io.Reader) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == "brizz-code" && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("brizz-code binary not found in archive")
}
