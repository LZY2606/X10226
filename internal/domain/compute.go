package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

var baseTime = Unit{"thermogravimetric", "s", "furnace elapsed time; t=0 at program start"}
var mgUnit = Unit{"thermogravimetric", "mg", "balance mass"}
var cUnit = Unit{"thermogravimetric", "°C", "temperature"}
var dtgUnit = Unit{"thermogravimetric", "mg/min", "finite difference, no cross-gap interpolation"}
var pctUnit = Unit{"thermogravimetric", "%", "mass percentage of accepted initial mass"}

func CanonicalJSON(v any) string {
	b, _ := json.Marshal(sortJSON(v))
	return string(b)
}

func HashJSON(v any) string {
	sum := sha256.Sum256([]byte(CanonicalJSON(v)))
	return hex.EncodeToString(sum[:])
}

func sortJSON(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(x))
		for _, k := range keys {
			out[k] = sortJSON(x[k])
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i := range x {
			out[i] = sortJSON(x[i])
		}
		return out
	default:
		return v
	}
}

func latestBatch(st *ProjectState, kind string) (RawBatch, bool) {
	var found RawBatch
	ok := false
	for _, id := range st.BatchOrder {
		b := st.Batches[id]
		if b.Kind == kind && b.Accepted {
			found, ok = b, true
		}
	}
	return found, ok
}

func batchParents(st *ProjectState, kind string) []SourceRef {
	var refs []SourceRef
	for _, id := range st.BatchOrder {
		b := st.Batches[id]
		if b.Kind == kind && b.Accepted {
			refs = append(refs, SourceRef{SourceID: b.ID, SourceKind: b.Kind})
		}
	}
	return refs
}

func q(id, label, raw string, value *float64, unit Unit, status string, sources []SourceRef, revision string) Quantity {
	return Quantity{ID: id, Label: label, Raw: raw, Value: value, Unit: unit, Status: status, SourceIDs: sourceIDValues(sources), Algorithm: AlgorithmVersion, RevisionID: revision}
}

func sourceIDValues(refs []SourceRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, r.SourceID)
	}
	return out
}

func valPtr(v float64) *float64 { return &v }

func parseBatchRows(b RawBatch) ([]parsedRow, []Diagnostic) {
	var rows []parsedRow
	var diags []Diagnostic
	for i, rawRow := range b.Rows {
		pr := parsedRow{SourceID: b.ID, Row: i + 1, Values: map[string]float64{}, Raw: map[string]string{}, Invalid: map[string]string{}}
		for field, raw := range rawRow {
			pr.Raw[field] = raw
			v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
			if err != nil {
				pr.Invalid[field] = raw
				diags = append(diags, Diagnostic{
					Code: "VALUE_NOT_CONVERTIBLE", Severity: "warning",
					Message:         fmt.Sprintf("%s=%q kept at source; no normalization was used for plotting", field, raw),
					Location:        fmt.Sprintf("%s:row:%d:%s", b.ID, i+1, field),
					AffectedObjects: []string{fmt.Sprintf("%s#r%d.%s", b.ID, i+1, field)},
				})
			} else {
				pr.Values[field] = v
			}
		}
		rows = append(rows, pr)
	}
	return rows, diags
}

func sourceFingerprint(st *ProjectState) string {
	type fp struct {
		ID       string `json:"id"`
		Kind     string `json:"kind"`
		ParentID string `json:"parentId"`
		Accepted bool   `json:"accepted"`
		RawText  string `json:"rawText"`
		Order    int    `json:"order"`
	}
	var items []fp
	for _, id := range st.BatchOrder {
		b := st.Batches[id]
		items = append(items, fp{b.ID, b.Kind, b.ParentID, b.Accepted, b.RawText, b.Order})
	}
	return HashJSON(items)
}

func parameterHash(p Parameters) string { return HashJSON(p) }

func get(r parsedRow, field string) (float64, bool) {
	v, ok := r.Values[field]
	return v, ok
}

func f3(v float64) float64 {
	if math.Abs(v) < 1e-12 {
		return 0
	}
	return math.Round(v*1000) / 1000
}

func ref(b RawBatch, row int, field string) []SourceRef {
	return []SourceRef{{SourceID: b.ID, SourceKind: b.Kind, Row: row, Field: field}}
}

type point struct {
	Time      float64
	RawMass   *float64
	Blank     *float64
	Corrected *float64
	SampleC   *float64
	ProgramC  *float64
	DTG       *float64
	Invalid   map[string]string
	SourceRow int
}

type blankPoint struct {
	Time, Mass float64
	Row        int
}

func collectPoints(sample, blank, furnace RawBatch, p Parameters) ([]point, []Diagnostic, map[string]string) {
	sampleRows, d1 := parseBatchRows(sample)
	blankRows, d2 := parseBatchRows(blank)
	furnaceRows, d3 := parseBatchRows(furnace)
	diags := append(append(d1, d2...), d3...)
	deps := map[string]string{
		"source:sample_points": HashJSON(struct {
			ID     string `json:"id"`
			Raw    string `json:"rawText"`
			Parent string `json:"parentId"`
		}{sample.ID, sample.RawText, sample.ParentID}),
		"source:blank_curve": HashJSON(struct {
			ID     string `json:"id"`
			Raw    string `json:"rawText"`
			Parent string `json:"parentId"`
		}{blank.ID, blank.RawText, blank.ParentID}),
		"source:furnace_program": HashJSON(struct {
			ID     string `json:"id"`
			Raw    string `json:"rawText"`
			Parent string `json:"parentId"`
		}{furnace.ID, furnace.RawText, furnace.ParentID}),
		"param:balanceDriftMgPerMin": HashJSON(p.BalanceDriftMgPerMin),
		"param:temperatureLagS":      HashJSON(p.TemperatureLagS),
		"algorithm":                  HashJSON(AlgorithmVersion),
	}
	var blanks []blankPoint
	for _, r := range blankRows {
		if t, ok1 := get(r, "time_s"); t >= 0 && ok1 {
			if m, ok2 := get(r, "blank_mass_mg"); ok2 {
				blanks = append(blanks, blankPoint{t, m, r.Row})
			}
		}
	}
	sort.Slice(blanks, func(i, j int) bool { return blanks[i].Time < blanks[j].Time })

	furnaceByTime := map[float64]parsedRow{}
	for _, r := range furnaceRows {
		if t, ok := get(r, "time_s"); ok {
			furnaceByTime[t] = r
		}
	}

	var points []point
	for _, r := range sampleRows {
		pt := point{SourceRow: r.Row, Invalid: map[string]string{}}
		for k, v := range r.Invalid {
			pt.Invalid[k] = v
		}
		if t, ok := get(r, "time_s"); ok {
			pt.Time = t
			if ts, exists := furnaceByTime[t]; exists {
				if v, ok := get(ts, "program_temp_c"); ok {
					pt.ProgramC = valPtr(f3(v))
				}
			}
			if v, ok := get(r, "sample_temp_c"); ok {
				pt.SampleC = valPtr(f3(v))
			}
			if b, br, ok := interpolateBlank(blanks, t); ok {
				pt.Blank = valPtr(f3(b))
				_ = br
			} else {
				diags = append(diags, Diagnostic{Code: "BLANK_NOT_AVAILABLE", Severity: "warning", Message: "blank correction is unknown at this time; raw point remains but corrected mass is not plotted", Location: fmt.Sprintf("%s:row:%d", sample.ID, r.Row), AffectedObjects: []string{fmt.Sprintf("tg-point:%g", t)}})
			}
			if m, ok := get(r, "mass_mg"); ok {
				pt.RawMass = valPtr(f3(m))
				if pt.Blank != nil {
					corrected := m - *pt.Blank - p.BalanceDriftMgPerMin*(t/60.0)
					pt.Corrected = valPtr(f3(corrected))
				}
			}
		}
		points = append(points, pt)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Time < points[j].Time })

	for i := range points {
		if points[i].Corrected == nil {
			continue
		}
		if i > 0 && points[i-1].Corrected != nil && points[i].Time-points[i-1].Time <= p.MaxSampleGapS && *points[i].Corrected > *points[i-1].Corrected+0.005 {
			diags = append(diags, Diagnostic{Code: "NONMONOTONIC_MASS_INCREASE", Severity: "info", Message: "observed mass increase is retained as evidence of noise or buoyancy; no smoothing is applied", Location: fmt.Sprintf("%s:row:%d", sample.ID, points[i].SourceRow), AffectedObjects: []string{fmt.Sprintf("tg-point:%g", points[i].Time)}})
		}
		if i > 0 && i+1 < len(points) && points[i-1].Corrected != nil && points[i+1].Corrected != nil {
			before, after := points[i-1], points[i+1]
			gap1, gap2 := points[i].Time-before.Time, after.Time-points[i].Time
			if gap1 <= p.MaxSampleGapS && gap2 <= p.MaxSampleGapS {
				d := (*after.Corrected - *before.Corrected) / ((after.Time - before.Time) / 60.0)
				points[i].DTG = valPtr(f3(d))
			} else {
				diags = append(diags, Diagnostic{Code: "DTG_NOT_ACROSS_GAP", Severity: "warning", Message: "DTG left unknown because one neighboring interval is a sampling gap", Location: fmt.Sprintf("%s:row:%d", sample.ID, points[i].SourceRow), AffectedObjects: []string{fmt.Sprintf("dtg-point:%g", points[i].Time)}})
			}
		}
	}
	return points, diags, deps
}

func interpolateBlank(xs []blankPoint, t float64) (float64, blankPoint, bool) {
	if len(xs) == 0 {
		return 0, blankPoint{}, false
	}
	if t <= xs[0].Time {
		return xs[0].Mass, xs[0], true
	}
	if t >= xs[len(xs)-1].Time {
		last := xs[len(xs)-1]
		return last.Mass, last, true
	}
	for i := 1; i < len(xs); i++ {
		if t <= xs[i].Time {
			a, b := xs[i-1], xs[i]
			frac := 0.0
			if b.Time != a.Time {
				frac = (t - a.Time) / (b.Time - a.Time)
			}
			return a.Mass + (b.Mass-a.Mass)*frac, a, true
		}
	}
	last := xs[len(xs)-1]
	return last.Mass, last, true
}

func buildSeries(points []point, sample RawBatch, p Parameters, deps map[string]string) ([]ComputedSeries, []Gap) {
	rawPts := make([]Quantity, 0, len(points))
	corrPts := make([]Quantity, 0, len(points))
	dtgPts := make([]Quantity, 0, len(points))
	tempPts := make([]Quantity, 0, len(points))
	var gaps []Gap
	for i, pt := range points {
		timeRaw := strconv.FormatFloat(pt.Time, 'f', 0, 64)
		timeQ := q(fmt.Sprintf("%s#r%d.time_s", sample.ID, pt.SourceRow), "furnace elapsed time", timeRaw, valPtr(pt.Time), baseTime, "ok", ref(sample, pt.SourceRow, "time_s"), "")
		_ = timeQ
		refs := ref(sample, pt.SourceRow, "mass_mg")
		if pt.RawMass != nil {
			rawPts = append(rawPts, q(fmt.Sprintf("tg-raw-point:%g", pt.Time), "raw balance mass", strconv.FormatFloat(*pt.RawMass, 'f', 3, 64), pt.RawMass, mgUnit, "ok", refs, ""))
		} else {
			rawText := pt.Invalid["mass_mg"]
			rawPts = append(rawPts, q(fmt.Sprintf("tg-raw-point:%g", pt.Time), "raw balance mass", rawText, nil, mgUnit, "unknown", refs, ""))
		}
		if pt.Corrected != nil {
			refs = append(refs, SourceRef{SourceID: "blank_curve", SourceKind: "blank_curve"}, SourceRef{SourceID: "param:balanceDriftMgPerMin", SourceKind: "parameter"})
			corrPts = append(corrPts, q(fmt.Sprintf("tg-corrected-point:%g", pt.Time), "blank- and drift-corrected mass", strconv.FormatFloat(*pt.Corrected, 'f', 3, 64), pt.Corrected, mgUnit, "ok", refs, ""))
		} else {
			corrPts = append(corrPts, q(fmt.Sprintf("tg-corrected-point:%g", pt.Time), "corrected mass", "", nil, mgUnit, "unknown", refs, ""))
		}
		if pt.DTG != nil {
			dtgPts = append(dtgPts, q(fmt.Sprintf("dtg-point:%g", pt.Time), "DTG central finite difference", strconv.FormatFloat(*pt.DTG, 'f', 3, 64), pt.DTG, dtgUnit, "ok", ref(sample, pt.SourceRow, "mass_mg"), ""))
		} else {
			status := "unknown"
			diag := "boundary or sampling gap; no interpolated DTG"
			_ = diag
			dtgPts = append(dtgPts, Quantity{ID: fmt.Sprintf("dtg-point:%g", pt.Time), Label: "DTG", Raw: "", Value: nil, Unit: dtgUnit, Status: status, SourceIDs: []string{sample.ID}, Algorithm: AlgorithmVersion})
		}
		if pt.SampleC != nil {
			tempPts = append(tempPts, q(fmt.Sprintf("sample-temp-point:%g", pt.Time), "sample temperature", strconv.FormatFloat(*pt.SampleC, 'f', 1, 64), pt.SampleC, cUnit, "ok", ref(sample, pt.SourceRow, "sample_temp_c"), ""))
		}
		if i > 0 && pt.Time-points[i-1].Time > p.MaxSampleGapS {
			gaps = append(gaps, Gap{
				ID: fmt.Sprintf("sample-gap:%g-%g", points[i-1].Time, pt.Time), StartS: points[i-1].Time, EndS: pt.Time,
				Reason: "adjacent observed samples exceed the maximum accepted sampling interval", NoInterpolation: true,
			})
		}
	}
	for i := range []ComputedSeries{} {
		_ = i
	}
	series := []ComputedSeries{
		{ID: "series:tg-raw", Kind: "tg_raw", Label: "Raw balance mass", Unit: mgUnit, Points: rawPts, Revision: sample.ID, Deps: map[string]string{"source:sample_points": deps["source:sample_points"]}},
		{ID: "series:tg-corrected", Kind: "tg_corrected", Label: "Corrected TG", Unit: mgUnit, Points: corrPts, Gaps: gaps, Revision: sample.ID, Deps: map[string]string{"source:sample_points": deps["source:sample_points"], "source:blank_curve": deps["source:blank_curve"], "param:balanceDriftMgPerMin": deps["param:balanceDriftMgPerMin"]}},
		{ID: "series:dtg", Kind: "dtg", Label: "DTG", Unit: dtgUnit, Points: dtgPts, Gaps: gaps, Revision: sample.ID, Deps: map[string]string{"source:sample_points": deps["source:sample_points"], "param:maxSampleGapS": HashJSON(p.MaxSampleGapS)}},
		{ID: "series:sample-temperature", Kind: "sample_temperature", Label: "Sample temperature", Unit: cUnit, Points: tempPts, Revision: sample.ID, Deps: map[string]string{"source:sample_points": deps["source:sample_points"]}},
	}
	return series, gaps
}

type runWindow struct {
	Start, End float64
	Peak       *float64
	PeakDTG    *float64
}

func findRuns(points []point, threshold float64, maxGap float64) []runWindow {
	var runs []runWindow
	i := 1
	for i < len(points)-1 {
		if points[i].DTG != nil && *points[i].DTG < threshold {
			startIdx := i - 1
			endIdx := i
			peakIdx := i
			for endIdx+1 < len(points) && points[endIdx].DTG != nil && *points[endIdx].DTG < threshold && points[endIdx].Time-points[endIdx-1].Time <= maxGap {
				if *points[endIdx].DTG < *points[peakIdx].DTG {
					peakIdx = endIdx
				}
				endIdx++
			}
			if points[endIdx].Time-points[endIdx-1].Time > maxGap {
				endIdx--
			}
			peak := points[peakIdx].Time
			peakV := *points[peakIdx].DTG
			runs = append(runs, runWindow{Start: points[startIdx].Time, End: points[endIdx].Time, Peak: &peak, PeakDTG: &peakV})
			i = endIdx + 1
		} else {
			i++
		}
	}
	return runs
}

func localMaxDTG(points []point, between runWindow) *float64 {
	var best *float64
	for i := 1; i < len(points)-1; i++ {
		if points[i].DTG == nil || points[i].Time <= between.Start || points[i].Time >= between.End {
			continue
		}
		if points[i-1].DTG != nil && points[i+1].DTG != nil && *points[i].DTG > *points[i-1].DTG && *points[i].DTG > *points[i+1].DTG {
			if best == nil || *points[i].DTG > *best {
				v := points[i].Time
				best = &v
			}
		}
	}
	return best
}

func correctedAt(points []point, t float64) (float64, bool) {
	for _, p := range points {
		if p.Time == t {
			if p.Corrected != nil {
				return *p.Corrected, true
			}
			return 0, false
		}
	}
	return 0, false
}

func fixedStage(id, label, kind string, w runWindow, points []point, initial float64, p Parameters, gas string) StageCandidate {
	start, _ := correctedAt(points, w.Start)
	end, _ := correctedAt(points, w.End)
	loss := f3(start - end)
	lossPct := f3(100 * loss / initial)
	c := StageCandidate{
		ID: id, RevisionID: id + ":rev1", Kind: kind, Label: label,
		StartS: w.Start, EndS: w.End, MassLossMg: loss, LossPctInitial: lossPct,
		AtmosphereAtPeak: gas, Status: "candidate",
		SourceIDs: []string{"sample_points:v1", "parameter:dtg-threshold", "parameter:max-sample-gap"},
		Deps:      map[string]string{"source:sample_points": "sample_points:v1", "param:dtg": HashJSON([]float64{p.MainDTGThresholdMgPerMin, p.WeakDTGThresholdMgPerMin}), "param:maxSampleGapS": HashJSON(p.MaxSampleGapS)},
	}
	if w.Peak != nil {
		c.PeakS = valPtr(*w.Peak)
	}
	if w.PeakDTG != nil {
		c.PeakMgPerMin = valPtr(*w.PeakDTG)
	}
	c.Chain = []ChainNode{
		{ID: id + ":start", Label: "start corrected mass", Order: 1, Value: q(id+":start-mass", label+" start", strconv.FormatFloat(start, 'f', 3, 64), &start, mgUnit, "ok", nil, ""), Formula: "TG_corrected(start)"},
		{ID: id + ":end", Label: "end corrected mass", Order: 2, Value: q(id+":end-mass", label+" end", strconv.FormatFloat(end, 'f', 3, 64), &end, mgUnit, "ok", nil, ""), Formula: "TG_corrected(end)"},
		{ID: id + ":loss", Label: "mass loss", Order: 3, Value: q(id+":mass-loss", label+" Δm", strconv.FormatFloat(loss, 'f', 3, 64), &loss, mgUnit, "ok", nil, ""), Formula: "m_start-m_end"},
		{ID: id + ":pct", Label: "initial-mass percentage", Order: 4, Value: q(id+":loss-pct", label+" loss percentage", strconv.FormatFloat(lossPct, 'f', 2, 64), &lossPct, pctUnit, "ok", nil, ""), Formula: "100*Δm/m_initial"},
	}
	return c
}

func gasAtTime(valveRows []parsedRow, delay, t float64) string {
	gas := "initial"
	for _, r := range valveRows {
		at, ok1 := get(r, "time_s")
		g := strings.TrimSpace(r.Raw["gas"])
		if ok1 && g != "" && at+delay <= t {
			gas = g
		}
	}
	return gas
}

func buildStages(points []point, p Parameters, initial float64, valveRows []parsedRow, gaps []Gap) []StageCandidate {
	var out []StageCandidate
	broad := findRuns(points, p.WeakDTGThresholdMgPerMin, p.MaxSampleGapS)
	main := findRuns(points, p.MainDTGThresholdMgPerMin, p.MaxSampleGapS)
	for i, w := range broad {
		gas := "N2→CO2"
		if w.Peak != nil {
			gas = gasAtTime(valveRows, p.ValveDelayS, *w.Peak)
		}
		id := fmt.Sprintf("stage:broad-%d", i+1)
		label := "Weak-DTG mass-loss candidate"
		if i == 0 {
			label = "Moisture / volatile loss candidate"
		} else {
			label = "Broad overlapping decomposition candidate"
		}
		out = append(out, fixedStage(id, label, "broad", w, points, initial, p, gas))
	}
	for i, w := range main {
		gas := ""
		if w.Peak != nil {
			gas = gasAtTime(valveRows, p.ValveDelayS, *w.Peak)
		}
		out = append(out, fixedStage(fmt.Sprintf("stage:main-%d", i+1), "Primary DTG peak candidate", "main", w, points, initial, p, gas))
		if len(broad) > 0 && i == 0 {
			for _, b := range broad {
				if w.Start >= b.Start && w.End <= b.End {
					if split := localMaxDTG(points, b); split != nil {
						w1 := runWindow{Start: b.Start, End: *split + 60, Peak: &[]float64{b.Start + 120}[0]}
						w2 := runWindow{Start: *split, End: b.End, Peak: &[]float64{*split + 60}[0]}
						out = append(out,
							fixedStage("stage:overlap-fit-a", "Overlapping peak fit A (legal alternative)", "overlap_fit", w1, points, initial, p, "N2"),
							fixedStage("stage:overlap-fit-b", "Overlapping peak fit B (legal alternative)", "overlap_fit", w2, points, initial, p, "CO2"),
						)
						for x := range out {
							if out[x].ID == "stage:overlap-fit-a" {
								out[x].OverlapsWith = []string{"stage:overlap-fit-b"}
							}
							if out[x].ID == "stage:overlap-fit-b" {
								out[x].OverlapsWith = []string{"stage:overlap-fit-a"}
							}
						}
					}
				}
			}
		}
	}
	for _, g := range gaps {
		out = append(out, StageCandidate{
			ID: "rejected:" + g.ID, RevisionID: "rejected:" + g.ID + ":rev1", Kind: "rejected_gap", Label: "Candidate crossing observed sampling gap",
			StartS: g.StartS, EndS: g.EndS, Status: "rejected",
			RejectionReasons: []string{"mass and DTG are not interpolated across a sampling gap", "no unique stage boundary can be supported without new observations"},
			SourceIDs:        []string{"sample_points:v1"},
			Deps:             map[string]string{"source:sample_points": "sample_points:v1", "principle:no-cross-gap": "no interpolation"},
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].StartS == out[j].StartS {
			return out[i].ID < out[j].ID
		}
		return out[i].StartS < out[j].StartS
	})
	return out
}

func latestInitialMass(st *ProjectState) (float64, string, []SourceRef, []Diagnostic) {
	b, ok := latestBatch(st, "initial_mass")
	if !ok {
		return 0, "", nil, []Diagnostic{{Code: "INITIAL_MASS_MISSING", Severity: "error", Message: "residue percentage cannot be normalized without an accepted initial mass"}}
	}
	rows, diags := parseBatchRows(b)
	for _, r := range rows {
		if v, ok := get(r, "initial_mass_mg"); ok && v > 0 {
			return f3(v), b.ID, ref(b, r.Row, "initial_mass_mg"), diags
		}
	}
	return 0, b.ID, nil, append(diags, Diagnostic{Code: "INITIAL_MASS_INVALID", Severity: "error", Message: "accepted initial mass source has no positive convertible value; raw representation remains in place"})
}

func buildResidue(points []point, initial float64, initialID string, initialRefs []SourceRef, revision string, deps map[string]string) (ResidueResult, []Diagnostic) {
	var diags []Diagnostic
	last := point{}
	for _, p := range points {
		if p.Corrected != nil {
			last = p
		}
	}
	if last.Corrected == nil {
		return ResidueResult{}, append(diags, Diagnostic{Code: "FINAL_MASS_UNKNOWN", Severity: "error", Message: "no corrected terminal mass exists"})
	}
	final := *last.Corrected
	pct := f3(100 * final / initial)
	rid := "residue:final"
	mk := func(suffix, label, raw string, v float64, unit Unit, formula string, refs []SourceRef) ChainNode {
		return ChainNode{ID: rid + "@" + revision + ":" + suffix, Label: label, Order: 0, Value: q(rid+"@"+revision+":"+suffix, label, raw, &v, unit, "ok", refs, revision), Formula: formula, Inputs: refs}
	}
	nodes := []ChainNode{
		mk("initial", "accepted initial mass", strconv.FormatFloat(initial, 'f', 3, 64), initial, mgUnit, "latest accepted initial_mass source", initialRefs),
		mk("final", "last observed corrected mass", strconv.FormatFloat(final, 'f', 3, 64), final, mgUnit, "last non-gap corrected TG point; no extrapolation", []SourceRef{{SourceID: "sample_points", SourceKind: "sample_points"}, {SourceID: "blank_curve", SourceKind: "blank_curve"}, {SourceID: "parameter:balanceDriftMgPerMin", SourceKind: "parameter"}}),
		mk("pct", "final residue", strconv.FormatFloat(pct, 'f', 2, 64)+"%", pct, pctUnit, "100*m_final/m_initial", append(initialRefs, SourceRef{SourceID: "series:tg-corrected", SourceKind: "derived_series"})),
	}
	for i := range nodes {
		nodes[i].Order = i + 1
		if i > 0 {
			nodes[i].Children = []string{nodes[i-1].ID}
		}
	}
	return ResidueResult{
		ID: rid, RevisionID: revision, Label: "Final residual mass percentage",
		FinalMass: nodes[1].Value, InitialMass: nodes[0].Value, ResiduePct: nodes[2].Value,
		Chain: nodes, Deps: deps,
	}, diags
}

func buildAtmosphere(st *ProjectState, revision string, p Parameters) ([]AtmosphereMoment, []parsedRow, []Diagnostic) {
	b, ok := latestBatch(st, "gas_valve")
	if !ok {
		return nil, nil, []Diagnostic{{Code: "ATMOSPHERE_TIMELINE_MISSING", Severity: "error", Message: "gas valve source missing"}}
	}
	rows, diags := parseBatchRows(b)
	var moments []AtmosphereMoment
	var valid []parsedRow
	for _, r := range rows {
		t, ok1 := get(r, "time_s")
		gas := strings.TrimSpace(r.Raw["gas"])
		if !ok1 || gas == "" {
			diags = append(diags, Diagnostic{Code: "GAS_EVENT_PARTIAL", Severity: "warning", Message: "gas event fragment retained but not used in timeline normalization", Location: fmt.Sprintf("%s:row:%d", b.ID, r.Row), AffectedObjects: []string{"atmosphere-timeline"}})
			continue
		}
		valid = append(valid, r)
		moments = append(moments, AtmosphereMoment{ID: fmt.Sprintf("atmosphere:valve:%g@%s", t, revision), TimeS: t, Kind: "valve", Gas: gas, Label: gas + " valve switched", SourceIDs: []string{b.ID}, RevisionID: revision, Deps: map[string]string{"source:gas_valve": b.ID}})
		arrival := f3(t + p.ValveDelayS)
		thermal := f3(arrival + p.TemperatureLagS)
		moments = append(moments,
			AtmosphereMoment{ID: fmt.Sprintf("atmosphere:gas-arrival:%g@%s", arrival, revision), TimeS: arrival, Kind: "gas_reaction", Gas: gas, Label: gas + " reaches sample after transport delay", SourceIDs: []string{b.ID, "parameter:valveDelayS"}, RevisionID: revision, Deps: map[string]string{"source:gas_valve": b.ID, "param:valveDelayS": HashJSON(p.ValveDelayS)}},
			AtmosphereMoment{ID: fmt.Sprintf("atmosphere:thermal-response:%g@%s", thermal, revision), TimeS: thermal, Kind: "thermal_response", Gas: gas, Label: gas + " thermal response window after lag", SourceIDs: []string{b.ID, "parameter:valveDelayS", "parameter:temperatureLagS"}, RevisionID: revision, Deps: map[string]string{"source:gas_valve": b.ID, "param:valveDelayS": HashJSON(p.ValveDelayS), "param:temperatureLagS": HashJSON(p.TemperatureLagS)}},
		)
	}
	sort.SliceStable(moments, func(i, j int) bool { return moments[i].TimeS < moments[j].TimeS })
	return moments, valid, diags
}

func buildFurnace(st *ProjectState, sample RawBatch, points []point, revision string) ([]FurnaceMoment, []Diagnostic) {
	fb, ok := latestBatch(st, "furnace_program")
	if !ok {
		return nil, []Diagnostic{{Code: "FURNACE_PROGRAM_MISSING", Severity: "error", Message: "furnace program missing"}}
	}
	rows, diags := parseBatchRows(fb)
	program := map[float64]float64{}
	for _, r := range rows {
		if t, ok1 := get(r, "time_s"); ok1 {
			if v, ok2 := get(r, "program_temp_c"); ok2 {
				program[t] = v
			}
		}
	}
	var out []FurnaceMoment
	for _, pt := range points {
		pv, pok := program[pt.Time]
		if !pok || pt.SampleC == nil {
			diags = append(diags, Diagnostic{Code: "FURNACE_COORDINATE_PARTIAL", Severity: "warning", Message: "program/sample coordinate pair incomplete; no synthetic point created", Location: fmt.Sprintf("time:%g", pt.Time), AffectedObjects: []string{fmt.Sprintf("furnace:%g", pt.Time)}})
			continue
		}
		out = append(out, FurnaceMoment{ID: fmt.Sprintf("furnace:%g@%s", pt.Time, revision), TimeS: pt.Time, ProgramC: f3(pv), SampleC: *pt.SampleC, SourceIDs: []string{fb.ID, sample.ID}, RevisionID: revision, Deps: map[string]string{"source:furnace_program": fb.ID, "source:sample_points": sample.ID}})
	}
	return out, diags
}

func Evaluate(st *ProjectState, req EvaluationRequested, seq int) (Evaluation, []Diagnostic) {
	revision := req.ID
	paramID := "parameters:default"
	if len(st.ParameterHistory) > 0 {
		paramID = st.ParameterHistory[len(st.ParameterHistory)-1].ID
	}
	sampleB, sampleOK := latestBatch(st, "sample_points")
	blankB, blankOK := latestBatch(st, "blank_curve")
	if !sampleOK || !blankOK {
		return Evaluation{ID: revision, Sequence: seq, RequestedBy: req.Reason, At: req.At, Algorithm: AlgorithmVersion, ParameterID: paramID, Diagnostics: []Diagnostic{{Code: "REQUIRED_SOURCE_MISSING", Severity: "error", Message: "sample points and blank curve are required before evaluation"}}}, nil
	}
	points, pointDiags, deps := collectPoints(sampleB, blankB, RawBatch{Kind: "furnace_program"}, st.Parameters)
	if fb, ok := latestBatch(st, "furnace_program"); ok {
		points = nil
		points, pointDiags, deps = collectPoints(sampleB, blankB, fb, st.Parameters)
	}
	series, gaps := buildSeries(points, sampleB, st.Parameters, deps)
	atmosphere, valveRows, atmoDiags := buildAtmosphere(st, revision, st.Parameters)
	furnace, furnaceDiags := buildFurnace(st, sampleB, points, revision)
	initial, initialID, initialRefs, initDiags := latestInitialMass(st)
	var stages []StageCandidate
	if initial > 0 {
		stages = buildStages(points, st.Parameters, initial, valveRows, gaps)
		for i := range stages {
			stages[i].RevisionID = stages[i].ID + "@" + revision
		}
	}
	residue, residueDiags := buildResidue(points, initial, initialID, initialRefs, revision, map[string]string{"source:sample_points": deps["source:sample_points"], "source:blank_curve": deps["source:blank_curve"], "source:initial_mass": initialID, "param:balanceDriftMgPerMin": deps["param:balanceDriftMgPerMin"]})
	diags := append(append(append(append(pointDiags, atmoDiags...), furnaceDiags...), initDiags...), residueDiags...)
	ev := Evaluation{
		ID: revision, Sequence: seq, RequestedBy: req.Reason, At: req.At, Algorithm: AlgorithmVersion, ParameterID: paramID,
		ParameterHash: parameterHash(st.Parameters), SourceFingerprint: sourceFingerprint(st),
		Series: series, Gaps: gaps, Stages: stages, Residues: []ResidueResult{residue}, Atmosphere: atmosphere, Furnace: furnace,
		Diagnostics: diags, Principles: append([]PrincipleEvent(nil), st.Principles...),
		Deps: map[string]string{"sources": sourceFingerprint(st), "parameters": parameterHash(st.Parameters)},
	}
	return ev, diags
}
