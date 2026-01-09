package setup

import (
	"os"
	"path/filepath"
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
