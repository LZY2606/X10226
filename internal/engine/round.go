package engine

import "math"

// round4 stabilizes ids against tiny float jitter (4 decimals is below the
// physical precision of mg/s/K measurements in this domain).
func round4(x float64) float64 {
	return math.Round(x*1e4) / 1e4
}
