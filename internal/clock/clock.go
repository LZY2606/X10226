// Package clock defines time sources so replays can use a virtual clock.
package clock

import "time"

type Clock interface {
	Now() time.Time
}

type Real struct{}

func (Real) Now() time.Time { return time.Now().UTC() }

// Virtual is a deterministic clock: Now() starts at Base and advances by Step
// on every tick (each call to Now advances). Tests use a fixed base time.
type Virtual struct {
	Base time.Time
	Step time.Duration
	n    int
}

func NewVirtual(base time.Time, step time.Duration) *Virtual {
	return &Virtual{Base: base, Step: step}
}

func (v *Virtual) Now() time.Time {
	t := v.Base.Add(time.Duration(v.n) * v.Step)
	v.n++
	return t
}

// Freeze returns a virtual clock that always returns the same time until
// Advanced explicitly.
type Freeze struct{ T time.Time }

func (f *Freeze) Now() time.Time { return f.T }
func (f *Freeze) Advance(d time.Duration) { f.T = f.T.Add(d) }
