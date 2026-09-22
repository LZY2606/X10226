// Package identity produces stable, deterministic identifiers for projects,
// events, layers, parameter sets, derived artifacts and individual display
// values. Determinism is what lets an export survive a local database
// deletion + re-import without renaming anything in the review UI.
package identity

import (
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

const crockford = "0123456789abcdefghjkmnpqrstvwxyz" // 32 symbols, Crockford alphabet

func encode(b []byte, n int) string {
	out := make([]byte, n)
	var acc uint64
	var bits uint
	bi := 0
	for _, x := range b {
		acc = (acc << 8) | uint64(x)
		bits += 8
		for bits >= 5 && bi < n {
			bits -= 5
			out[bi] = crockford[int((acc>>bits)&31)]
			bi++
		}
	}
	if bi < n {
		out[bi] = crockford[int((acc<<(5-bits))&31)]
		bi++
	}
	for i := bi; i < n; i++ {
		out[i] = '0'
	}
	return string(out)
}

// Hash returns a prefix + 12-symbol Crockford-base32 digest of the
// canonicalised concatenation of parts. Parts are length-framed so that
// ["ab","c"] cannot collide with ["a","bc"].
func Hash(prefix string, parts ...string) string {
	h := sha256.New()
	var lb [4]byte
	for _, p := range parts {
		binary.BigEndian.PutUint32(lb[:], uint32(len(p)))
		h.Write(lb[:])
		h.Write([]byte(p))
	}
	return prefix + encode(h.Sum(nil), 12)
}

// Join folds several stable ids into one, preserving order.
func Join(prefix string, ids ...string) string {
	return Hash(prefix, append([]string{"join"}, ids...)...)
}

// SplitRef splits a prefixed stable id such as "evt_01....".
func SplitRef(id, prefix string) string {
	return strings.TrimPrefix(id, prefix)
}
