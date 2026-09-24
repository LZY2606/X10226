// Package clock defines time and identity sources so that tests can pin both
// to a virtual clock and a fixed seed while production code uses the wall
// clock.
package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

// Clock returns deterministic timestamps when pinned (replays, tests) and wall
// time otherwise.
type Clock interface {
	Now() time.Time
}

type wallClock struct{}

func (wallClock) Now() time.Time { return time.Now().UTC() }

// Wall returns the production clock.
func Wall() Clock { return wallClock{} }

// Virtual is a manually-advanced clock starting at a fixed epoch.
type Virtual struct {
	mu  sync.Mutex
	now time.Time
}

// NewVirtual creates a virtual clock at 2026-01-01T00:00:00Z.
func NewVirtual() *Virtual {
	t, _ := time.Parse(time.RFC3339, "2026-01-01T00:00:00Z")
	return &Virtual{now: t}
}

func (v *Virtual) Now() time.Time {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.now
}

// Advance moves the clock forward.
func (v *Virtual) Advance(d time.Duration) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = v.now.Add(d)
}

// SetAt pins the clock to a fixed time.
func (v *Virtual) SetAt(t time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.now = t.UTC()
}

// IDSource mints short stable identifiers. Pinned sources derive every id from
// the fed key with SHA-256 (replays get identical ids); the wall source uses a
// seeded RNG suffix plus the key for human readability.
type IDSource interface {
	ID(prefix, key string) string
}

// ContentID returns a deterministic, stable id derived from key.
func ContentID(prefix, key string) string {
	sum := sha256.Sum256([]byte(key))
	return prefix + "_" + hex.EncodeToString(sum[:8])
}

// HashKey exposes the short hash used in fingerprints.
func HashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

type contentIDs struct{}

func (contentIDs) ID(prefix, key string) string { return ContentID(prefix, key) }

// ContentIDSource should be used for replay so identifiers survive export /
// import / delete-database cycles.
func ContentIDSource() IDSource { return contentIDs{} }

type randomIDs struct {
	mu *sync.Mutex
	r  *rand.Rand
}

// RandomIDSource returns a non-repeating id source; seed is fixed in tests.
func RandomIDSource(seed int64) IDSource {
	return randomIDs{mu: &sync.Mutex{}, r: rand.New(rand.NewSource(seed))}
}

func (s randomIDs) ID(prefix, key string) string {
	s.mu.Lock()
	n := s.r.Uint64()
	s.mu.Unlock()
	return prefix + "_" + strconv.FormatUint(n, 36) + "_" + HashKey(key)[:4]
}
