package calc

import (
	"encoding/json"
	"fmt"

	"gotga/internal/domain"
	"gotga/internal/ingest"
)

// FurnaceStep is one setpoint of the furnace program.
type FurnaceStep struct {
	StartT float64
	EndT   float64
	RateC  float64 // C/min, 0 = hold
	TargetC float64
}

// ValveEvent is one gas valve switch at valve time (not reaction time).
type ValveEvent struct {
	T   float64
	Gas string
}

// BlankPoint is one blank-curve point.
type BlankPoint struct{ T, M float64 }

// Mass0Info is the stratified initial sample mass.
type Mass0Info struct {
	MG    float64
	Raw   string
	Valid bool
	Diag  string
}

// DriftInfo describes balance baseline drift correction.
type DriftInfo struct {
	Mode   string // "linear" coefficients given, or "blank"
	Slope  float64 // mg/min
	Offset float64 // mg
	Raw    map[string]string
}

// Inputs assembles the current effective documents for a computation.
type Inputs struct {
	Samples  []domain.SamplePoint
	SampleDoc *domain.RawDoc
	Furnace  []FurnaceStep
	FurnaceDoc *domain.RawDoc
	Valves   []ValveEvent
	GasDoc   *domain.RawDoc
	Blank    []BlankPoint
	BlankDoc *domain.RawDoc
	Mass0    Mass0Info
	Mass0Doc *domain.RawDoc
	Drift    *DriftInfo
	DriftDoc *domain.RawDoc
	Diags    []string
}

type docPayload struct {
	Columns []string              `json:"columns"`
	Records []map[string]string   `json:"records"`
	Points  []domain.SamplePoint  `json:"points"`
	Diags   []string              `json:"diags"`
}

func loadDoc(d *domain.RawDoc) (*docPayload, error) {
	var p docPayload
	if d == nil {
		return &p, nil
	}
	if err := json.Unmarshal([]byte(d.Payload), &p); err != nil {
		return nil, fmt.Errorf("doc %s payload: %w", d.ID, err)
	}
	return &p, nil
}

// BuildInputs converts current effective docs into typed calc inputs.
func BuildInputs(docs map[domain.DocKind]*domain.RawDoc) (*Inputs, []string, error) {
	in := &Inputs{}
	var diags []string

	sd := docs[domain.KindSamples]
	in.SampleDoc = sd
	if sd == nil {
		return nil, nil, fmt.Errorf("missing samples layer")
	}
	sp, err := loadDoc(sd)
	if err != nil {
		return nil, nil, err
	}
	in.Samples = sp.Points
	diags = append(diags, sp.Diags...)
	in.Diags = append(in.Diags, sd.Diags...)

	if fd := docs[domain.KindFurnace]; fd != nil {
		in.FurnaceDoc = fd
		fp, _ := loadDoc(fd)
		for i, rec := range fp.Records {
			fs := FurnaceStep{}
			if v := ingest.ParseMeasure(rec["start_t"], "time", "start_t", i); v.Valid {
				fs.StartT = v.Value
			} else {
				diags = append(diags, v.Diag)
			}
			if v := ingest.ParseMeasure(rec["end_t"], "time", "end_t", i); v.Valid {
				fs.EndT = v.Value
			}
			if v := ingest.ParseMeasure(rec["target_c"], "temperature", "target_c", i); v.Valid {
				fs.TargetC = v.Value
			}
			if v := ingest.ParseMeasure(rec["rate_c_min"], "rate", "rate_c_min", i); v.Valid {
				fs.RateC = v.Value
			}
			in.Furnace = append(in.Furnace, fs)
		}
		in.Diags = append(in.Diags, fd.Diags...)
	} else {
		diags = append(diags, "furnace program layer absent; temperature axis approximate")
	}

	if gd := docs[domain.KindGas]; gd != nil {
		in.GasDoc = gd
		gp, _ := loadDoc(gd)
		for i, rec := range gp.Records {
			v := ingest.ParseMeasure(rec["t"], "time", "t", i)
			gas := rec["gas"]
			if !v.Valid {
				diags = append(diags, v.Diag)
				continue
			}
			if gas == "" {
				diags = append(diags, fmt.Sprintf("gas row %d missing gas name; kept at source", i))
			}
			in.Valves = append(in.Valves, ValveEvent{T: v.Value, Gas: gas})
		}
		in.Diags = append(in.Diags, gd.Diags...)
	}

	if bd := docs[domain.KindBlank]; bd != nil {
		in.BlankDoc = bd
		bp, _ := loadDoc(bd)
		for _, pt := range bp.Points {
			if pt.T != nil && pt.M != nil {
				in.Blank = append(in.Blank, BlankPoint{T: *pt.T, M: *pt.M})
			}
		}
	}

	if md := docs[domain.KindMass0]; md != nil {
		in.Mass0Doc = md
		mp, _ := loadDoc(md)
		in.Mass0 = Mass0Info{}
		if len(mp.Records) > 0 {
			raw := mp.Records[0]["mass0"]
			in.Mass0.Raw = raw
			v := ingest.ParseMeasure(raw, "mass", "mass0", 0)
			if v.Valid {
				in.Mass0.Valid = true
				in.Mass0.MG = v.Value
			} else {
				in.Mass0.Diag = v.Diag
				diags = append(diags, v.Diag)
			}
		}
		in.Diags = append(in.Diags, md.Diags...)
	} else {
		diags = append(diags, "mass0 layer absent; percentage losses and residue stay unknown")
	}

	if dd := docs[domain.KindDrift]; dd != nil {
		in.DriftDoc = dd
		dp, _ := loadDoc(dd)
		di := &DriftInfo{Raw: map[string]string{}}
		if len(dp.Records) > 0 {
			rec := dp.Records[0]
			di.Raw = rec
			if v := ingest.ParseMeasure(rec["slope_mg_min"], "rate", "slope_mg_min", 0); v.Valid {
				di.Slope = v.Value
			}
			if v := ingest.ParseMeasure(rec["offset_mg"], "mass", "offset_mg", 0); v.Valid {
				di.Offset = v.Value
			}
			di.Mode = rec["mode"]
		}
		in.Drift = di
	}
	return in, diags, nil
}
