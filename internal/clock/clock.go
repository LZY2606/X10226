// Package clock defines the time source. Production code uses System; tests
// inject a fixed virtual clock so every timestamp is deterministic.
package clock

import "time"

type Clock interface {
	Now() time.Time
}

type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Virtual is a manually advanced clock.
type Virtual struct{ T time.Time }

func NewVirtual(t time.Time) *Virtual { return &Virtual{T: t} }

func (v *Virtual) Now() time.Time { return v.T }

func (v *Virtual) Advance(d time.Duration) { v.T = v.T.Add(d) }
