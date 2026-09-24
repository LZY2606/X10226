package domain

import "crypto/sha256"

func SHA256(b []byte) [32]byte { return sha256.Sum256(b) }
