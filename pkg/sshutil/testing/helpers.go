package testing

// WithFiles pre-populates the mock filesystem with files.
// Keys are paths, values are file contents.
func WithFiles(client *MockClient, files map[string]string) {
	for path, content := range files {
		_ = client.GetFS().WriteFile(path, []byte(content))
	}
}

// WithDirs pre-populates the mock filesystem with directories.
func WithDirs(client *MockClient, dirs []string) {
	for _, dir := range dirs {
		_ = client.GetFS().MkdirAll(dir)
	}
}
