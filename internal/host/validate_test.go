package host

import (
	"testing"

	"github.com/rileyhilliard/rr/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateConnection(t *testing.T) {
	tests := []struct {
		name    string
		conn    *Connection
		wantErr bool
		errCode string
	}{
		{
			name:    "nil connection returns error",
			conn:    nil,
			wantErr: true,
			errCode: errors.ErrSSH,
		},
		{
			name: "connection with nil client returns error",
			conn: &Connection{
				Name:    "test",
				Client:  nil,
				IsLocal: false,
			},
			wantErr: true,
			errCode: errors.ErrSSH,
		},
		{
			name: "local connection without client is valid",
			conn: &Connection{
				Name:    "local",
				Client:  nil,
				IsLocal: true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConnection(tt.conn)
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errors.IsCode(err, tt.errCode))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateConnectionForSync(t *testing.T) {
	tests := []struct {
		name    string
		conn    *Connection
		wantErr bool
	}{
		{
			name:    "nil connection",
			conn:    nil,
			wantErr: true,
		},
		{
			name: "nil client",
			conn: &Connection{
				Name:   "test",
				Client: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConnectionForSync(tt.conn)
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errors.IsCode(err, errors.ErrSync))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateConnectionForLock(t *testing.T) {
	tests := []struct {
		name    string
		conn    *Connection
		wantErr bool
	}{
		{
			name:    "nil connection",
			conn:    nil,
			wantErr: true,
		},
		{
			name: "nil client",
			conn: &Connection{
				Name:   "test",
				Client: nil,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateConnectionForLock(tt.conn)
			if tt.wantErr {
				require.Error(t, err)
				assert.True(t, errors.IsCode(err, errors.ErrLock))
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHasClient(t *testing.T) {
	tests := []struct {
		name string
		conn *Connection
		want bool
	}{
		{
			name: "nil connection",
			conn: nil,
			want: false,
		},
		{
			name: "nil client",
			conn: &Connection{
				Name:   "test",
				Client: nil,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasClient(tt.conn)
			assert.Equal(t, tt.want, got)
		})
	}
}
