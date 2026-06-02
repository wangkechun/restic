package profile

import (
	"fmt"
	"os"
	"sync"
	"time"
)

var (
	enabled bool
	begin   time.Time
	last    time.Time
	once    sync.Once
)

func initEnabled() {
	once.Do(func() {
		enabled = os.Getenv("RESTIC_PROFILE") != ""
		if enabled {
			begin = time.Now()
			last = begin
			fmt.Fprintf(os.Stderr, "RESTIC_PROFILE === backup timing profile (set RESTIC_PROFILE=1) ===\n")
		}
	})
}

// Enabled reports whether RESTIC_PROFILE is set.
func Enabled() bool {
	initEnabled()
	return enabled
}

// Mark records elapsed time since the previous mark.
func Mark(label string) {
	initEnabled()
	if !enabled {
		return
	}
	now := time.Now()
	fmt.Fprintf(os.Stderr, "RESTIC_PROFILE %-36s +%8v  total %8v\n",
		label+":", now.Sub(last).Round(time.Microsecond), now.Sub(begin).Round(time.Microsecond))
	last = now
}

// Reset restarts the timer (e.g. at command entry).
func Reset() {
	initEnabled()
	if !enabled {
		return
	}
	begin = time.Now()
	last = begin
}
