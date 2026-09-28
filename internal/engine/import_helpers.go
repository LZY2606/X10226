package engine

import (
	"strconv"
	"strings"

	"tga/internal/domain"
)

func parseFloatRaw(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}

// payloadOf extracts the type-specific payload so a source ID depends only on
// its immutable content, not on bookkeeping fields.
func payloadOf(s *domain.Source) any {
	switch s.Type {
	case domain.SourceSamples:
		return s.Samples
	case domain.SourceDrift:
		return s.Drift
	case domain.SourceProgram:
		return s.Program
	case domain.SourceGas:
		return s.Gas
	case domain.SourceBlank:
		return s.Blank
	case domain.SourceInitMass:
		return s.InitMass
	case domain.SourceParams:
		return s.Params
	}
	return nil
}

func defUnit(u, d string) string {
	if strings.TrimSpace(u) == "" {
		return d
	}
	return strings.TrimSpace(u)
}
