package domain

import (
	"math"
	"strconv"
)

func formatFloat(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return strconv.FormatFloat(v, 'g', 10, 64)
}

// F is a convenience float pointer.
func F(v float64) *float64 { return &v }

// Val dereferences with NaN fallback.
func Val(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}
