package calc

import (
	"encoding/json"

	"tga-review/internal/domain"
)

// asType re-marshals an interface value and unmarshals into the target. This
// is needed because payloads round-trip through SQLite as map[string]any.
func asType(v any, out any) bool {
	if v == nil {
		return false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, out) == nil
}

func payloadSamplesX(s *domain.Source) []domain.SamplePoint {
	if s == nil {
		return nil
	}
	var out []domain.SamplePoint
	asType(s.Payload["points"], &out)
	return out
}

func payloadBlankX(s *domain.Source) []domain.BlankPoint {
	if s == nil {
		return nil
	}
	var out []domain.BlankPoint
	asType(s.Payload["points"], &out)
	return out
}

func payloadFurnaceX(s *domain.Source) []domain.FurnaceSegment {
	if s == nil {
		return nil
	}
	var out []domain.FurnaceSegment
	asType(s.Payload["segments"], &out)
	return out
}

func payloadValvesX(s *domain.Source) []domain.ValveEvent {
	if s == nil {
		return nil
	}
	var out []domain.ValveEvent
	asType(s.Payload["events"], &out)
	return out
}

func initialMassX(s *domain.Source) domain.InitialMass {
	var im domain.InitialMass
	if s == nil {
		return im
	}
	// payload may round-trip as map[string]any (SQLite) or typed struct (direct)
	if !asType(s.Payload["initial_mass"], &im) {
		var mass struct {
			Value float64 `json:"value"`
			Unit  string  `json:"unit"`
			Valid bool    `json:"valid"`
		}
		if m, ok := s.Payload["initial_mass"].(map[string]any); ok {
			asType(m["mass"], &mass)
			im.Mass.Value = mass.Value
			im.Mass.Unit = domain.Unit(mass.Unit)
			im.Mass.Valid = mass.Valid
			if note, ok := m["note"].(string); ok {
				im.Note = note
			}
		}
	}
	return im
}
