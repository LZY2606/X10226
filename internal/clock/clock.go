// Package clock defines the time abstraction used by the whole system so that
// every replay and automated test can run on a fixed, virtual timeline.
package clock

import (
	"sync"
	"time"
)

// Clock is the narrow time interface the application depends on.
type Clock interface {
	Now() time.Time
}

// System returns wall-clock time.
type System struct{}

// Now returns the current wall-clock time at second precision.
func (System) Now() time.Time { return time.Now().UTC().Truncate(time.Second) }

// Fake is a deterministic, controllable clock for tests and replays.
type Fake struct {
	mu  sync.Mutex
	now time.Time
}

// NewFake starts the virtual clock at the fixed seed time 2026-01-01T00:00:00Z.
func NewFake() *Fake {
	t, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	return &Fake{now: t}
}

// Now returns the current virtual time.
func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves the virtual clock forward by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}
