package domain

import "time"

// Clock abstracts time so replay and tests can use a virtual, deterministic clock.
type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

// VirtualClock is advanced explicitly. It never observes wall time.
type VirtualClock struct{ T time.Time }

func NewVirtualClock(t time.Time) *VirtualClock { return &VirtualClock{T: t} }
func (c *VirtualClock) Now() time.Time          { return c.T }
func (c *VirtualClock) Advance(d time.Duration) { c.T = c.T.Add(d) }
func (c *VirtualClock) Set(t time.Time)         { c.T = t }
