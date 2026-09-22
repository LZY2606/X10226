// Package clock defines the time source used by the event log. Production code
// uses the wall clock; tests inject a virtual clock whose advance is
// deterministic. Every stored timestamp is RFC3339 UTC so archives replay
// identically on any machine.
package clock

import "time"

type Clock interface {
	Now() time.Time
}

type Wall struct{}

func (Wall) Now() time.Time { return time.Now().UTC() }

// Fake is a deterministic virtual clock. It never reads the host clock.
type Fake struct{ T time.Time }

func NewFake() *Fake {
	return &Fake{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (f *Fake) Now() time.Time { return f.T }

// Advance moves the clock by a fixed, non-random duration and returns the new
// time. Tests drive the whole replay through Advance, not sleeps.
func (f *Fake) Advance(d time.Duration) time.Time {
	f.T = f.T.Add(d)
	return f.T
}
