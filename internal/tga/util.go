package tga

import "strconv"

func itoa(i int) string      { return strconv.Itoa(i) }
func itoa64(i int64) string  { return strconv.FormatInt(i, 10) }
func ftoa(v float64) string  { return strconv.FormatFloat(v, 'g', -1, 64) }

func ptr(v float64) *float64 { return &v }
