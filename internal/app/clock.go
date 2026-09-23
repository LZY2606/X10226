package app

import "time"

// Clock is injectable so tests run on virtual time.
type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type FixedClock struct{ T time.Time }

func (c FixedClock) Now() time.Time { return c.T }

// StepClock advances by Step on every Now call.
type StepClock struct {
	T    time.Time
	Step time.Duration
}

func (c *StepClock) Now() time.Time {
	t := c.T
	c.T = c.T.Add(c.Step)
	return t
}
