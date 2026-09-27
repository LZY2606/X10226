// Package analysis implements derivation layer algorithms. It only reads
// immutable parsed inputs and writes versioned runs; raw data is never touched.
//
// Algorithm versions are explicit. Any change here must bump AlgorithmVersion
// so old runs stay identifiable.
package analysis

import (
	"encoding/json"
	"sort"

	"tga/internal/domain"
	"tga/internal/ingest"
)

const AlgorithmVersion = "tga-core-v1"

// Inputs bundles the exact source versions a run depends on.
type Inputs struct {
	Params      *domain.Parameters
	Samples     *ingest.Result
	Drift       *ingest.Result
	Furnace     *ingest.Result
	Gas         *ingest.Result
	Blank       *ingest.Result
	InitialMass *ingest.Result
	Refs        map[string]domain.SourceRef // kind -> ref
}

// Point is a fully normalised SI trace point.
type Point struct {
	T              float64
	TempC          float64
	MassG          float64
	CorrectedG     float64
	Pct            float64
	DTG            float64
	Gap            bool
	NonMonotonic   bool
	BlankApplied   bool
	SourceRow      int
}

// GasEvent is a normalised valve switch in elapsed seconds.
type GasEvent struct {
	T      float64
	From   string
	To     string
	RowIdx int
}

// FurnaceSeg is a normalised programmed segment (seconds, °C).
type FurnaceSeg struct {
	StartS float64
	EndS   float64
	Target float64
	Mode   string
	RowIdx int
}

// DriftModel describes a linear balance drift.
type DriftModel struct {
	T0S      float64
	OffsetG  float64
	SlopeGpS float64
	Present  bool
}

// Normalised is the full derived data set.
type Normalised struct {
	Points      []Point
	Gas         []GasEvent
	Furnace     []FurnaceSeg
	Drift       DriftModel
	BlankPoints []Point
	InitialMassG float64
	InitialBasis string
	HasInitial   bool
	Notes        []string
}

func q(row ingest.Row, key string) domain.Quantity {
	return row.Fields[key]
}

// Normalise converts original-unit quantities into the SI/coordinate basis.
// Rows that failed parsing are skipped here but remain stored at import.
func Normalise(in *Inputs) (*Normalised, []domain.Diagnostic) {
	out := &Normalised{}
	var diags []domain.Diagnostic
	project := ""
	_ = project

	// samples
	tu := in.Samples.Headers["time_unit"]; if tu == "" { tu = "s" }
	pu := in.Samples.Headers["temp_unit"]; if pu == "" { pu = "C" }
	mu := in.Samples.Headers["mass_unit"]; if mu == "" { mu = "mg" }
	accept := acceptedRows(in.Samples)
	sort.SliceStable(accept, func(i, j int) bool { return accept[i].Index < accept[j].Index })

	var lastT float64
	haveLast := false
	gapThreshold := in.Params.GapMaxS
	for _, r := range accept {
		tq := q(r, "time")
		pq := q(r, "temp")
		mq := q(r, "mass")
		ts, okT := ingest.ToSeconds(tq.Value, tq.Unit)
		tc, okP := ingest.ToCelsius(pq.Value, pq.Unit)
		mg, okM := ingest.ToGrams(mq.Value, mq.Unit)
		// Unknown normalisation: keep the value at its original position with a
		// diagnostic rather than dropping it.
		if !okT || !okP || !okM {
			diags = append(diags, domain.Diagnostic{
				Severity: domain.SevWarning, Code: "UNCONVERTIBLE_UNIT",
				Message: "row kept at import; excluded from derivation due to unit basis",
				SourceID: refID(in, domain.KindSamples), RowIndex: r.Index,
			})
			continue
		}
		gap := haveLast && ts-lastT > gapThreshold
		if gap {
			diags = append(diags, domain.Diagnostic{
				Severity: domain.SevWarning, Code: "SAMPLING_GAP",
				Message: "gap detected; no interpolation across gap",
				SourceID: refID(in, domain.KindSamples), RowIndex: r.Index,
			})
		}
		nm := false
		if haveLast {
			if prev := lastPoint(out.Points); prev != nil && mg > prev.MassG {
				nm = true
				diags = append(diags, domain.Diagnostic{
					Severity: domain.SevInfo, Code: "NON_MONOTONIC_MASS",
					Message: "mass increased vs previous accepted point; retained with flag",
					SourceID: refID(in, domain.KindSamples), RowIndex: r.Index,
				})
			}
		}
		out.Points = append(out.Points, Point{
			T: ts, TempC: tc, MassG: mg, CorrectedG: mg,
			Gap: gap, NonMonotonic: nm, SourceRow: r.Index,
		})
		lastT = ts
		haveLast = true
	}

	// drift
	if in.Drift != nil && len(acceptedRows(in.Drift)) > 0 {
		r := acceptedRows(in.Drift)[0]
		d := q(r, "drift.t0", q(r, "t0"))
		t0q := q(r, "t0")
		offq := q(r, "offset")
		slq := q(r, "slope")
		t0s, _ := ingest.ToSeconds(t0q.Value, t0q.Unit)
		offG, _ := ingest.ToGrams(offq.Value, offq.Unit)
		// slope unit like mg/h -> g/s
		slopeGpS := convertSlope(slq.Value, slq.Unit)
		out.Drift = DriftModel{T0S: t0s, OffsetG: offG, SlopeGpS: slopeGpS, Present: true}
		_ = d
	}

	// blank curve
	if in.Blank != nil {
		btu := in.Blank.Headers["time_unit"]; if btu == "" { btu = "s" }
		bpu := in.Blank.Headers["temp_unit"]; if bpu == "" { bpu = "C" }
		bmu := in.Blank.Headers["mass_unit"]; if bmu == "" { bmu = "mg" }
		for _, r := range acceptedRows(in.Blank) {
			ts, okT := ingest.ToSeconds(q(r, "time").Value, btu)
			tc, okP := ingest.ToCelsius(q(r, "temp").Value, bpu)
			mg, okM := ingest.ToGrams(q(r, "mass").Value, bmu)
			if okT && okP && okM {
				out.BlankPoints = append(out.BlankPoints,
					Point{T: ts, TempC: tc, MassG: mg, CorrectedG: mg, SourceRow: r.Index})
			}
		}
	}

	// furnace
	if in.Furnace != nil {
		ftu := in.Furnace.Headers["time_unit"]; if ftu == "" { ftu = "min" }
		fpu := in.Furnace.Headers["temp_unit"]; if fpu == "" { fpu = "C" }
		for _, r := range acceptedRows(in.Furnace) {
			st, ok1 := ingest.ToSeconds(q(r, "start").Value, ftu)
			en, ok2 := ingest.ToSeconds(q(r, "end").Value, ftu)
			tp, ok3 := ingest.ToCelsius(q(r, "target").Value, fpu)
			if ok1 && ok2 && ok3 {
				out.Furnace = append(out.Furnace, FurnaceSeg{
					StartS: st, EndS: en, Target: tp, Mode: q(r, "mode").Raw, RowIdx: r.Index})
			}
		}
	}

	// gas
	if in.Gas != nil {
		gtu := in.Gas.Headers["time_unit"]; if gtu == "" { gtu = "s" }
		for _, r := range acceptedRows(in.Gas) {
			ts, ok := ingest.ToSeconds(q(r, "time").Value, gtu)
			if !ok {
				diags = append(diags, domain.Diagnostic{
					Severity: domain.SevWarning, Code: "GAS_TIME_UNIT",
					Message: "gas event time unit not convertible; retained at import",
					SourceID: refID(in, domain.KindGas), RowIndex: r.Index})
				continue
			}
			out.Gas = append(out.Gas, GasEvent{
				T: ts, From: q(r, "from_gas").Raw, To: q(r, "to_gas").Raw, RowIdx: r.Index})
		}
		sort.SliceStable(out.Gas, func(i, j int) bool { return out.Gas[i].T < out.Gas[j].T })
	}

	// initial mass
	if in.InitialMass != nil && len(acceptedRows(in.InitialMass)) > 0 {
		r := acceptedRows(in.InitialMass)[len(acceptedRows(in.InitialMass))-1]
		mmu := in.InitialMass.Headers["mass_unit"]; if mmu == "" { mmu = "mg" }
		if mg, ok := ingest.ToGrams(q(r, "mass").Value, mmu); ok {
			out.InitialMassG = mg
			out.HasInitial = true
			out.InitialBasis = q(r, "basis").Raw
		}
	}

	applyDriftAndBlank(out)
	computePercentAndDTG(out)
	return out, diags
}

func acceptedRows(res *ingest.Result) []ingest.Row {
	if res == nil {
		return nil
	}
	var ok []ingest.Row
	for _, r := range res.Rows {
		if r.ParseOK {
			ok = append(ok, r)
		}
	}
	return ok
}

func refID(in *Inputs, kind string) string {
	if in.Refs != nil {
		if ref, ok := in.Refs[kind]; ok {
			return ref.SourceID
		}
	}
	return ""
}

func lastPoint(ps []Point) *Point {
	if len(ps) == 0 {
		return nil
	}
	return &ps[len(ps)-1]
}

// convertSlope accepts "<mass>/<time>" style units (mg/h, g/min, mg/s...).
func convertSlope(v float64, unit string) float64 {
	u := unit
	massMult, timeDiv := 1.0, 3600.0 // default mg/h -> g/s
	switch {
	case contains(u, "mg"):
		massMult = 1.0 / 1000
	case contains(u, "ug"), contains(u, "µg"):
		massMult = 1.0 / 1e6
	case startsWith(u, "g/"):
		massMult = 1
	}
	switch {
	case endsWith(u, "/h"), endsWith(u, "/hr"), endsWith(u, "/hour"):
		timeDiv = 3600
	case endsWith(u, "/min"), endsWith(u, "/minute"):
		timeDiv = 60
	case endsWith(u, "/s"), endsWith(u, "/sec"):
		timeDiv = 1
	}
	return v * massMult / timeDiv
}

func contains(s, sub string) bool { return len(s) >= len(sub) && indexOf(s, sub) >= 0 }
func startsWith(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
func endsWith(s, p string) bool   { return len(s) >= len(p) && s[len(s)-len(p):] == p }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// applyDriftAndBlank corrects mass in the derived layer only.
func applyDriftAndBlank(out *Normalised) {
	for i := range out.Points {
		p := &out.Points[i]
		if out.Drift.Present {
			corr := out.Drift.OffsetG + out.Drift.SlopeGpS*(p.T-out.Drift.T0S)
			p.CorrectedG = p.MassG - corr
		}
		if len(out.BlankPoints) > 0 {
			if b := interpBlank(out.BlankPoints, p.T); b != nil {
				p.CorrectedG -= b.MassG
				p.BlankApplied = true
			}
		}
	}
}

// interpBlank interpolates ONLY within the blank curve support; it never
// bridges sample gaps.
func interpBlank(blank []Point, t float64) *Point {
	if t < blank[0].T || t > blank[len(blank)-1].T {
		return nil
	}
	for i := 1; i < len(blank); i++ {
		if t <= blank[i].T {
			a, b := blank[i-1], blank[i]
			if b.T == a.T {
				return &a
			}
			f := (t - a.T) / (b.T - a.T)
			return &Point{T: t, MassG: a.MassG + f*(b.MassG-a.MassG)}
		}
	}
	return nil
}

func computePercentAndDTG(out *Normalised) {
	base := out.InitialMassG
	if base <= 0 && len(out.Points) > 0 {
		base = out.Points[0].CorrectedG
	}
	for i := range out.Points {
		if base > 0 {
			out.Points[i].Pct = 100 * out.Points[i].CorrectedG / base
		}
	}
	for i := 1; i < len(out.Points); i++ {
		a, b := out.Points[i-1], out.Points[i]
		dt := (b.T - a.T) / 60.0
		if dt > 0 && !b.Gap {
			out.Points[i].DTG = (b.Pct - a.Pct) / dt
		}
	}
}

// MarshalPoints is a small JSON helper used by the HTTP/SVG layer.
func MarshalPoints(ps []Point) string {
	b, _ := json.Marshal(ps)
	return string(b)
}
