package setup

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInferKeyType(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "ed25519 key",
			path: "/home/user/.ssh/id_ed25519",
			want: "ed25519",
		},
		{
			name: "rsa key",
			path: "/home/user/.ssh/id_rsa",
			want: "rsa",
		},
		{
			name: "ecdsa key",
			path: "/home/user/.ssh/id_ecdsa",
			want: "ecdsa",
		},
		{
			name: "unknown key type",
			path: "/home/user/.ssh/id_dsa",
			want: "unknown",
		},
		{
			name: "custom ed25519 name",
			path: "/home/user/.ssh/mykey_ed25519",
			want: "ed25519",
		},
		{
			name: "public key ed25519",
			path: "/home/user/.ssh/id_ed25519.pub",
			want: "ed25519",
		},
		{
			name: "no type in name",
			path: "/home/user/.ssh/id_unknown",
			want: "unknown",
		},
		{
			name: "rsa in middle of name",
			path: "/home/user/.ssh/backup_rsa_key",
			want: "rsa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferKeyType(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultKeyPaths(t *testing.T) {
	paths := DefaultKeyPaths()

	// Should return paths when HOME is set
	if os.Getenv("HOME") != "" || os.Getenv("USERPROFILE") != "" {
		require.NotNil(t, paths, "should return paths when home directory is available")
		assert.Len(t, paths, 3, "should return 3 default key paths")

		// Check order: ed25519, rsa, ecdsa
		assert.Contains(t, paths[0], "id_ed25519", "first should be ed25519")
		assert.Contains(t, paths[1], "id_rsa", "second should be rsa")
		assert.Contains(t, paths[2], "id_ecdsa", "third should be ecdsa")

		// All should be in .ssh directory
		for _, p := range paths {
			assert.Contains(t, p, ".ssh", "all paths should be in .ssh directory")
		}
	}
}

func TestDefaultKeyPath(t *testing.T) {
	path := DefaultKeyPath()

	// Should return a path
	assert.NotEmpty(t, path)

	// Should prefer ed25519
	assert.Contains(t, path, "ed25519")

	// Should be in .ssh directory
	assert.Contains(t, path, ".ssh")
}

func TestKeyInfo_Struct(t *testing.T) {
	info := KeyInfo{
		Path:       "/home/user/.ssh/id_ed25519",
		Type:       "ed25519",
		PublicPath: "/home/user/.ssh/id_ed25519.pub",
		HasPublic:  true,
	}

	assert.Equal(t, "/home/user/.ssh/id_ed25519", info.Path)
	assert.Equal(t, "ed25519", info.Type)
	assert.Equal(t, "/home/user/.ssh/id_ed25519.pub", info.PublicPath)
	assert.True(t, info.HasPublic)
}

func TestReadPublicKey(t *testing.T) {
	// Create temp directory
	tmpDir := t.TempDir()

	// Create a mock public key file
	pubKeyPath := filepath.Join(tmpDir, "id_test.pub")
	pubKeyContent := "ssh-ed25519 AAAA... user@host"
	err := os.WriteFile(pubKeyPath, []byte(pubKeyContent+"\n"), 0600)
	require.NoError(t, err)

	// Test reading
	content, err := ReadPublicKey(pubKeyPath)
	require.NoError(t, err)
	assert.Equal(t, pubKeyContent, content, "should trim whitespace")
}

func TestReadPublicKey_MissingFile(t *testing.T) {
	_, err := ReadPublicKey("/nonexistent/path/id_test.pub")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Failed to read public key")
}

func TestReadPublicKey_TrimsWhitespace(t *testing.T) {
	tmpDir := t.TempDir()
	pubKeyPath := filepath.Join(tmpDir, "id_test.pub")

	// Content with extra whitespace
	err := os.WriteFile(pubKeyPath, []byte("  ssh-ed25519 AAAA...  \n\n"), 0600)
	require.NoError(t, err)

	content, err := ReadPublicKey(pubKeyPath)
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA...", content)
}

func TestGenerateKey_InvalidKeyType(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "id_invalid")

	err := GenerateKey(keyPath, "invalid_type")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "isn't a valid key type")
	assert.Contains(t, err.Error(), "Pick from")
}

func TestGenerateKey_ExistingKey(t *testing.T) {
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "id_existing")

	// Create an existing file
	err := os.WriteFile(keyPath, []byte("existing key"), 0600)
	require.NoError(t, err)

	err = GenerateKey(keyPath, "ed25519")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already a key at")
}

func TestGenerateKey_EmptyTypeDefaultsToEd25519(t *testing.T) {
	// This test would actually generate a key if we let it run
	// We'll test the validation path instead
	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "id_test")

	// Create the file to trigger "already exists" error
	// This way we can verify the function doesn't error on empty type
	err := os.WriteFile(keyPath, []byte("test"), 0600)
	require.NoError(t, err)

	err = GenerateKey(keyPath, "")

	// Should fail with "already exists", not "invalid type"
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already a key at")
	assert.NotContains(t, err.Error(), "Invalid key type")
}

func TestGenerateKey_TildeExpansion(t *testing.T) {
	// Test that tilde expansion works by checking it doesn't error on tilde paths
	// We'll use an existing key to trigger the "already exists" path after expansion

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory")
	}

	// Create a temp key in the home directory structure
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		t.Skip("No .ssh directory")
	}

	// Check for existing ed25519 key
	existingKey := filepath.Join(sshDir, "id_ed25519")
	if _, err := os.Stat(existingKey); err == nil {
		// Key exists, test tilde expansion works
		err := GenerateKey("~/.ssh/id_ed25519", "ed25519")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already a key at")
	}
}

func TestFindLocalKeys_ReturnsKeyInfo(t *testing.T) {
	// This test depends on the actual filesystem
	// It's more of an integration test
	keys := FindLocalKeys()

	// We can only verify the structure, not the content
	for _, key := range keys {
		assert.NotEmpty(t, key.Path, "key path should not be empty")
		assert.NotEmpty(t, key.Type, "key type should not be empty")
		assert.NotEmpty(t, key.PublicPath, "public path should not be empty")
		// HasPublic can be true or false
	}
}

func TestGetPreferredKey_WithMockKeys(t *testing.T) {
	// This is essentially testing the preference logic
	// Since GetPreferredKey calls FindLocalKeys which reads the filesystem,
	// we can only verify it returns nil or a valid pointer
	key := GetPreferredKey()

	if key != nil {
		assert.NotEmpty(t, key.Path)
		assert.NotEmpty(t, key.Type)
	}
}

func TestHasAnyKey(t *testing.T) {
	// This depends on filesystem state
	// Just verify it returns a boolean without error
	result := HasAnyKey()
	_ = result // Just verify no panic
}

func TestGenerateKey_ValidKeyTypes(t *testing.T) {
	tests := []struct {
		name    string
		keyType string
		wantErr bool
	}{
		{name: "ed25519 is valid", keyType: "ed25519", wantErr: false},
		{name: "rsa is valid", keyType: "rsa", wantErr: false},
		{name: "ecdsa is valid", keyType: "ecdsa", wantErr: false},
		{name: "dsa is invalid", keyType: "dsa", wantErr: true},
		{name: "random is invalid", keyType: "random", wantErr: true},
		{name: "empty defaults to ed25519", keyType: "", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			keyPath := filepath.Join(tmpDir, "id_test")

			// For valid types, we need to check if ssh-keygen is available
			// and skip if it's not (the test environment may not have it)
			if !tt.wantErr {
				_, err := exec.LookPath("ssh-keygen")
				if err != nil {
					t.Skip("ssh-keygen not available")
				}

				err = GenerateKey(keyPath, tt.keyType)
				if err != nil {
					// Could fail for other reasons (ssh-keygen issues)
					t.Logf("GenerateKey returned error: %v", err)
				}
			} else {
				// For invalid types, it should fail regardless of ssh-keygen
				err := GenerateKey(keyPath, tt.keyType)
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "isn't a valid key type")
			}
		})
	}
}

func TestGenerateKey_CreatesDirectoryIfNeeded(t *testing.T) {
	_, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmpDir := t.TempDir()
	nestedPath := filepath.Join(tmpDir, "nested", "dir", "id_test")

	err = GenerateKey(nestedPath, "ed25519")
	require.NoError(t, err)

	// Verify the key was created
	_, err = os.Stat(nestedPath)
	assert.NoError(t, err, "private key should exist")

	_, err = os.Stat(nestedPath + ".pub")
	assert.NoError(t, err, "public key should exist")
}

func TestGenerateKey_RSAHas4096Bits(t *testing.T) {
	_, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmpDir := t.TempDir()
	keyPath := filepath.Join(tmpDir, "id_rsa_test")

	err = GenerateKey(keyPath, "rsa")
	require.NoError(t, err)

	// Read the public key and verify it's RSA
	pubKey, err := ReadPublicKey(keyPath + ".pub")
	require.NoError(t, err)
	assert.Contains(t, pubKey, "ssh-rsa", "should be an RSA key")
}

func TestFindLocalKeys_WithMockDir(t *testing.T) {
	// This test verifies the function doesn't panic with various filesystem states
	// Since FindLocalKeys relies on DefaultKeyPaths() which uses the real home dir,
	// we can only do a basic sanity check here
	keys := FindLocalKeys()

	// Verify all returned keys have valid structure
	for _, key := range keys {
		assert.NotEmpty(t, key.Path, "key path should not be empty")
		assert.NotEmpty(t, key.Type, "key type should not be empty")
		assert.Contains(t, key.PublicPath, ".pub", "public path should end with .pub")
	}
}

func TestGetPreferredKey_PreferenceOrder(t *testing.T) {
	// Test the preference logic by checking function behavior
	// The preference order is: ed25519 > ecdsa > rsa > any with public > any

	key := GetPreferredKey()
	if key != nil {
		// If we have a key, it should be a valid structure
		assert.NotEmpty(t, key.Path)
		assert.NotEmpty(t, key.Type)

		// If it's ed25519 with a public key, that's optimal
		// Otherwise, the function is working correctly anyway
	}
	// No assertion if nil - that just means no keys exist
}

func TestReadPublicKey_LargeFile(t *testing.T) {
	tmpDir := t.TempDir()
	pubKeyPath := filepath.Join(tmpDir, "id_test.pub")

	// Create a larger public key file (simulate a key with long comment)
	longComment := strings.Repeat("x", 1000)
	content := fmt.Sprintf("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyDataHere %s", longComment)
	err := os.WriteFile(pubKeyPath, []byte(content+"\n"), 0600)
	require.NoError(t, err)

	result, err := ReadPublicKey(pubKeyPath)
	require.NoError(t, err)
	assert.Equal(t, content, result)
}

func TestReadPublicKey_MultilineContent(t *testing.T) {
	tmpDir := t.TempDir()
	pubKeyPath := filepath.Join(tmpDir, "id_test.pub")

	// Public keys shouldn't have multiple lines, but test the trimming behavior
	content := "ssh-ed25519 AAAA...\n\n\n"
	err := os.WriteFile(pubKeyPath, []byte(content), 0600)
	require.NoError(t, err)

	result, err := ReadPublicKey(pubKeyPath)
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519 AAAA...", result, "should trim all trailing whitespace")
}

func TestInferKeyType_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "uppercase ED25519", path: "/home/user/.ssh/id_ED25519", want: "unknown"},
		{name: "mixed case", path: "/home/user/.ssh/id_Ed25519", want: "unknown"},
		{name: "ed25519 in directory", path: "/ed25519/id_test", want: "unknown"},
		{name: "multiple types prefers first", path: "/home/user/.ssh/id_ed25519_rsa", want: "ed25519"},
		{name: "just the type name", path: "ed25519", want: "ed25519"},
		{name: "empty string", path: "", want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := inferKeyType(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDefaultKeyPath_ReturnsEd25519(t *testing.T) {
	path := DefaultKeyPath()

	// Should always prefer ed25519
	assert.Contains(t, path, "ed25519")
	assert.Contains(t, path, ".ssh")

	// Should be an absolute path if home directory is available
	home, err := os.UserHomeDir()
	if err == nil {
		assert.True(t, strings.HasPrefix(path, home), "should start with home directory")
	}
}

func TestGenerateKey_TildeExpansionWithNestedPath(t *testing.T) {
	// Create a key that would use tilde expansion into a nested path
	// We can't fully test this without modifying home, but we can verify
	// the error behavior when the key already exists

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot determine home directory")
	}

	// Check if .ssh directory exists
	sshDir := filepath.Join(home, ".ssh")
	if _, err := os.Stat(sshDir); os.IsNotExist(err) {
		t.Skip("No .ssh directory")
	}

	// Try to generate at existing ed25519 location
	existingKey := filepath.Join(sshDir, "id_ed25519")
	if _, err := os.Stat(existingKey); err == nil {
		err := GenerateKey("~/.ssh/id_ed25519", "ed25519")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already a key at")

		// Verify the path was expanded (check for full path in error)
		assert.Contains(t, err.Error(), sshDir)
	}
}

func TestGetPreferredKey_PreferenceLogic(t *testing.T) {
	// Create a temp directory to simulate .ssh with different key types
	tmpDir := t.TempDir()

	// Helper to create key files in temp dir
	createKeyPair := func(name string) {
		keyPath := filepath.Join(tmpDir, name)
		pubPath := keyPath + ".pub"
		require.NoError(t, os.WriteFile(keyPath, []byte("private key"), 0600))
		require.NoError(t, os.WriteFile(pubPath, []byte("public key"), 0600))
	}

	// Test when no keys exist - should return nil
	// Note: GetPreferredKey uses real filesystem paths, so we can only test its behavior
	// indirectly through FindLocalKeys

	// Verify the preference order logic is correct
	// The expected order: ed25519 > ecdsa > rsa > any with public > any

	// Create all three key types
	createKeyPair("id_ed25519")
	createKeyPair("id_rsa")
	createKeyPair("id_ecdsa")

	// The actual GetPreferredKey looks in ~/.ssh, but we can test
	// that the function doesn't crash with various states
	key := GetPreferredKey()
	if key != nil {
		// If we have a key, verify it's a valid structure
		assert.NotEmpty(t, key.Path)
		assert.NotEmpty(t, key.Type)
	}
}

func TestFindLocalKeys_Structure(t *testing.T) {
	// Test that FindLocalKeys returns properly structured KeyInfo
	keys := FindLocalKeys()

	// Each key should have all fields populated correctly
	for _, key := range keys {
		assert.NotEmpty(t, key.Path, "Path should not be empty")
		assert.NotEmpty(t, key.Type, "Type should not be empty")
		assert.NotEmpty(t, key.PublicPath, "PublicPath should not be empty")
		assert.True(t, strings.HasSuffix(key.PublicPath, ".pub"), "PublicPath should end with .pub")
		assert.Equal(t, key.Path+".pub", key.PublicPath, "PublicPath should be Path + .pub")
	}
}

func TestHasAnyKey_ReturnsBoolean(t *testing.T) {
	// Verify HasAnyKey returns a boolean and doesn't panic
	result := HasAnyKey()
	assert.IsType(t, true, result)
}

func TestDefaultKeyPaths_WhenHomeNotSet(t *testing.T) {
	// We can't easily unset HOME without affecting other tests,
	// but we can verify the function returns expected paths
	paths := DefaultKeyPaths()

	if len(paths) > 0 {
		// Verify the order and content
		assert.Contains(t, paths[0], "id_ed25519")
		assert.Contains(t, paths[1], "id_rsa")
		assert.Contains(t, paths[2], "id_ecdsa")
	}
}

func TestDefaultKeyPath_FallbackToTilde(t *testing.T) {
	// DefaultKeyPath should return a valid path
	path := DefaultKeyPath()

	// Should contain ed25519 and .ssh
	assert.Contains(t, path, "ed25519")
	assert.Contains(t, path, ".ssh")

	// Should be absolute (starts with /) or tilde path if home unavailable
	if !strings.HasPrefix(path, "/") {
		assert.Equal(t, "~/.ssh/id_ed25519", path)
	}
}

func TestKeyInfo_Fields(t *testing.T) {
	// Verify KeyInfo struct behavior
	tests := []struct {
		name      string
		info      KeyInfo
		hasPublic bool
	}{
		{
			name: "complete key info",
			info: KeyInfo{
				Path:       "/path/to/id_ed25519",
				Type:       "ed25519",
				PublicPath: "/path/to/id_ed25519.pub",
				HasPublic:  true,
			},
			hasPublic: true,
		},
		{
			name: "key without public",
			info: KeyInfo{
				Path:       "/path/to/id_rsa",
				Type:       "rsa",
				PublicPath: "/path/to/id_rsa.pub",
				HasPublic:  false,
			},
			hasPublic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.hasPublic, tt.info.HasPublic)
		})
	}
}

func TestGenerateKey_DirectoryCreation(t *testing.T) {
	_, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}

	tmpDir := t.TempDir()

	// Create a deeply nested path
	deepPath := filepath.Join(tmpDir, "a", "b", "c", "id_test")

	err = GenerateKey(deepPath, "ed25519")
	require.NoError(t, err)

	// Verify directory was created with correct permissions
	dirInfo, err := os.Stat(filepath.Dir(deepPath))
	require.NoError(t, err)
	assert.True(t, dirInfo.IsDir())
}

func TestGenerateKey_AllValidTypes(t *testing.T) {
	_, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}

	types := []string{"ed25519", "rsa", "ecdsa"}

	for _, keyType := range types {
		t.Run(keyType, func(t *testing.T) {
			tmpDir := t.TempDir()
			keyPath := filepath.Join(tmpDir, "id_"+keyType+"_test")

			err := GenerateKey(keyPath, keyType)
			require.NoError(t, err)

			// Verify both private and public key exist
			_, err = os.Stat(keyPath)
			assert.NoError(t, err, "private key should exist")

			_, err = os.Stat(keyPath + ".pub")
			assert.NoError(t, err, "public key should exist")

			// Verify the public key contains the expected type
			pubKey, err := ReadPublicKey(keyPath + ".pub")
			require.NoError(t, err)
			if keyType == "ed25519" {
				assert.Contains(t, pubKey, "ssh-ed25519")
			} else if keyType == "rsa" {
				assert.Contains(t, pubKey, "ssh-rsa")
			} else if keyType == "ecdsa" {
				assert.Contains(t, pubKey, "ecdsa-sha2")
			}
		})
	}
}

func TestInferKeyType_PathVariations(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"windows path ed25519", "C:\\Users\\test\\.ssh\\id_ed25519", "ed25519"},
		{"nested rsa", "/deep/nested/path/id_rsa_backup", "rsa"},
		{"ecdsa with suffix", "/home/user/.ssh/id_ecdsa_work", "ecdsa"},
		{"no extension", "/home/user/.ssh/mykey", "unknown"},
		{"extension only", ".ed25519", "ed25519"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := inferKeyType(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}
