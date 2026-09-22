// Package clock provides a virtual, deterministic clock.
// All timestamps in the system are logical ticks, never wall time,
// so tests and replays are fully deterministic.
package clock

// Clock is a monotonic logical clock.
type Clock struct{ now int64 }

// New returns a Clock starting at tick 0.
func New() *Clock { return &Clock{} }

// Now advances the clock and returns the new tick.
func (c *Clock) Now() int64 { c.now++; return c.now }

// Peek returns the current tick without advancing.
func (c *Clock) Peek() int64 { return c.now }

// Sync raises the clock to at least v (used after import).
func (c *Clock) Sync(v int64) {
	if v > c.now {
		c.now = v
	}
}
