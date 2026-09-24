package analyzer

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// digest returns a stable 12-hex content hash over canonical JSON of v.
func digest(parts ...interface{}) string {
	h := sha1.New()
	enc := json.NewEncoder(h)
	for _, p := range parts {
		if err := enc.Encode(p); err != nil {
			h.Write([]byte(fmt.Sprintf("%v", p)))
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// idWith prefixes a content digest with a domain tag.
func idWith(prefix string, parts ...interface{}) string {
	return prefix + "_" + digest(parts...)
}
