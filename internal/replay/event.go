package replay

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
)

// Event is the only thing that is ever appended. Sources, derived nodes,
// judgments, decisions and conflict resolutions are all projections of the
// event stream, so any past state can be rebuilt by truncating replay.
type Event struct {
	ID         string          `json:"id"`
	ProjectID  string          `json:"project_id"`
	Seq        int64           `json:"seq"`
	Type       string          `json:"type"`
	VirtualTS  string          `json:"virtual_ts"`
	Actor      string          `json:"actor,omitempty"`
	Payload    json.RawMessage `json:"payload"`
	Undone     bool            `json:"undone,omitempty"`
}

// Event types.
const (
	EvProjectCreated = "project_created"
	EvSourceCommit   = "source_commit"   // new immutable version of one stratum
	EvImport         = "import_batch"    // one import attempt (may partially fail)
	EvDecision       = "decision"        // accept/reject/adjust a candidate
	EvConflict       = "conflict"        // mutually exclusive decisions detected
	EvResolution     = "conflict_resolution"
	EvUndo           = "undo"            // marks another event undone
	EvNote           = "note"
)

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}
