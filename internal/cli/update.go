package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// githubReleasesURL is the GitHub API endpoint for releases
	githubReleasesURL = "https://api.github.com/repos/rileyhilliard/rr/releases/latest"

	// updateCheckCacheTTL is how long to cache the update check result
	updateCheckCacheTTL = 24 * time.Hour

	// updateCheckTimeout is the max time to wait for the GitHub API
	updateCheckTimeout = 3 * time.Second
)

// githubRelease represents the relevant fields from GitHub's release API
type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// updateCache stores cached update check results
type updateCache struct {
	LatestVersion string    `json:"latest_version"`
	CheckedAt     time.Time `json:"checked_at"`
}

// getCacheDir returns the cache directory for rr
func getCacheDir() (string, error) {
	// Prefer XDG_CACHE_HOME, fall back to ~/.cache
	cacheDir := os.Getenv("XDG_CACHE_HOME")
	if cacheDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("get home dir: %w", err)
		}
		cacheDir = filepath.Join(homeDir, ".cache")
	}
	return filepath.Join(cacheDir, "rr"), nil
}

// getCachePath returns the path to the update check cache file
func getCachePath() (string, error) {
	cacheDir, err := getCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDir, "update-check"), nil
}

// readUpdateCache reads the cached update check result
func readUpdateCache() (*updateCache, error) {
	cachePath, err := getCachePath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cache updateCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}

	return &cache, nil
}

// writeUpdateCache writes the update check result to cache
func writeUpdateCache(cache *updateCache) error {
	cachePath, err := getCachePath()
	if err != nil {
		return err
	}

	// Ensure cache directory exists
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	data, err := json.Marshal(cache)
	if err != nil {
		return err
	}

	return os.WriteFile(cachePath, data, 0644)
}

// isCacheValid returns true if the cache is still within TTL
func isCacheValid(cache *updateCache) bool {
	return time.Since(cache.CheckedAt) < updateCheckCacheTTL
}

// fetchLatestVersion fetches the latest version from GitHub
func fetchLatestVersion() (string, error) {
	client := &http.Client{Timeout: updateCheckTimeout}

	req, err := http.NewRequest("GET", githubReleasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "rr-cli")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}

	return release.TagName, nil
}

// normalizeVersion removes 'v' prefix and returns cleaned version string
func normalizeVersion(v string) string {
	return strings.TrimPrefix(v, "v")
}

// isNewerVersion returns true if latest is newer than current
// Uses simple string comparison which works for semver (v1.0.0 < v1.1.0)
func isNewerVersion(current, latest string) bool {
	// Normalize both versions
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)

	// Don't compare dev versions
	if current == "dev" || current == "" {
		return false
	}

	// Simple comparison works for semver strings
	return latest > current
}

// checkForUpdate checks if a newer version is available
// Returns the latest version if an update is available, empty string otherwise
func checkForUpdate() string {
	// Check if update checks are disabled
	if os.Getenv("RR_NO_UPDATE_CHECK") == "1" {
		return ""
	}

	// Try to read from cache first
	cache, err := readUpdateCache()
	if err == nil && isCacheValid(cache) {
		if isNewerVersion(version, cache.LatestVersion) {
			return cache.LatestVersion
		}
		return ""
	}

	// Cache is stale or doesn't exist - fetch in background
	// For the version command, we do a quick synchronous check
	// but with a short timeout so it doesn't block
	latest, err := fetchLatestVersion()
	if err != nil {
		// Silently fail - don't bother the user with network errors
		return ""
	}

	// Update cache
	newCache := &updateCache{
		LatestVersion: latest,
		CheckedAt:     time.Now(),
	}
	// Ignore cache write errors - not critical
	_ = writeUpdateCache(newCache)

	if isNewerVersion(version, latest) {
		return latest
	}

	return ""
}

// checkAndDisplayUpdate checks for updates and displays a notice if available
func checkAndDisplayUpdate() {
	latest := checkForUpdate()
	if latest == "" {
		return
	}

	fmt.Println()
	fmt.Printf("A new version is available: %s\n", formatVersion(latest))
	fmt.Println("Update with: go install github.com/rileyhilliard/rr@latest")
}

// RefreshUpdateCache forces a refresh of the update cache in the background
// This can be called during other operations to keep the cache fresh
func RefreshUpdateCache() {
	// Check if update checks are disabled
	if os.Getenv("RR_NO_UPDATE_CHECK") == "1" {
		return
	}

	// Check if cache is still valid
	cache, err := readUpdateCache()
	if err == nil && isCacheValid(cache) {
		return
	}

	// Refresh in background
	go func() {
		latest, err := fetchLatestVersion()
		if err != nil {
			return
		}
		newCache := &updateCache{
			LatestVersion: latest,
			CheckedAt:     time.Now(),
		}
		_ = writeUpdateCache(newCache)
	}()
}
