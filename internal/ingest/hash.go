package ingest

import "crypto/sha256"

func shaBytes(b []byte) [32]byte { return sha256.Sum256(b) }
