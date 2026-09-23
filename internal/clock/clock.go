// Package clock provides a fixed, deterministic virtual clock for tests and
// replay, and a real clock for interactive use.
package clock

import (
	"strconv"
	"sync"
	"time"
)

// Clock yields wall timestamps and monotonic virtual ticks.
type Clock interface {
	Now() string
	Millis() int64
}

// Real uses the system clock.
type Real struct{}

func (Real) Now() string   { return time.Now().UTC().Format(time.RFC3339Nano) }
func (Real) Millis() int64 { return time.Now().UnixMilli() }

// Virtual advances deterministically; zero value starts at the Unix epoch.
type Virtual struct {
	mu  sync.Mutex
	now time.Time
}

func NewVirtual(start time.Time) *Virtual {
	if start.IsZero() {
		start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	}
	return &Virtual{now: start}
}

// Advance moves the clock and returns the new timestamp.
func (v *Virtual) Advance(d time.Duration) string {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = v.now.Add(d)
	return v.now.Format(time.RFC3339Nano)
}

func (v *Virtual) Now() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now.Format(time.RFC3339Nano)
}

func (v *Virtual) Millis() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now.UnixMilli()
}

// Seeded is a deterministic numeric seed derived from a fixed string.
func Seeded(tag string) int64 {
	var h int64 = 1469598103934665603
	for _, b := range []byte(tag) {
		h ^= int64(b)
		h *= 1099511628211
	}
	if h < 0 {
		h = -h
	}
	if h == 0 {
		h = 1
	}
	// keep it readable in logs
	s := strconv.FormatInt(h%1_000_000, 10)
	n, _ := strconv.ParseInt(s, 10, 64)
	return n + 1
}
