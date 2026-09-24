// Package clock provides a virtual-time clock so tests never depend on
// wall-clock time.
package clock

import "time"

// Clock is a source of time. Implementations must be safe for the
// single-threaded usage of this application.
type Clock interface {
	Now() time.Time
}

// Fixed is a deterministic clock that advances by Step on every Now call.
type Fixed struct {
	T    time.Time
	Step time.Duration
}

// NewFixed returns a Fixed clock starting at t, advancing one second per read.
func NewFixed(t time.Time) *Fixed {
	return &Fixed{T: t, Step: time.Second}
}

// Now returns the current virtual time and advances the clock.
func (f *Fixed) Now() time.Time {
	t := f.T
	f.T = f.T.Add(f.Step)
	return t
}

// System reads the real wall clock. Only used by the server entrypoint.
type System struct{}

// Now returns time.Now().UTC().
func (System) Now() time.Time { return time.Now().UTC() }
