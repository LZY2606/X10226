package domain

import (
	"math"
	"strconv"
	"strings"
)

func parseFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

func roundTo(f float64, digits int) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return f
	}
	p := math.Pow(10, float64(digits))
	return math.Round(f*p) / p
}

// Round returns a canonical Val rounded for stable display and hashing.
func Round(v Val, digits int) Val {
	if v.ConvertOK {
		v.Value = roundTo(v.Value, digits)
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
