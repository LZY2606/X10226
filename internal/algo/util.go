package algo

import (
	"fmt"
	"strconv"
)

func idf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

func strconvF(v float64) string { return strconv.FormatFloat(v, 'g', 6, 64) }

// peakCounterStart derives the numeric counter embedded in a parent peak id
// so alternative sub-peaks get deterministic, stable identities.
func peakCounterStart(id string) int {
	var n int
	for _, c := range id {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// round2 keeps displayed derived numbers stable.
func round2(v float64) float64 {
	return float64(int(v*100+sign(v)*0.5)) / 100
}

func sign(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
