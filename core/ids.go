package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func StableID(parts ...string) string {
	h := sha256.New()
	for i, part := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(part))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]

}

func PrefixedID(prefix string, parts ...string) string {
	return prefix + "_" + StableID(parts...)
}

func Fingerprint(value any) string {
	return StableID(fmt.Sprintf("%#v", value))
}

func CanonicalFingerprint(lines ...string) string {
	return StableID(strings.Join(lines, "\n"))
}
