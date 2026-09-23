// Package clock provides a deterministic clock for tests (fixed seed base
// time, advanced logically) and a real clock for the HTTP server.
package clock

import (
	"strconv"
	"sync"
	"time"
)

// FixedBase is the virtual timeline origin for seeded replays.
const FixedBase = "2026-01-01T00:00:00Z"

// Clock yields timestamps for events.
type Clock interface {
	Now() string
	Tick() string
}

// Virtual is deterministic: each Tick advances one logical second.
type Virtual struct {
	mu  sync.Mutex
	base time.Time
	n   int64
}

// NewVirtual builds a virtual clock from a seed. Different seeds shift the
// base by whole days, keeping the run reproducible.
func NewVirtual(seed int64) *Virtual {
	base, _ := time.Parse(time.RFC3339, FixedBase)
	return &Virtual{base: base.AddDate(0, 0, int(seed%7)), n: 0}
}

func (v *Virtual) Now() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.base.Add(time.Duration(v.n) * time.Second).UTC().Format(time.RFC3339)
}

// Tick advances the logical clock and returns the new timestamp.
func (v *Virtual) Tick() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.n++
	return v.base.Add(time.Duration(v.n) * time.Second).UTC().Format(time.RFC3339)
}

// Real uses wall time but still satisfies Tick semantics.
type Real struct{ mu sync.Mutex; n int64 }

func (r *Real) Now() string { return time.Now().UTC().Format(time.RFC3339) }
func (r *Real) Tick() string {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return time.Now().UTC().Format(time.RFC3339 + "." + strconv.FormatInt(r.n, 10))
}
