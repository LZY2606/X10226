package engine

import "tga-review/internal/domain"

func resArtValue(a domain.Artifact) domain.Residual {
	var r domain.Residual
	_ = decodePayload(a.Payload, &r)
	return r
}
