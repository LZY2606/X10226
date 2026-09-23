package store

import "time"

// Clock is injectable so tests run on virtual time.
type Clock interface {
	NowISO() string
}

type realClock struct{}

func (realClock) NowISO() string { return time.Now().UTC().Format(time.RFC3339) }

// NewRealClock returns wall-clock UTC time.
func NewRealClock() Clock { return realClock{} }

// VirtualClock returns a fixed, controllable timestamp source.
type VirtualClock struct {
	T time.Time
}

func NewVirtualClock(t time.Time) *VirtualClock { return &VirtualClock{T: t} }
func (c *VirtualClock) NowISO() string          { return c.T.UTC().Format(time.RFC3339) }
func (c *VirtualClock) Advance(d time.Duration) { c.T = c.T.Add(d) }
