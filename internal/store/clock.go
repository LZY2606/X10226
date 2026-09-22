package store

import "time"

// Clock is the injectable time source so tests can use virtual time.
type Clock interface {
	Now() time.Time
}

// SystemClock uses wall-clock time.
type SystemClock struct{}

// Now returns the current wall-clock time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }

// SeqGen makes time-ordered, collision-free identifiers using the project's
// monotonic counter. IDs are time-sortable (Crockford base32 of millis) with a
// per-project counter suffix, so a fresh project with a virtual clock and a
// fixed seed always produces stable identifiers.
type SeqGen struct {
	clock Clock
}

// NewSeqGen returns a sequence generator.
func NewSeqGen(c Clock) *SeqGen {
	if c == nil {
		c = SystemClock{}
	}
	return &SeqGen{clock: c}
}

const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// NewID returns an identifier like "01J...-0007". The counter is supplied by
// the caller (project event counter), keeping IDs deterministic in tests.
func (g *SeqGen) NewID(counter int64) string {
	ms := g.clock.Now().UnixMilli()
	n := ms
	var b [13]byte
	for i := len(b) - 1; i >= 0; i-- {
		b[i] = crockford[n%32]
		n /= 32
	}
	return encodePref(b[:], counter)
}

func encodePref(pref []byte, counter int64) string {
	out := make([]byte, 0, len(pref)+5)
	out = append(out, pref...)
	out = append(out, '-')
	var c [4]byte
	for i := len(c) - 1; i >= 0; i-- {
		c[i] = crockford[counter%32]
		counter /= 32
	}
	out = append(out, c[:]...)
	return string(out)
}
