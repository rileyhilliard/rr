package testing

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockFS_Mkdir(t *testing.T) {
	fs := NewMockFS()

	// Should succeed on first create
	err := fs.Mkdir("/tmp/test")
	require.NoError(t, err)
	assert.True(t, fs.IsDir("/tmp/test"))

	// Should fail if already exists
	err = fs.Mkdir("/tmp/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestMockFS_MkdirAll(t *testing.T) {
	fs := NewMockFS()

	// Should create nested dirs
	err := fs.MkdirAll("/a/b/c/d")
	require.NoError(t, err)

	assert.True(t, fs.IsDir("/a"))
	assert.True(t, fs.IsDir("/a/b"))
	assert.True(t, fs.IsDir("/a/b/c"))
	assert.True(t, fs.IsDir("/a/b/c/d"))
}

func TestMockFS_WriteAndReadFile(t *testing.T) {
	fs := NewMockFS()

	// Write a file
	err := fs.WriteFile("/tmp/hello.txt", []byte("hello world"))
	require.NoError(t, err)

	// Read it back
	content, err := fs.ReadFile("/tmp/hello.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))

	// Check it exists as a file
	assert.True(t, fs.IsFile("/tmp/hello.txt"))
	assert.False(t, fs.IsDir("/tmp/hello.txt"))
}

func TestMockFS_ReadFile_NotFound(t *testing.T) {
	fs := NewMockFS()

	_, err := fs.ReadFile("/nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMockFS_Remove(t *testing.T) {
	fs := NewMockFS()

	// Create a file
	fs.WriteFile("/tmp/file.txt", []byte("data"))
	assert.True(t, fs.Exists("/tmp/file.txt"))

	// Remove it
	err := fs.Remove("/tmp/file.txt")
	require.NoError(t, err)
	assert.False(t, fs.Exists("/tmp/file.txt"))
}

func TestMockFS_Remove_Recursive(t *testing.T) {
	fs := NewMockFS()

	// Create a directory structure
	fs.MkdirAll("/tmp/dir")
	fs.WriteFile("/tmp/dir/file1.txt", []byte("data1"))
	fs.WriteFile("/tmp/dir/file2.txt", []byte("data2"))

	// Remove recursively
	err := fs.Remove("/tmp/dir")
	require.NoError(t, err)

	assert.False(t, fs.Exists("/tmp/dir"))
	assert.False(t, fs.Exists("/tmp/dir/file1.txt"))
	assert.False(t, fs.Exists("/tmp/dir/file2.txt"))
}

func TestMockClient_Exec_Mkdir(t *testing.T) {
	client := NewMockClient("testhost")

	// Mkdir should work
	_, _, code, err := client.Exec(`mkdir "/tmp/test"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.True(t, client.GetFS().IsDir("/tmp/test"))

	// Mkdir again should fail with exit code 1
	_, stderr, code, err := client.Exec(`mkdir "/tmp/test"`)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
	assert.Contains(t, string(stderr), "cannot create")
}

func TestMockClient_Exec_CatWrite(t *testing.T) {
	client := NewMockClient("testhost")

	cmd := `cat > "/tmp/file.txt" << 'EOF'
hello world
EOF`
	_, _, code, err := client.Exec(cmd)
	require.NoError(t, err)
	assert.Equal(t, 0, code)

	content, err := client.GetFS().ReadFile("/tmp/file.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))
}

func TestMockClient_Exec_CatRead(t *testing.T) {
	client := NewMockClient("testhost")

	// Write a file first
	client.GetFS().WriteFile("/tmp/data.txt", []byte("test data"))

	// Read it via cat
	stdout, _, code, err := client.Exec(`cat "/tmp/data.txt"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "test data", string(stdout))
}

func TestMockClient_Exec_CatRead_NotFound(t *testing.T) {
	client := NewMockClient("testhost")

	_, stderr, code, err := client.Exec(`cat "/nonexistent"`)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
	assert.Contains(t, string(stderr), "No such file")
}

func TestMockClient_Exec_Rm(t *testing.T) {
	client := NewMockClient("testhost")

	// Create a directory with file
	client.GetFS().MkdirAll("/tmp/dir")
	client.GetFS().WriteFile("/tmp/dir/file.txt", []byte("data"))

	// Remove it
	_, _, code, err := client.Exec(`rm -rf "/tmp/dir"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.False(t, client.GetFS().Exists("/tmp/dir"))
}

func TestMockClient_Exec_Which(t *testing.T) {
	client := NewMockClient("testhost")

	// Known command
	stdout, _, code, err := client.Exec("which rsync")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, string(stdout), "/usr/bin/rsync")

	// Unknown command
	_, _, code, err = client.Exec("which nonexistent")
	require.NoError(t, err)
	assert.Equal(t, 1, code)
}

func TestMockClient_Exec_Uname(t *testing.T) {
	client := NewMockClient("testhost")

	stdout, _, code, err := client.Exec("uname -s")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Contains(t, string(stdout), "Linux")
}

func TestMockClient_CustomResponse(t *testing.T) {
	client := NewMockClient("testhost")

	// Set a custom response
	client.SetCommandResponse("custom-cmd", CommandResponse{
		Stdout:   []byte("custom output"),
		ExitCode: 42,
	})

	stdout, _, code, err := client.Exec("custom-cmd")
	require.NoError(t, err)
	assert.Equal(t, 42, code)
	assert.Equal(t, "custom output", string(stdout))
}

func TestMockClient_Close(t *testing.T) {
	client := NewMockClient("testhost")

	// Should work before close
	_, _, _, err := client.Exec("echo test")
	require.NoError(t, err)

	// Close
	err = client.Close()
	require.NoError(t, err)

	// Should fail after close
	_, _, _, err = client.Exec("echo test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestMockClient_NewSession(t *testing.T) {
	client := NewMockClient("testhost")

	// Should work
	session, err := client.NewSession()
	require.NoError(t, err)
	require.NotNil(t, session)
	err = session.Close()
	require.NoError(t, err)

	// Should fail after client close
	client.Close()
	_, err = client.NewSession()
	assert.Error(t, err)
}

func TestMockClient_GetHostAndAddress(t *testing.T) {
	client := NewMockClient("myserver")

	assert.Equal(t, "myserver", client.GetHost())
	assert.Equal(t, "myserver:22", client.GetAddress())
}

func TestMockClient_StripRedirects(t *testing.T) {
	client := NewMockClient("testhost")
	client.GetFS().WriteFile("/tmp/test.txt", []byte("data"))

	// Command with 2>/dev/null should still work
	stdout, _, code, err := client.Exec(`cat "/tmp/test.txt" 2>/dev/null`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "data", string(stdout))
}

func TestHelpers_WithFiles(t *testing.T) {
	client := NewMockClient("testhost")

	WithFiles(client, map[string]string{
		"/tmp/a.txt": "content a",
		"/tmp/b.txt": "content b",
	})

	a, err := client.GetFS().ReadFile("/tmp/a.txt")
	require.NoError(t, err)
	assert.Equal(t, "content a", string(a))

	b, err := client.GetFS().ReadFile("/tmp/b.txt")
	require.NoError(t, err)
	assert.Equal(t, "content b", string(b))
}

func TestHelpers_WithDirs(t *testing.T) {
	client := NewMockClient("testhost")

	WithDirs(client, []string{"/a/b", "/c/d/e"})

	assert.True(t, client.GetFS().IsDir("/a/b"))
	assert.True(t, client.GetFS().IsDir("/c/d/e"))
}
