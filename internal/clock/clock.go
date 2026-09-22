// Package clock provides time abstraction so tests run on virtual time.
package clock

import "time"

// Clock yields wall time. Production uses System; tests use Fixed/Virtual.
type Clock interface {
	Now() time.Time
}

// System is the real-time clock.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Fixed always returns the same instant.
type Fixed struct{ T time.Time }

func (f Fixed) Now() time.Time { return f.T }

// Virtual advances by Step every call from Start (deterministic in tests).
type Virtual struct {
	Start time.Time
	Step  time.Duration
	n     uint64
}

func (v *Virtual) Now() time.Time {
	t := v.Start.Add(v.Step * time.Duration(v.n))
	v.n++
	return t
}
