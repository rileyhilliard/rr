package cli

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/rileyhilliard/rr/internal/ui"
	"github.com/spf13/cobra"
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

// parseVersion parses a semver string into major, minor, patch integers.
// Returns (0, 0, 0) if parsing fails.
func parseVersion(v string) (major, minor, patch int) {
	v = normalizeVersion(v)
	parts := strings.Split(v, ".")
	if len(parts) >= 1 {
		_, _ = fmt.Sscanf(parts[0], "%d", &major)
	}
	if len(parts) >= 2 {
		_, _ = fmt.Sscanf(parts[1], "%d", &minor)
	}
	if len(parts) >= 3 {
		// Handle versions like "1.0.0-beta" by taking only the numeric prefix
		patchStr := parts[2]
		if idx := strings.IndexAny(patchStr, "-+"); idx > 0 {
			patchStr = patchStr[:idx]
		}
		_, _ = fmt.Sscanf(patchStr, "%d", &patch)
	}
	return
}

// isNewerVersion returns true if latest is newer than current
// Properly compares semver versions numerically
func isNewerVersion(current, latest string) bool {
	// Normalize both versions
	current = normalizeVersion(current)
	latest = normalizeVersion(latest)

	// Don't compare dev versions
	if current == "dev" || current == "" {
		return false
	}

	// Parse and compare numerically
	curMajor, curMinor, curPatch := parseVersion(current)
	latMajor, latMinor, latPatch := parseVersion(latest)

	if latMajor != curMajor {
		return latMajor > curMajor
	}
	if latMinor != curMinor {
		return latMinor > curMinor
	}
	return latPatch > curPatch
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
	fmt.Println("Update with: rr update")
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

// UpdateOptions holds options for the update command.
type UpdateOptions struct {
	Force bool // Force update even if already on latest
	Check bool // Just check for updates, don't install
}

// githubReleaseAsset represents an asset in a GitHub release
type githubReleaseAsset struct {
	Name        string `json:"name"`
	DownloadURL string `json:"browser_download_url"`
	Size        int64  `json:"size"`
}

// githubReleaseWithAssets extends githubRelease with asset information
type githubReleaseWithAssets struct {
	TagName string               `json:"tag_name"`
	HTMLURL string               `json:"html_url"`
	Assets  []githubReleaseAsset `json:"assets"`
}

// fetchLatestRelease fetches the latest release info including assets
func fetchLatestRelease() (*githubReleaseWithAssets, error) {
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest("GET", githubReleasesURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "rr-cli")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github api returned %d", resp.StatusCode)
	}

	var release githubReleaseWithAssets
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

// getAssetName returns the expected asset name for the current platform
func getAssetName() string {
	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("rr_%s_%s.%s", runtime.GOOS, runtime.GOARCH, ext)
}

// findAsset finds the appropriate asset for the current platform
func findAsset(release *githubReleaseWithAssets) *githubReleaseAsset {
	expectedName := getAssetName()
	for _, asset := range release.Assets {
		if asset.Name == expectedName {
			return &asset
		}
	}
	return nil
}

// downloadFile downloads a file from a URL to a temporary location
func downloadFile(url string, progress *ui.InlineProgress) (string, error) {
	client := &http.Client{Timeout: 5 * time.Minute}

	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download returned %d", resp.StatusCode)
	}

	// Create temp file
	tmpFile, err := os.CreateTemp("", "rr-update-*")
	if err != nil {
		return "", err
	}
	defer tmpFile.Close()

	// Copy with progress tracking
	var reader io.Reader = resp.Body
	if progress != nil {
		reader = &progressReader{
			reader:   resp.Body,
			total:    resp.ContentLength,
			progress: progress,
		}
	}

	if _, err := io.Copy(tmpFile, reader); err != nil {
		os.Remove(tmpFile.Name())
		return "", err
	}

	return tmpFile.Name(), nil
}

// progressReader wraps an io.Reader to track download progress
type progressReader struct {
	reader   io.Reader
	total    int64
	read     int64
	progress *ui.InlineProgress
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.read += int64(n)

	if pr.progress != nil && pr.total > 0 {
		percent := float64(pr.read) / float64(pr.total)
		pr.progress.Update(percent, "", "", pr.read)
	}

	return n, err
}

// extractBinary extracts the rr binary from the downloaded archive
func extractBinary(archivePath, destPath string) error {
	if strings.HasSuffix(archivePath, ".zip") {
		return extractFromZip(archivePath, destPath)
	}
	return extractFromTarGz(archivePath, destPath)
}

// extractFromTarGz extracts rr binary from a tar.gz archive
func extractFromTarGz(archivePath, destPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		// Look for the rr binary
		if header.Typeflag == tar.TypeReg && (header.Name == "rr" || strings.HasSuffix(header.Name, "/rr")) {
			return writeExecutable(tarReader, destPath)
		}
	}

	return fmt.Errorf("rr binary not found in archive")
}

// extractFromZip extracts rr binary from a zip archive
func extractFromZip(archivePath, destPath string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		// Look for the rr binary (or rr.exe on Windows)
		name := filepath.Base(file.Name)
		if name == "rr" || name == "rr.exe" {
			return extractZipEntry(file, destPath)
		}
	}

	return fmt.Errorf("rr binary not found in archive")
}

// extractZipEntry extracts a single zip entry to the destination path
func extractZipEntry(file *zip.File, destPath string) error {
	rc, err := file.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	return writeExecutable(rc, destPath)
}

// writeExecutable writes the binary to the destination path
func writeExecutable(reader io.Reader, destPath string) error {
	// Write to temp file first, then rename (atomic)
	tmpPath := destPath + ".tmp"

	// #nosec G302 - executable needs 0755 permissions
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}

	if _, err := io.Copy(out, reader); err != nil {
		out.Close()
		os.Remove(tmpPath)
		return err
	}

	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return err
	}

	return nil
}

// getCurrentBinaryPath returns the path to the currently running binary
func getCurrentBinaryPath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(execPath)
}

// Update checks for and installs the latest version of rr
func Update(opts UpdateOptions) error {
	currentVersion := version

	// Check-only mode
	if opts.Check {
		return checkAndPrintUpdate(currentVersion)
	}

	// Fetch latest release
	spinner := ui.NewSpinner("Checking for updates")
	spinner.Start()

	release, err := fetchLatestRelease()
	if err != nil {
		spinner.Fail()
		return errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't check for updates",
			"Make sure you have internet connectivity and try again.")
	}

	latestVersion := release.TagName
	spinner.Success()

	// Check if update is needed
	if !opts.Force && !isNewerVersion(currentVersion, latestVersion) {
		printUpToDate(currentVersion)
		return nil
	}

	// Find the right asset for this platform
	asset := findAsset(release)
	if asset == nil {
		return errors.New(errors.ErrExec,
			fmt.Sprintf("No release found for %s/%s", runtime.GOOS, runtime.GOARCH),
			"You can build from source: go install github.com/rileyhilliard/rr@latest")
	}

	// Get current binary path
	binaryPath, err := getCurrentBinaryPath()
	if err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't locate the current binary",
			"Try reinstalling with: go install github.com/rileyhilliard/rr@latest")
	}

	// Check if we can write to the binary location
	if err := checkWriteAccess(binaryPath); err != nil {
		return errors.WrapWithCode(err, errors.ErrExec,
			fmt.Sprintf("Can't write to %s", binaryPath),
			"Try running with sudo, or reinstall to a writable location.")
	}

	// Download the new version
	fmt.Printf("\nUpdating %s -> %s\n\n", formatVersion(currentVersion), formatVersion(latestVersion))

	downloadProgress := ui.NewInlineProgress("Downloading", os.Stdout)
	downloadProgress.Start()

	archivePath, err := downloadFile(asset.DownloadURL, downloadProgress)
	if err != nil {
		downloadProgress.Fail()
		return errors.WrapWithCode(err, errors.ErrExec,
			"Download failed",
			"Check your internet connection and try again.")
	}
	defer os.Remove(archivePath)
	downloadProgress.Success()

	// Extract and install
	installSpinner := ui.NewSpinner("Installing")
	installSpinner.Start()

	if err := extractBinary(archivePath, binaryPath); err != nil {
		installSpinner.Fail()
		return errors.WrapWithCode(err, errors.ErrExec,
			"Installation failed",
			"Try reinstalling with: go install github.com/rileyhilliard/rr@latest")
	}

	installSpinner.Success()

	// Success message
	printUpdateSuccess(latestVersion, release.HTMLURL)

	return nil
}

// checkWriteAccess checks if we can write to the given path
func checkWriteAccess(path string) error {
	// Try to open for writing
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	return file.Close()
}

// checkAndPrintUpdate just checks for updates and prints the result
func checkAndPrintUpdate(currentVersion string) error {
	spinner := ui.NewSpinner("Checking for updates")
	spinner.Start()

	release, err := fetchLatestRelease()
	if err != nil {
		spinner.Fail()
		return errors.WrapWithCode(err, errors.ErrExec,
			"Couldn't check for updates",
			"Make sure you have internet connectivity and try again.")
	}

	spinner.Success()

	if isNewerVersion(currentVersion, release.TagName) {
		printUpdateAvailable(currentVersion, release.TagName, release.HTMLURL)
	} else {
		printUpToDate(currentVersion)
	}

	return nil
}

// printUpToDate prints a friendly message when already on the latest version
func printUpToDate(currentVersion string) {
	fmt.Println()
	successStyle := ui.SuccessStyle()
	mutedStyle := ui.MutedStyle()

	fmt.Printf("%s You're on the latest version (%s)\n",
		successStyle.Render(ui.SymbolComplete),
		formatVersion(currentVersion))
	fmt.Println(mutedStyle.Render("No update needed."))
}

// printUpdateAvailable prints info about an available update
func printUpdateAvailable(currentVersion, latestVersion, releaseURL string) {
	fmt.Println()
	warningStyle := ui.WarningStyle()
	mutedStyle := ui.MutedStyle()

	fmt.Printf("%s Update available: %s -> %s\n",
		warningStyle.Render(ui.SymbolPending),
		formatVersion(currentVersion),
		formatVersion(latestVersion))
	fmt.Println()
	fmt.Println("Run 'rr update' to install the latest version.")
	fmt.Println(mutedStyle.Render("Release notes: " + releaseURL))
}

// printUpdateSuccess prints a success message after updating
func printUpdateSuccess(newVersion, releaseURL string) {
	fmt.Println()
	successStyle := ui.SuccessStyle()
	mutedStyle := ui.MutedStyle()

	fmt.Printf("%s Updated to %s\n",
		successStyle.Render(ui.SymbolComplete),
		formatVersion(newVersion))
	fmt.Println()
	fmt.Println(mutedStyle.Render("Release notes: " + releaseURL))
}

// updateCmd is the cobra command for self-updating
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update rr to the latest version",
	Long: `Check for and install the latest version of rr.

Downloads the appropriate binary for your platform from GitHub releases
and replaces the current binary.

Examples:
  rr update              # Update to latest version
  rr update --check      # Just check if update is available
  rr update --force      # Force reinstall even if on latest`,
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		check, _ := cmd.Flags().GetBool("check")
		return Update(UpdateOptions{
			Force: force,
			Check: check,
		})
	},
}

func init() {
	updateCmd.Flags().Bool("force", false, "force update even if already on latest version")
	updateCmd.Flags().Bool("check", false, "just check for updates, don't install")
	rootCmd.AddCommand(updateCmd)
}
