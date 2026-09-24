package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// Deterministic identifiers. They are content/position based, not random, so
// export -> delete -> import preserves identity, order and source relations.

func layerStableID(prefix, logical string) string {
	sum := sha256.Sum256([]byte(prefix + ":" + logical))
	return prefix + "-" + hex.EncodeToString(sum[:6])
}

func recordID(importID, logical string, i int) string {
	sum := sha256.Sum256([]byte(importID + ":" + logical + ":" + fmt.Sprint(i)))
	return "rec-" + hex.EncodeToString(sum[:6])
}

func batchID(seed int64, virtual time.Time) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("batch:%d:%d", seed, virtual.UnixNano())))
	return "batch-" + hex.EncodeToString(sum[:6])
}

func eventID(kind string, seq int64) string {
	return fmt.Sprintf("evt-%06d-%s", seq, kind)
}

func decisionID(seq int64) string  { return fmt.Sprintf("dec-%06d", seq) }
func conflictID(seq int64) string  { return fmt.Sprintf("cnf-%06d", seq) }

// logicalID extracts the stable logical key of a record (fixed in the case
// package), defaulting to the record name.
func logicalID(rec map[string]any) string {
	if v, ok := rec["logical_id"].(string); ok && v != "" {
		return v
	}
	if v, ok := rec["name"].(string); ok && v != "" {
		return v
	}
	return "unnamed"
}
