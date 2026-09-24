package derive

import "encoding/hex"

func hexShort(b []byte) string {
	if len(b) > 8 {
		b = b[:8]
	}
	return hex.EncodeToString(b)
}
