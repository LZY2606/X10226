package derive

import (
	"strconv"
)

func formatNum(v float64) string {
	return strconv.FormatFloat(float64(int64(v*1e6+0.5))/1e6, 'f', -1, 64)
}
