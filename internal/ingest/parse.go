package ingest

import (
	"math"
	"strconv"
	"strings"
)

// parseLooseFloat accepts the original representation including unit-bearing
// tokens such as "12.30 mg"; unit conversion itself happens in derive.
func parseLooseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return math.NaN(), strconv.ErrSyntax
	}
	// Strip a trailing unit token but keep original text in the raw row.
	fields := strings.Fields(s)
	if len(fields) > 1 {
		s = fields[0]
	}
	return strconv.ParseFloat(s, 64)
}
