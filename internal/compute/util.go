package compute

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strconv"
	"strings"
)

func ftoa(v float64) string  { return strconv.FormatFloat(v, 'g', -1, 64) }
func btoa(v bool) string     { if v { return "1" }; return "0" }

func shortHash(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return "fp" + hex.EncodeToString(h[:])[:12]
}

// ID builds a stable output identity from a label and contributing tokens.
func ID(label string, tokens ...string) string {
	h := sha256.Sum256([]byte(label + "::" + strings.Join(tokens, "|")))
	return "der_" + label + "_" + hex.EncodeToString(h[:6])
}

func round(v, digits float64) float64 {
	p := math.Pow(10, digits)
	return math.Round(v*p) / p
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// interp1 linearly interpolates y at x given sorted xs, ys. It must never be
// called across a declared gap; callers split segments first.
func interp1(xs, ys []float64, x float64) float64 {
	if x <= xs[0] {
		return ys[0]
	}
	if x >= xs[len(xs)-1] {
		return ys[len(ys)-1]
	}
	lo, hi := 0, len(xs)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if xs[mid] <= x {
			lo = mid
		} else {
			hi = mid
		}
	}
	f := (x - xs[lo]) / (xs[hi] - xs[lo])
	return ys[lo] + f*(ys[hi]-ys[lo])
}
