package store

import "time"

type timeT = time.Time

func timeParse(ts string) time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.000000000Z", time.RFC3339} {
		if t, err := time.Parse(layout, ts); err == nil {
			return t
		}
	}
	return time.Time{}
}
