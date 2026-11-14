package exec

import "time"

// SetLocalInterruptGrace sets the grace period a cancelled local command gets
// before it's killed, and returns a func that restores the old value.
func SetLocalInterruptGrace(d time.Duration) func() {
	old := localInterruptGrace
	localInterruptGrace = d
	return func() { localInterruptGrace = old }
}
