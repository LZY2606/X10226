package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Hash16 returns a stable, lowercase 16-char hex digest of the canonical JSON of
// v. Identity is content-derived so it survives export/delete/import.
func Hash16(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// ChecksumSHA256 returns a full lowercase hex sha256 of raw bytes.
func ChecksumSHA256(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// StableID builds a content identifier "<prefix>_<hash>" from canonical
// key/value pairs. Deterministic regardless of map iteration order.
func StableID(prefix string, parts ...string) string {
	sorted := append([]string(nil), parts...)
	sort.Strings(sorted)
	sum := sha256.Sum256([]byte(strings.Join(sorted, "|")))
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(sum[:])[:14])
}
