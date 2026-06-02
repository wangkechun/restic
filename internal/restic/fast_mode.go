package restic

import "time"

// EnableFastMode disables lock retry delays for local low-latency use.
func EnableFastMode() {
	waitBeforeLockCheck = 0
	initialWaitBetweenLockRetries = 0
}

// FastModeLockDelay returns the current lock check delay (for profiling).
func FastModeLockDelay() time.Duration {
	return waitBeforeLockCheck
}
