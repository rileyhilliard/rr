package doctor

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/host"
	sshtesting "github.com/rileyhilliard/rr/pkg/sshutil/testing"
	"github.com/stretchr/testify/assert"
)

func TestRequirementsCheck_Run(t *testing.T) {
	tests := []struct {
		name            string
		reqs            []string
		missing         []string
		wantStatus      CheckStatus
		wantFixable     bool
		wantSuggestions []string
	}{
		{
			name:       "all satisfied passes",
			reqs:       []string{"make"},
			wantStatus: StatusPass,
		},
		{
			// rr run refuses to start on a missing requirement, so doctor fails too.
			name:            "missing installable tool fails and points to rr provision",
			reqs:            []string{"make", "jq"},
			missing:         []string{"jq"},
			wantStatus:      StatusFail,
			wantFixable:     true,
			wantSuggestions: []string{"rr provision --host box"},
		},
		{
			name:            "missing tool without installer fails",
			reqs:            []string{"not-a-real-tool"},
			missing:         []string{"not-a-real-tool"},
			wantStatus:      StatusFail,
			wantFixable:     false,
			wantSuggestions: []string{"Install the missing tools on box"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := sshtesting.NewMockClient("box")
			for _, tool := range tt.reqs {
				client.SetCommandResponse("command -v "+tool, sshtesting.CommandResponse{Stdout: []byte("/usr/bin/" + tool + "\n")})
			}
			for _, tool := range tt.missing {
				client.SetCommandResponse("command -v "+tool, sshtesting.CommandResponse{ExitCode: 1})
			}

			check := &RequirementsCheck{
				HostName:     "box",
				Conn:         &host.Connection{Name: "box", Client: client},
				Requirements: tt.reqs,
			}
			result := check.Run()

			assert.Equal(t, tt.wantStatus, result.Status, result.Message)
			assert.Equal(t, tt.wantFixable, result.Fixable)
			for _, s := range tt.wantSuggestions {
				assert.Contains(t, result.Suggestion, s)
			}
		})
	}
}
