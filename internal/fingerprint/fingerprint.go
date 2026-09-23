// Package fingerprint builds stable content hashes and canonical parameter
// fingerprints. All hashing uses SHA-256 hex so identities survive export,
// database deletion and re-import on any platform.
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// SHA256 returns the lowercase hex SHA-256 of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Short returns the first 12 hex chars of a full hash, prefixed for display.
func Short(kind, full string) string {
	if len(full) > 12 {
		return kind + "_" + full[:12]
	}
	return kind + "_" + full
}

// CanonicalJSON round-trips v through encoding/json with sorted keys so the
// fingerprint is independent of map iteration order.
func CanonicalJSON(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	return json.Marshal(generic)
}

// Params returns "hex12" style fingerprint of canonical parameter JSON.
func Params(v any) (string, []byte, error) {
	canon, err := CanonicalJSON(v)
	if err != nil {
		return "", nil, err
	}
	return SHA256(canon)[:12], canon, nil
}

// ChainHash hashes the previous chain hash together with this event body,
// forming a tamper-evident log.
func ChainHash(prev string, body []byte) string {
	return SHA256([]byte(fmt.Sprintf("%s\n%s", prev, body)))
}
