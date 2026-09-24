package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func StableHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:16])
}

func FingerprintParams(p Params) string {
	p.Fingerprint = ""
	data, _ := json.Marshal(p)
	return "sha256:" + StableHash(string(data))
}

func F(v float64) *float64 { return &v }

func FormatFloat(v float64) string {
	return strconv.FormatFloat(round4(v), 'f', -1, 64)
}

func round4(v float64) float64 {
	parsed, _ := strconv.ParseFloat(fmt.Sprintf("%.4f", v), 64)
	return parsed
}

func JoinID(parts ...string) string {
	return strings.Join(parts, ":")
}
