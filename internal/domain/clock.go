package domain

import "time"

// Clock abstracts time so tests run on a fixed virtual clock and the server
// uses wall time.
type Clock interface {
	Now() time.Time
}

type WallClock struct{}

func (WallClock) Now() time.Time { return time.Now().UTC() }

// FakeClock advances deterministically; tests seed it once.
type FakeClock struct {
	t time.Time
}

func NewFakeClock(start time.Time) *FakeClock { return &FakeClock{t: start.UTC()} }
func (c *FakeClock) Now() time.Time           { return c.t }
func (c *FakeClock) Advance(d time.Duration)  { c.t = c.t.Add(d) }
