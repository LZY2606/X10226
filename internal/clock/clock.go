// Package clock defines the time source so tests can use virtual time.
package clock

import (
	"sync"
	"time"
)

// Clock returns wall-clock-like instants; production is System, tests use Fake.
type Clock interface {
	Now() time.Time
}

// System is the real wall clock.
type System struct{}

func (System) Now() time.Time { return time.Now().UTC() }

// Fake is a deterministic, manually advanced clock started at a fixed epoch.
type Fake struct {
	mu  sync.Mutex
	now time.Time
	// Epoch is the fixed virtual epoch used by all tests.
	Epoch time.Time
}

const FakeEpoch = "2024-01-01T00:00:00Z"

// NewFake returns a virtual clock at the fixed epoch.
func NewFake() *Fake {
	t, err := time.Parse(time.RFC3339, FakeEpoch)
	if err != nil {
		panic(err)
	}
	return &Fake{now: t, Epoch: t}
}

func (f *Fake) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

// Advance moves virtual time by d.
func (f *Fake) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// Set moves virtual time to t.
func (f *Fake) Set(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = t
}
