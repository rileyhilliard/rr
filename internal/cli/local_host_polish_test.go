package cli

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pretty task summary names the host ("dev"), not the connection alias
// ("local"), the same way rr run does.
func TestRunTask_LocalHostSummaryNamesHost(t *testing.T) {
	projectDir, _ := writeLocalHostConfigs(t)
	t.Chdir(projectDir)

	prettyMode = true
	t.Cleanup(func() { prettyMode = false })

	var exitCode int
	var err error
	out := captureStdout(t, func() {
		captureStderr(t, func() {
			exitCode, err = RunTask(TaskOptions{TaskName: "check"})
		})
	})
	require.NoError(t, err)
	require.Equal(t, 0, exitCode, out)

	assert.Contains(t, out, "Task 'check' completed on dev ")
	assert.NotContains(t, out, "completed on local")
}

func TestRenderFailureHelp_LocalHostWording(t *testing.T) {
	tests := []struct {
		name      string
		localHost bool
		want      []string
		notWant   []string
	}{
		{
			name:      "local host",
			localHost: true,
			want:      []string{"Check the command output above and your local environment", "rr doctor"},
			notWant:   []string{"ssh ", "remote logs"},
		},
		{
			name:      "remote host",
			localHost: false,
			want:      []string{"ssh box", "Check remote logs or environment", "rr doctor"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				renderFailureHelp(3, "make test", "box", tt.localHost)
			})
			for _, w := range tt.want {
				assert.Contains(t, out, w)
			}
			for _, w := range tt.notWant {
				assert.NotContains(t, out, w)
			}
		})
	}
}

// rr setup on a local host returns before any key discovery or prompt.
func TestSetup_LocalHostRejected(t *testing.T) {
	writeLocalHostConfigs(t)

	err := Setup(SetupOptions{Host: "dev"})
	require.Error(t, err)
	assert.True(t, errors.IsCode(err, errors.ErrConfig), "got %v", err)
	assert.Contains(t, err.Error(), "'dev' is a local host; there's no SSH to set up")

	var rrErr *errors.Error
	require.ErrorAs(t, err, &rrErr)
	assert.Contains(t, rrErr.Suggestion, "rr doctor")
}
