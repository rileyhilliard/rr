package lock

import (
	"encoding/json"
	"os"
	"time"
)

// LockInfo contains metadata about who holds a lock.
type LockInfo struct {
	User     string    `json:"user"`
	Hostname string    `json:"hostname"`
	Started  time.Time `json:"started"`
	PID      int       `json:"pid"`
	Command  string    `json:"command,omitempty"`
}

// NewLockInfo creates a LockInfo with the current user, hostname, time, PID, and command.
func NewLockInfo(command string) (*LockInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}

	return &LockInfo{
		User:     user,
		Hostname: hostname,
		Started:  time.Now(),
		PID:      os.Getpid(),
		Command:  command,
	}, nil
}

// Age returns how long ago the lock was acquired.
func (i *LockInfo) Age() time.Duration {
	return time.Since(i.Started)
}

// Marshal serializes the LockInfo to JSON.
func (i *LockInfo) Marshal() ([]byte, error) {
	return json.Marshal(i)
}

// ParseLockInfo deserializes JSON data into a LockInfo.
func ParseLockInfo(data []byte) (*LockInfo, error) {
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// String returns a human-readable description of who holds the lock.
func (i *LockInfo) String() string {
	return i.User + "@" + i.Hostname + " (pid " + itoa(i.PID) + ")"
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
