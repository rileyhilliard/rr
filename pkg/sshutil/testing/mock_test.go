package testing

import (
	"bytes"
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

func TestMockClient_SendRequest(t *testing.T) {
	client := NewMockClient("testhost")

	// Should succeed when connection is open
	accepted, payload, err := client.SendRequest("keepalive@openssh.com", true, nil)
	require.NoError(t, err)
	assert.True(t, accepted)
	assert.Nil(t, payload)

	// Should work with arbitrary request names
	accepted, _, err = client.SendRequest("test-request", true, []byte("test payload"))
	require.NoError(t, err)
	assert.True(t, accepted)
}

func TestMockClient_SendRequest_AfterClose(t *testing.T) {
	client := NewMockClient("testhost")

	// Close the connection
	err := client.Close()
	require.NoError(t, err)

	// SendRequest should fail after close
	_, _, err = client.SendRequest("keepalive@openssh.com", true, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestMockClient_Exec_TestDirExists(t *testing.T) {
	client := NewMockClient("testhost")
	client.GetFS().MkdirAll("/tmp/mydir")

	_, _, code, err := client.Exec(`test -d "/tmp/mydir"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code, "directory should exist")
}

func TestMockClient_Exec_TestDirNotExists(t *testing.T) {
	client := NewMockClient("testhost")

	_, _, code, err := client.Exec(`test -d "/tmp/nonexistent"`)
	require.NoError(t, err)
	assert.Equal(t, 1, code, "directory should not exist")
}

func TestMockClient_Exec_TestFileExists(t *testing.T) {
	client := NewMockClient("testhost")
	client.GetFS().WriteFile("/tmp/myfile.txt", []byte("content"))

	_, _, code, err := client.Exec(`test -f "/tmp/myfile.txt"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code, "file should exist")
}

func TestMockClient_Exec_TestFileNotExists(t *testing.T) {
	client := NewMockClient("testhost")

	_, _, code, err := client.Exec(`test -f "/tmp/nonexistent.txt"`)
	require.NoError(t, err)
	assert.Equal(t, 1, code, "file should not exist")
}

func TestMockClient_Exec_MkdirWithParent(t *testing.T) {
	client := NewMockClient("testhost")

	_, _, code, err := client.Exec(`mkdir -p "/tmp/a/b/c"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)

	assert.True(t, client.GetFS().IsDir("/tmp/a/b/c"))
	assert.True(t, client.GetFS().IsDir("/tmp/a/b"))
	assert.True(t, client.GetFS().IsDir("/tmp/a"))
}

func TestMockClient_Exec_MkdirNoParent(t *testing.T) {
	client := NewMockClient("testhost")

	// Create parent first
	client.GetFS().MkdirAll("/tmp")

	_, _, code, err := client.Exec(`mkdir "/tmp/newdir"`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.True(t, client.GetFS().IsDir("/tmp/newdir"))
}

func TestMockClient_Exec_MkdirNoParentFails(t *testing.T) {
	client := NewMockClient("testhost")

	// Try to create dir without parent
	_, stderr, code, err := client.Exec(`mkdir "/nonexistent/dir"`)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
	assert.Contains(t, string(stderr), "No such file")
}

func TestMockClient_ExecStream(t *testing.T) {
	client := NewMockClient("testhost")
	client.GetFS().WriteFile("/tmp/data.txt", []byte("stream content"))

	var stdout, stderr bytes.Buffer
	code, err := client.ExecStream(`cat "/tmp/data.txt"`, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "stream content", stdout.String())
	assert.Empty(t, stderr.String())
}

func TestMockClient_ExecStream_WithError(t *testing.T) {
	client := NewMockClient("testhost")

	var stdout, stderr bytes.Buffer
	code, err := client.ExecStream(`cat "/nonexistent"`, &stdout, &stderr)
	require.NoError(t, err)
	assert.Equal(t, 1, code)
	assert.Contains(t, stderr.String(), "No such file")
}

func TestMockClient_RegexPattern(t *testing.T) {
	client := NewMockClient("testhost")

	// Set a regex pattern response
	client.SetCommandResponse("echo .*", CommandResponse{
		Stdout:   []byte("matched"),
		ExitCode: 0,
	})

	stdout, _, code, err := client.Exec("echo hello world")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
	assert.Equal(t, "matched", string(stdout))
}

func TestMockClient_CustomError(t *testing.T) {
	client := NewMockClient("testhost")

	// Set a custom error response
	client.SetCommandResponse("fail-cmd", CommandResponse{
		Error: assert.AnError,
	})

	_, _, _, err := client.Exec("fail-cmd")
	assert.Error(t, err)
}

func TestMockFS_Exists(t *testing.T) {
	fs := NewMockFS()

	assert.False(t, fs.Exists("/nonexistent"))

	fs.WriteFile("/tmp/file.txt", []byte("content"))
	assert.True(t, fs.Exists("/tmp/file.txt"))

	fs.MkdirAll("/tmp/dir")
	assert.True(t, fs.Exists("/tmp/dir"))
}

func TestMockFS_IsFile(t *testing.T) {
	fs := NewMockFS()

	fs.WriteFile("/tmp/file.txt", []byte("content"))
	fs.MkdirAll("/tmp/dir")

	assert.True(t, fs.IsFile("/tmp/file.txt"))
	assert.False(t, fs.IsFile("/tmp/dir"))
	assert.False(t, fs.IsFile("/nonexistent"))
}

func TestMockFS_IsDir(t *testing.T) {
	fs := NewMockFS()

	fs.WriteFile("/tmp/file.txt", []byte("content"))
	fs.MkdirAll("/tmp/dir")

	assert.False(t, fs.IsDir("/tmp/file.txt"))
	assert.True(t, fs.IsDir("/tmp/dir"))
	assert.False(t, fs.IsDir("/nonexistent"))
}

func TestExtractPath(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{
			name:   "double quoted",
			input:  `"/path/to/file"`,
			expect: "/path/to/file",
		},
		{
			name:   "single quoted",
			input:  `'/path/to/file'`,
			expect: "/path/to/file",
		},
		{
			name:   "unquoted",
			input:  "/path/to/file",
			expect: "/path/to/file",
		},
		{
			name:   "with trailing text",
			input:  "/path/to/file extra stuff",
			expect: "/path/to/file",
		},
		{
			name:   "empty",
			input:  "",
			expect: "",
		},
		{
			name:   "whitespace only",
			input:  "   ",
			expect: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractPath(tt.input)
			assert.Equal(t, tt.expect, result)
		})
	}
}

func TestMockClient_Exec_BracketTestSyntax(t *testing.T) {
	client := NewMockClient("testhost")
	client.GetFS().MkdirAll("/tmp/mydir")

	// Test with bracket syntax: [ -d "/path" ]
	_, _, code, err := client.Exec(`[ -d "/tmp/mydir" ]`)
	require.NoError(t, err)
	assert.Equal(t, 0, code)
}

func TestMockClient_Exec_UnknownCommand(t *testing.T) {
	client := NewMockClient("testhost")

	// Unknown commands should return success by default
	_, _, code, err := client.Exec("unknown-command arg1 arg2")
	require.NoError(t, err)
	assert.Equal(t, 0, code)
}

func TestMockClient_Exec_UnameVariants(t *testing.T) {
	client := NewMockClient("testhost")

	tests := []struct {
		cmd      string
		contains string
	}{
		{"uname", "Linux"},
		{"uname -s", "Linux"},
		{"uname -r", "5.15"},
		{"uname -a", "mockhost"},
	}

	for _, tt := range tests {
		stdout, _, code, err := client.Exec(tt.cmd)
		require.NoError(t, err, "command: %s", tt.cmd)
		assert.Equal(t, 0, code)
		assert.Contains(t, string(stdout), tt.contains, "command: %s", tt.cmd)
	}
}
