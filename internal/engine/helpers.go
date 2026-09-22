package engine

import "strconv"

func f64f(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
