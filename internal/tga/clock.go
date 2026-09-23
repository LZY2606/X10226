package tga

import "time"

// Clock abstracts wall-clock access so tests can run on a fixed,
// deterministic virtual clock.
type Clock func() time.Time

// SystemClock returns the real wall clock.
func SystemClock() Clock { return time.Now }

// FixedClock returns a clock frozen at t.
func FixedClock(t time.Time) Clock {
	return func() time.Time { return t }
}

// AdvancingClock returns a virtual clock that steps by step every call.
func AdvancingClock(start time.Time, step time.Duration) Clock {
	cur := start
	return func() time.Time {
		t := cur
		cur = cur.Add(step)
		return t
	}
}
