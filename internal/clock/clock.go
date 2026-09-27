// Package clock provides a virtual-time abstraction so that tests can run
// with fixed, deterministic timestamps.
package clock

import "time"

// Clock returns a monotonically increasing logical timestamp (unix seconds).
type Clock interface {
	Now() int64
}

// Real uses the wall clock.
type Real struct{}

func (Real) Now() int64 { return time.Now().Unix() }

// Fake is a deterministic virtual clock for tests. It starts at a fixed
// epoch and advances only when told to (or by a fixed step on each read).
type Fake struct {
	T    int64
	Step int64
}

// NewFake returns a Fake clock anchored at a fixed instant so every test
// run observes identical timestamps.
func NewFake() *Fake { return &Fake{T: 1700000000, Step: 1} }

func (f *Fake) Now() int64 {
	t := f.T
	f.T += f.Step
	return t
}
