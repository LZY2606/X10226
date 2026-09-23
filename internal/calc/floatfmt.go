package calc

import "strconv"

func formatFloat(x float64) string {
	return strconv.FormatFloat(x, 'g', -1, 64)
}
