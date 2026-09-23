package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func strconvParseFloat(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

func itoa(v int) string { return strconv.Itoa(v) }

func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

func hashText(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
