package store

import "time"

func eventTime(v any) time.Time {
	switch t := v.(type) {
	case nil:
		return time.Now().UTC()
	case time.Time:
		return t.UTC()
	case *time.Time:
		if t == nil {
			return time.Now().UTC()
		}
		return t.UTC()
	default:
		return time.Now().UTC()
	}
}

func parseTime(v string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, v)
	if err != nil {
		return time.Time{}
	}
	return t
}
