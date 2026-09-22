package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Event types of the append-only log.
const (
	EvProjectCreated = "project_created"
	EvRawSubmitted   = "raw_submitted"
	EvParamsRevised  = "params_revised"
	EvDerived        = "derived_result"
	EvDecision       = "decision"
	EvUndo           = "decision_undone"
	EvAnnotation     = "annotation"
	EvExported       = "project_exported"
	EvImported       = "project_imported"
)

// Event is the unit of history. Payload is one of RawDoc/Parameters/
// Result/Decision/export metadata, stored as raw JSON so replays are exact.
type Event struct {
	Seq        int64     `json:"seq"`
	ID         string    `json:"id"`
	ProjectID  string    `json:"project_id"`
	Type       string    `json:"type"`
	OccurredAt time.Time `json:"occurred_at"`
	Actor      string    `json:"actor,omitempty"`
	Payload    []byte    `json:"payload"`
	PrevHash   string    `json:"prev_hash"`
	Hash       string    `json:"hash"`
}

// Seal computes this event's content hash and stores it.
func (e *Event) Seal() {
	h := sha256.New()
	h.Write([]byte(e.PrevHash))
	h.Write([]byte{0})
	enc := json.NewEncoder(h)
	_ = enc.Encode(struct {
		Seq        int64     `json:"seq"`
		ID         string    `json:"id"`
		ProjectID  string    `json:"project_id"`
		Type       string    `json:"type"`
		OccurredAt time.Time `json:"occurred_at"`
		Actor      string    `json:"actor"`
		Payload    []byte    `json:"payload"`
	}{e.Seq, e.ID, e.ProjectID, e.Type, e.OccurredAt, e.Actor, e.Payload})
	e.Hash = hex.EncodeToString(h.Sum(nil))
}

// VerifyChain checks hash linkage of an ordered event slice.
func VerifyChain(evs []Event) error {
	prev := ""
	for i := range evs {
		got := evs[i].Hash
		evs[i].Hash = ""
		evs[i].Seal()
		want := evs[i].Hash
		evs[i].Hash = got
		if got != want {
			return &ChainError{Seq: evs[i].Seq, Want: want, Got: got}
		}
		if evs[i].PrevHash != prev {
			return &ChainError{Seq: evs[i].Seq, Want: prev, Got: evs[i].PrevHash}
		}
		prev = got
	}
	return nil
}

// ChainError describes a broken hash chain.
type ChainError struct {
	Seq  int64
	Want string
	Got  string
}

func (c *ChainError) Error() string {
	return "event hash chain broken at seq " + itoa(c.Seq)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
