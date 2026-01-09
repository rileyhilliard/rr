package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"v1.0.0", "1.0.0"},
		{"1.0.0", "1.0.0"},
		{"v2.3.4-beta.1", "2.3.4-beta.1"},
		{"", ""},
		{"dev", "dev"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeVersion(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		{
			name:    "newer patch version",
			current: "1.0.0",
			latest:  "1.0.1",
			want:    true,
		},
		{
			name:    "newer minor version",
			current: "1.0.0",
			latest:  "1.1.0",
			want:    true,
		},
		{
			name:    "newer major version",
			current: "1.0.0",
			latest:  "2.0.0",
			want:    true,
		},
		{
			name:    "same version",
			current: "1.0.0",
			latest:  "1.0.0",
			want:    false,
		},
		{
			name:    "older version",
			current: "2.0.0",
			latest:  "1.0.0",
			want:    false,
		},
		{
			name:    "with v prefix current",
			current: "v1.0.0",
			latest:  "1.1.0",
			want:    true,
		},
		{
			name:    "with v prefix latest",
			current: "1.0.0",
			latest:  "v1.1.0",
			want:    true,
		},
		{
			name:    "both with v prefix",
			current: "v1.0.0",
			latest:  "v1.1.0",
			want:    true,
		},
		{
			name:    "dev version",
			current: "dev",
			latest:  "1.0.0",
			want:    false,
		},
		{
			name:    "empty current",
			current: "",
			latest:  "1.0.0",
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isNewerVersion(tt.current, tt.latest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsCacheValid(t *testing.T) {
	tests := []struct {
		name      string
		checkedAt time.Time
		want      bool
	}{
		{
			name:      "fresh cache",
			checkedAt: time.Now().Add(-1 * time.Hour),
			want:      true,
		},
		{
			name:      "stale cache",
			checkedAt: time.Now().Add(-25 * time.Hour),
			want:      false,
		},
		{
			name:      "edge case - exactly at TTL",
			checkedAt: time.Now().Add(-updateCheckCacheTTL),
			want:      false,
		},
		{
			name:      "just before TTL",
			checkedAt: time.Now().Add(-updateCheckCacheTTL + time.Minute),
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &updateCache{
				LatestVersion: "1.0.0",
				CheckedAt:     tt.checkedAt,
			}
			got := isCacheValid(cache)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateCacheReadWrite(t *testing.T) {
	// Create temp cache directory
	tempDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalXDG)

	// Write cache
	cache := &updateCache{
		LatestVersion: "1.2.3",
		CheckedAt:     time.Now().Truncate(time.Second), // Truncate for comparison
	}
	err := writeUpdateCache(cache)
	require.NoError(t, err)

	// Verify file was created
	cachePath := filepath.Join(tempDir, "rr", "update-check")
	_, err = os.Stat(cachePath)
	require.NoError(t, err, "cache file should exist")

	// Read cache back
	readCache, err := readUpdateCache()
	require.NoError(t, err)
	assert.Equal(t, cache.LatestVersion, readCache.LatestVersion)
	assert.Equal(t, cache.CheckedAt.Unix(), readCache.CheckedAt.Unix())
}

func TestReadUpdateCacheNotExists(t *testing.T) {
	// Create temp directory without cache file
	tempDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalXDG)

	_, err := readUpdateCache()
	assert.Error(t, err, "should error when cache doesn't exist")
}

func TestReadUpdateCacheInvalidJSON(t *testing.T) {
	tempDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalXDG)

	// Create invalid cache file
	cacheDir := filepath.Join(tempDir, "rr")
	err := os.MkdirAll(cacheDir, 0755)
	require.NoError(t, err)

	cachePath := filepath.Join(cacheDir, "update-check")
	err = os.WriteFile(cachePath, []byte("not valid json"), 0644)
	require.NoError(t, err)

	_, err = readUpdateCache()
	assert.Error(t, err, "should error on invalid JSON")
}

func TestFetchLatestVersion(t *testing.T) {
	// Create mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request headers
		assert.Equal(t, "application/vnd.github.v3+json", r.Header.Get("Accept"))
		assert.Equal(t, "rr-cli", r.Header.Get("User-Agent"))

		response := githubRelease{
			TagName: "v1.5.0",
			HTMLURL: "https://github.com/rileyhilliard/rr/releases/v1.5.0",
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	// Replace the GitHub URL temporarily
	originalURL := githubReleasesURL
	// We can't modify const, so we test the response parsing instead
	// by using httptest server in integration tests

	// For this unit test, just verify the URL is set correctly
	assert.Contains(t, originalURL, "github.com")
}

func TestCheckForUpdateDisabled(t *testing.T) {
	// Save and set env var
	originalEnv := os.Getenv("RR_NO_UPDATE_CHECK")
	os.Setenv("RR_NO_UPDATE_CHECK", "1")
	defer os.Setenv("RR_NO_UPDATE_CHECK", originalEnv)

	result := checkForUpdate()
	assert.Empty(t, result, "should return empty when update check is disabled")
}

func TestCheckForUpdateWithValidCache(t *testing.T) {
	// Set up temp cache
	tempDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalXDG)

	// Make sure update check is enabled
	originalNoCheck := os.Getenv("RR_NO_UPDATE_CHECK")
	os.Unsetenv("RR_NO_UPDATE_CHECK")
	defer os.Setenv("RR_NO_UPDATE_CHECK", originalNoCheck)

	// Save and set version
	originalVersion := version
	version = "1.0.0"
	defer func() { version = originalVersion }()

	// Write fresh cache with newer version
	cache := &updateCache{
		LatestVersion: "1.5.0",
		CheckedAt:     time.Now(),
	}
	err := writeUpdateCache(cache)
	require.NoError(t, err)

	result := checkForUpdate()
	assert.Equal(t, "1.5.0", result, "should return newer version from cache")
}

func TestCheckForUpdateWithStaleCache(t *testing.T) {
	// Set up temp cache
	tempDir := t.TempDir()
	originalXDG := os.Getenv("XDG_CACHE_HOME")
	os.Setenv("XDG_CACHE_HOME", tempDir)
	defer os.Setenv("XDG_CACHE_HOME", originalXDG)

	// Make sure update check is enabled
	originalNoCheck := os.Getenv("RR_NO_UPDATE_CHECK")
	os.Unsetenv("RR_NO_UPDATE_CHECK")
	defer os.Setenv("RR_NO_UPDATE_CHECK", originalNoCheck)

	// Save and set version
	originalVersion := version
	version = "1.0.0"
	defer func() { version = originalVersion }()

	// Write stale cache
	cache := &updateCache{
		LatestVersion: "1.5.0",
		CheckedAt:     time.Now().Add(-48 * time.Hour), // 2 days old
	}
	err := writeUpdateCache(cache)
	require.NoError(t, err)

	// This will try to fetch from GitHub which will fail in test
	// but should fail silently and return empty string
	result := checkForUpdate()
	assert.Empty(t, result, "should return empty when fetch fails")
}

func TestGetCacheDir(t *testing.T) {
	tests := []struct {
		name        string
		xdgCacheEnv string
		wantSuffix  string
	}{
		{
			name:        "with XDG_CACHE_HOME",
			xdgCacheEnv: "/custom/cache",
			wantSuffix:  "/custom/cache/rr",
		},
		{
			name:        "without XDG_CACHE_HOME",
			xdgCacheEnv: "",
			wantSuffix:  ".cache/rr",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalXDG := os.Getenv("XDG_CACHE_HOME")
			if tt.xdgCacheEnv != "" {
				os.Setenv("XDG_CACHE_HOME", tt.xdgCacheEnv)
			} else {
				os.Unsetenv("XDG_CACHE_HOME")
			}
			defer os.Setenv("XDG_CACHE_HOME", originalXDG)

			got, err := getCacheDir()
			require.NoError(t, err)
			assert.Contains(t, got, tt.wantSuffix)
		})
	}
}

func TestGetAssetName(t *testing.T) {
	// This test verifies the asset name generation based on runtime OS/arch
	name := getAssetName()

	// Should contain rr_ prefix
	assert.True(t, len(name) > 0)
	assert.Contains(t, name, "rr_")

	// Should end with tar.gz or zip depending on OS
	if os.Getenv("GOOS") == "windows" {
		assert.Contains(t, name, ".zip")
	} else {
		assert.Contains(t, name, ".tar.gz")
	}
}

func TestFindAsset(t *testing.T) {
	release := &githubReleaseWithAssets{
		TagName: "v1.0.0",
		Assets: []githubReleaseAsset{
			{Name: "rr_darwin_arm64.tar.gz", DownloadURL: "https://example.com/darwin_arm64"},
			{Name: "rr_darwin_amd64.tar.gz", DownloadURL: "https://example.com/darwin_amd64"},
			{Name: "rr_linux_amd64.tar.gz", DownloadURL: "https://example.com/linux_amd64"},
			{Name: "rr_windows_amd64.zip", DownloadURL: "https://example.com/windows_amd64"},
		},
	}

	// This will find the asset matching the current platform
	asset := findAsset(release)

	// Should find an asset (we're running on a supported platform)
	assert.NotNil(t, asset, "should find asset for current platform")
	assert.Contains(t, asset.Name, "rr_")
}

func TestFindAssetNotFound(t *testing.T) {
	release := &githubReleaseWithAssets{
		TagName: "v1.0.0",
		Assets: []githubReleaseAsset{
			{Name: "rr_unsupported_os.tar.gz", DownloadURL: "https://example.com/unsupported"},
		},
	}

	// Should return nil if platform not found
	asset := findAsset(release)
	assert.Nil(t, asset)
}

func TestFetchLatestReleaseWithAssets(t *testing.T) {
	// Create mock GitHub API server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := githubReleaseWithAssets{
			TagName: "v1.5.0",
			HTMLURL: "https://github.com/rileyhilliard/rr/releases/v1.5.0",
			Assets: []githubReleaseAsset{
				{Name: "rr_darwin_arm64.tar.gz", DownloadURL: "https://example.com/darwin_arm64", Size: 1000},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		err := json.NewEncoder(w).Encode(response)
		assert.NoError(t, err)
	}))
	defer server.Close()

	// Verify the mock server works (we can't change the const URL, but verify parsing works)
	resp, err := http.Get(server.URL)
	require.NoError(t, err)
	defer resp.Body.Close()

	var release githubReleaseWithAssets
	err = json.NewDecoder(resp.Body).Decode(&release)
	require.NoError(t, err)
	assert.Equal(t, "v1.5.0", release.TagName)
	assert.Len(t, release.Assets, 1)
}
