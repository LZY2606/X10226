package derive

import (
	"strconv"
)

func strconvF(v float64) string { return strconv.FormatFloat(float64(int(v*1000+sign(v)))/1000, 'f', -1, 64) }
func strconvI(i int) string     { return strconv.Itoa(i) }

func sign(v float64) float64 {
	if v < 0 {
		return -0.5
	}
	return 0.5
}
