package domain

import "strconv"

func strconvAppendInt(b []byte, n int64) []byte {
	return strconv.AppendInt(b, n, 10)
}

// appendFloat uses a compact, deterministic representation preserving
// full round-trip precision ('g', -1) without trailing zeros.
func appendFloat(b []byte, f float64) []byte {
	return strconv.AppendFloat(b, f, 'g', -1, 64)
}
