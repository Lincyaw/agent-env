package main

import (
	"fmt"
	"time"
)

func allocationTimeoutSecondsFromDuration(timeout time.Duration) (*int, error) {
	if timeout < 0 {
		return nil, fmt.Errorf("--wait-timeout cannot be negative")
	}
	seconds := 0
	if timeout > 0 {
		seconds = int((timeout + time.Second - 1) / time.Second)
	}
	return &seconds, nil
}

func clientTimeoutForAllocation(seconds *int) time.Duration {
	if seconds == nil {
		return 0
	}
	if *seconds <= 0 {
		return 0
	}
	return time.Duration(*seconds)*time.Second + 30*time.Second
}
