package app

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"sync"
	"time"
)

type Clock interface{ Now() time.Time }

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now().UTC() }

type FixedClock struct{ Value time.Time }

func (clock FixedClock) Now() time.Time                  { return clock.Value }
func (clock *FixedClock) Advance(duration time.Duration) { clock.Value = clock.Value.Add(duration) }

type SequencedID struct {
	mu    sync.Mutex
	clock Clock
	seed  uint64
	next  uint64
}

func NewSequencedID(clock Clock, seed uint64) *SequencedID {
	return &SequencedID{clock: clock, seed: seed, next: 1}
}

func (generator *SequencedID) New(kind, projectID string, parts ...any) string {
	generator.mu.Lock()
	defer generator.mu.Unlock()
	sequence := generator.next
	generator.next++
	now := generator.clock.Now().UTC().Format("20060102T150405.000000000")
	h := sha256.New()
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(projectID))
	h.Write([]byte{0})
	h.Write([]byte(now))
	h.Write([]byte{0})
	var buffer [8]byte
	binary.BigEndian.PutUint64(buffer[:], generator.seed^sequence)
	h.Write(buffer[:])
	for _, part := range parts {
		h.Write([]byte{0})
		h.Write([]byte(fmt.Sprint(part)))
	}
	return kind + "_" + hex.EncodeToString(h.Sum(nil))[:20]
}
