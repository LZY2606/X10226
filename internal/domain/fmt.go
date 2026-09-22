package domain

import "strconv"

func qf(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
