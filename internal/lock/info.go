package lock

import (
	"encoding/json"
	"os"
	"time"
)

// LockInfo contains metadata about who holds a lock.
type LockInfo struct {
	User         string    `json:"user"`
	Hostname     string    `json:"hostname"`
	Started      time.Time `json:"started"`
	PID          int       `json:"pid"`
	Command      string    `json:"command,omitempty"`
	MachineToken string    `json:"machine_token,omitempty"`
	// JobPGID is the process group of the job a local host is running under
	// this lock, set once the job starts (see Lock.SetJobPGID). The job runs
	// in its own session, so it outlives a killed rr; while the group exists
	// the holder counts as alive. 0 when unknown (a remote host, or a lock
	// written before this field existed).
	JobPGID int `json:"job_pgid,omitempty"`
}

// NewLockInfo creates a LockInfo with the current user, hostname, time, PID, and command.
func NewLockInfo(command string) (*LockInfo, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	return &LockInfo{
		User:         currentUser(),
		Hostname:     hostname,
		Started:      time.Now(),
		PID:          os.Getpid(),
		Command:      command,
		MachineToken: machineToken(),
	}, nil
}

func currentUser() string {
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}
	return user
}

// Age returns how long ago the lock was acquired.
func (i *LockInfo) Age() time.Duration {
	return time.Since(i.Started)
}

// SameMachine reports whether the lock was acquired from this machine.
// Machine tokens are compared when both sides have one; otherwise it falls
// back to matching hostname AND user, since default hostnames (e.g.
// "MacBook-Pro.local") collide across machines.
func (i *LockInfo) SameMachine() bool {
	if i.MachineToken != "" {
		if tok := machineToken(); tok != "" {
			return i.MachineToken == tok
		}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return false
	}
	return i.Hostname == hostname && i.User == currentUser()
}

// IsDeadLocalHolder reports whether the lock is held by a process on this
// machine that is no longer running, and whose job (JobPGID, when recorded)
// has stopped too. Such locks are safe to remove immediately instead of
// waiting for the staleness threshold. A SIGKILLed rr leaves its local job
// running in its own session; the lock stays held until that job is gone.
func (i *LockInfo) IsDeadLocalHolder() bool {
	if i.PID <= 0 || i.PID == os.Getpid() {
		return false
	}
	if !i.SameMachine() {
		return false
	}
	if processAlive(i.PID) {
		return false
	}
	return i.JobPGID <= 0 || !processGroupAlive(i.JobPGID)
}

// Describe returns a human-readable description with command, holder, age,
// and whether the holder is on this machine.
func (i *LockInfo) Describe() string {
	desc := ""
	if i.Command != "" {
		desc = "'" + i.Command + "' held by "
	}
	desc += i.User + "@" + i.Hostname + " (pid " + itoa(i.PID)
	if !i.Started.IsZero() {
		desc += ", started " + i.Age().Truncate(time.Second).String() + " ago"
	}
	if i.SameMachine() {
		desc += ", this machine"
	}
	return desc + ")"
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
