package store

import "tga-lineage/internal/identity"

// eventUID is deterministic from (project, seq, kind, payload), so a rebuilt
// database from an export carries the same event identities.
func eventUID(project string, seq int64, kind, payload string) string {
	return identity.Hash("evt_", project, itoa(seq), kind, payload)
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
