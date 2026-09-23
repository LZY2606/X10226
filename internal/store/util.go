package store

import "time"

func timeNowRFC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
