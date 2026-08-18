package wol

import (
	"fmt"
	"time"
)

// SendOffsets returns send times in [0, duration). A 120 second job with a
// 30 second interval sends at 0, 30, 60 and 90 seconds.
func SendOffsets(duration, interval time.Duration) ([]time.Duration, error) {
	if duration <= 0 {
		return nil, fmt.Errorf("duration must be positive")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("interval must be positive")
	}

	var offsets []time.Duration
	for at := time.Duration(0); at < duration; at += interval {
		offsets = append(offsets, at)
	}
	return offsets, nil
}
