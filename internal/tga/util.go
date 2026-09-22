package tga

import "strconv"

func i64(n int64) string { return strconv.FormatInt(n, 10) }

func f64(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
