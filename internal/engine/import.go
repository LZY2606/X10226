package engine

import (
	"encoding/json"
	"fmt"

	"tga/internal/domain"
)

// ImportResult reports a (possibly partial) import. Accepted layers are
// versioned and applied; rejected layers keep their fragment and an explicit
// format error so a half-failed import never silently drops data.
type ImportResult struct {
	Accepted []AcceptedLayer `json:"accepted"`
	Rejected []RejectedLayer `json:"rejected"`
}

type AcceptedLayer struct {
	Type   domain.SourceType `json:"type"`
	Source *domain.Source    `json:"source"`
}
type RejectedLayer struct {
	Type     domain.SourceType `json:"type"`
	Fragment json.RawMessage   `json:"fragment"`
	Errors   []string          `json:"errors"`
}

type layerIn struct {
	Type    domain.SourceType `json:"type"`
	Label   string            `json:"label"`
	Samples []sampleIn        `json:"samples"`
	Drift   *driftIn          `json:"drift"`
	Program []programIn       `json:"program"`
	Gas     []gasIn           `json:"gas"`
	Blank   []blankIn         `json:"blank"`
	InitialMass *initMassIn   `json:"initial_mass"`
	Origin  string            `json:"origin"`
}

type sampleIn struct {
	Index       *int    `json:"index"`
	Time        string  `json:"time"`
	TimeUnit    string  `json:"time_unit"`
	FurnaceTime string  `json:"furnace_time"`
	FurnaceUnit string  `json:"furnace_time_unit"`
	Mass        string  `json:"mass"`
	MassUnit    string  `json:"mass_unit"`
	Temp        string  `json:"temp"`
	TempUnit    string  `json:"temp_unit"`
	Present     *bool   `json:"present"`
}

type driftIn struct {
	Kind      string  `json:"kind"`
	Slope     string  `json:"slope"`
	Intercept string  `json:"intercept"`
	BlankID   string  `json:"blank_id"`
}
type programIn struct {
	From        string `json:"from"`
	To          string `json:"to"`
	StartTemp   string `json:"start_temp"`
	TargetTemp  string `json:"target_temp"`
	Rate        string `json:"rate"`
}
type gasIn struct {
	At     string  `json:"at"`
	From   string  `json:"from_gas"`
	To     string  `json:"to_gas"`
	Flow   string  `json:"flow"`
}
type blankIn struct {
	Time string  `json:"time"`
	Mass string  `json:"mass"`
	Temp string  `json:"temp"`
}
type initMassIn struct {
	Value string `json:"value"`
	Unit  string `json:"unit"`
	Basis string `json:"basis"`
	Note  string `json:"note"`
}

// ParseImport decodes one import document containing any subset of the six
// layers plus optional algorithm parameters.
func ParseImport(raw []byte) ([]layerIn, *ParamsEnvelope, error) {
	var doc struct {
		Layers []layerIn       `json:"layers"`
		Params *ParamsEnvelope `json:"params"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("document is not valid JSON: %w", err)
	}
	if doc.Layers == nil {
		// also accept a bare layer array
		var arr []layerIn
		if err := json.Unmarshal(raw, &arr); err == nil && arr != nil {
			return arr, nil, nil
		}
		return nil, nil, fmt.Errorf("import document must contain a non-empty \"layers\" array")
	}
	if len(doc.Layers) == 0 {
		return nil, nil, fmt.Errorf("import document contains no layers")
	}
	return doc.Layers, doc.Params, nil
}

// ParamsEnvelope wraps a parameter revision.
type ParamsEnvelope struct {
	domain.Params
}

func (e *Engine) buildLayer(projectID string, in layerIn, prev map[domain.SourceType]*domain.Source, seq int64) (*domain.Source, []string) {
	var errs []string
	s := &domain.Source{
		ProjectID: projectID, Type: in.Type, Label: in.Label,
		Origin: domain.Origin(in.Origin), CreatedEventSeq: seq,
		Basis: domain.CoordinateBasis{TimeOrigin: "experiment_start", TimeUnit: "s", TempBasis: "sample_measured"},
	}
	if s.Origin == "" {
		s.Origin = domain.OriginImported
	}
	if p := prev[in.Type]; p != nil {
		s.ParentID = p.ID
		s.Version = p.Version + 1
	} else {
		s.Version = 1
	}
	switch in.Type {
	case domain.SourceSamples:
		if len(in.Samples) == 0 {
			errs = append(errs, "samples layer requires at least one point")
		}
		for i, r := range in.Samples {
			idx := i
			if r.Index != nil {
				idx = *r.Index
			}
			present := true
			if r.Present != nil {
				present = *r.Present
			}
			sp := domain.SamplePoint{
				Index: idx, Present: present,
				Time:        domain.Convert(r.Time, defUnit(r.TimeUnit, "s"), domain.KindTime),
				FurnaceTime: domain.Convert(r.FurnaceTime, defUnit(r.FurnaceUnit, "s"), domain.KindTime),
				Mass:        domain.Convert(r.Mass, defUnit(r.MassUnit, "mg"), domain.KindMass),
				Temp:        domain.Convert(r.Temp, defUnit(r.TempUnit, "C"), domain.KindTemperature),
				Raw: map[string]string{
					"time": r.Time, "time_unit": r.TimeUnit, "mass": r.Mass, "mass_unit": r.MassUnit,
					"temp": r.Temp, "temp_unit": r.TempUnit,
					"furnace_time": r.FurnaceTime, "furnace_time_unit": r.FurnaceUnit,
				},
			}
			if present {
				if !sp.Time.ConvertOK {
					errs = append(errs, fmt.Sprintf("row %d: %s", idx, sp.Time.Diagnostic))
				}
				if !sp.Mass.ConvertOK {
					errs = append(errs, fmt.Sprintf("row %d: %s", idx, sp.Mass.Diagnostic))
				}
			}
			s.Samples = append(s.Samples, sp)
		}
	case domain.SourceDrift:
		if in.Drift == nil {
			errs = append(errs, "drift layer requires a \"drift\" object")
			break
		}
		d := &domain.DriftCorrection{Kind: in.Drift.Kind, BlankID: in.Drift.BlankID,
			Raw: map[string]string{"kind": in.Drift.Kind, "slope": in.Drift.Slope, "intercept": in.Drift.Intercept}}
		switch d.Kind {
		case "linear":
			sl, e1 := parseFloatRaw(in.Drift.Slope)
			ic, e2 := parseFloatRaw(in.Drift.Intercept)
			if e1 != nil {
				errs = append(errs, "drift.slope: "+e1.Error())
			}
			if e2 != nil {
				errs = append(errs, "drift.intercept: "+e2.Error())
			}
			d.Slope, d.Intercept = sl, ic
		case "blank_curve":
			if d.BlankID == "" {
				errs = append(errs, "drift.kind=blank_curve requires blank_id")
			}
		case "none", "":
			d.Kind = "none"
		default:
			errs = append(errs, fmt.Sprintf("drift.kind %q is not one of linear|blank_curve|none", d.Kind))
		}
		s.Drift = d
	case domain.SourceProgram:
		if len(in.Program) == 0 {
			errs = append(errs, "furnace layer requires program steps")
		}
		for i, st := range in.Program {
			from, e1 := parseFloatRaw(st.From)
			to, e2 := parseFloatRaw(st.To)
			st0, e3 := parseFloatRaw(st.StartTemp)
			tgt, e4 := parseFloatRaw(st.TargetTemp)
			for _, e := range []error{e1, e2, e3, e4} {
				if e != nil {
					errs = append(errs, fmt.Sprintf("program[%d]: %s", i, e.Error()))
				}
			}
			kind := "ramp"
			if st0 == tgt {
				kind = "hold"
			}
			s.Program = append(s.Program, domain.ProgramStep{
				Index: i, FromTime: from, ToTime: to, StartTempC: st0, TargetTempC: tgt,
				Kind: kind,
			})
		}
	case domain.SourceGas:
		if len(in.Gas) == 0 {
			errs = append(errs, "gas layer requires at least one valve event")
		}
		for i, g := range in.Gas {
			at, e1 := parseFloatRaw(g.At)
			if e1 != nil {
				errs = append(errs, fmt.Sprintf("gas[%d].at: %s", i, e1.Error()))
			}
			flow, _ := parseFloatRaw(g.Flow)
			if g.From == "" || g.To == "" {
				errs = append(errs, fmt.Sprintf("gas[%d]: from_gas and to_gas are required", i))
			}
			s.Gas = append(s.Gas, domain.GasEvent{
				Index: i, ValveTimeS: at, FromGas: g.From, ToGas: g.To, FlowMLperMin: flow,
				Raw: map[string]string{"at": g.At, "from_gas": g.From, "to_gas": g.To},
			})
		}
	case domain.SourceBlank:
		if len(in.Blank) == 0 {
			errs = append(errs, "blank layer requires points")
		}
		for i, b := range in.Blank {
			t, e1 := parseFloatRaw(b.Time)
			m, e2 := parseFloatRaw(b.Mass)
			tc, e3 := parseFloatRaw(b.Temp)
			for _, e := range []error{e1, e2, e3} {
				if e != nil {
					errs = append(errs, fmt.Sprintf("blank[%d]: %s", i, e.Error()))
				}
			}
			s.Blank = append(s.Blank, domain.BlankPoint{Index: i, TimeS: t, MassMg: m, TempC: tc})
		}
	case domain.SourceInitMass:
		if in.InitialMass == nil {
			errs = append(errs, "initial_mass layer requires an initial_mass object")
			break
		}
		mv := domain.Convert(in.InitialMass.Value, defUnit(in.InitialMass.Unit, "mg"), domain.KindMass)
		if !mv.ConvertOK {
			errs = append(errs, "initial_mass.value: "+mv.Diagnostic)
		}
		s.InitMass = &domain.InitialMass{Mass: mv, Basis: in.InitialMass.Basis, Note: in.InitialMass.Note}
	default:
		errs = append(errs, fmt.Sprintf("unknown layer type %q", in.Type))
	}

	// structural identity computed from the immutable payload
	s.ID = sourceID(s)
	if len(errs) > 0 {
		return nil, errs
	}
	return s, nil
}

func sourceID(s *domain.Source) string {
	return domain.StableID("src-"+string(s.Type), []any{
		"v1", s.ProjectID, s.Type, s.Version, s.ParentID, payloadOf(s),
	})
}
