package derive

import (
	"encoding/json"

	"tga/internal/domain"
)

// Inputs bundles the typed direct source layers for one derivation.
type Inputs struct {
	Samples     []domain.SamplePoint
	Drift       []domain.DriftPoint
	Program     []domain.ProgramSegment
	Valves      []domain.GasValveEvent
	Blank       []domain.BlankPoint
	InitialMass *domain.InitialMass
	Versions    map[domain.SourceKind]int
}

// ParseInputs decodes raw submissions into typed layers. Missing optional
// layers are allowed; a malformed layer returns an error so the caller can
// quarantine it as an import/derive failure.
func ParseInputs(raws map[domain.SourceKind]domain.RawSubmission) (*Inputs, error) {
	in := &Inputs{Versions: map[domain.SourceKind]int{}}
	decode := func(k domain.SourceKind, dst any) error {
		r, ok := raws[k]
		if !ok {
			return nil
		}
		if err := json.Unmarshal(r.Payload, dst); err != nil {
			return err
		}
		in.Versions[k] = r.Version
		return nil
	}
	if err := decode(domain.KindSamples, &in.Samples); err != nil {
		return nil, err
	}
	if err := decode(domain.KindDrift, &in.Drift); err != nil {
		return nil, err
	}
	if err := decode(domain.KindProgram, &in.Program); err != nil {
		return nil, err
	}
	if err := decode(domain.KindGasEvent, &in.Valves); err != nil {
		return nil, err
	}
	if err := decode(domain.KindBlank, &in.Blank); err != nil {
		return nil, err
	}
	if r, ok := raws[domain.KindInitialMass]; ok {
		var im domain.InitialMass
		if err := json.Unmarshal(r.Payload, &im); err != nil {
			return nil, err
		}
		in.InitialMass = &im
		in.Versions[domain.KindInitialMass] = r.Version
	}
	return in, nil
}
