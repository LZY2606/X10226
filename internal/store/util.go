package store

import "time"

const timeFmt = "2006-01-02T15:04:05.000000000Z07:00"

func parseTime(s string) time.Time {
	t, err := time.Parse(timeFmt, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func nf(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}
