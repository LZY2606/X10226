package derive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"tga-review/internal/domain"
)

// Fingerprint hashes algorithm version and parameter set so every derived
// value can state which rule version produced it.
func Fingerprint(p Parameters) string {
	b, _ := json.Marshal(struct {
		Algo string     `json:"algo"`
		P    Parameters `json:"params"`
	}{AlgoVersion, p})
	sum := sha256.Sum256(b)
	return AlgoVersion + "-" + hex.EncodeToString(sum[:8])
}

type cpoint struct {
	index int
	t     float64
	temp  float64
	rawM  float64
	mass  float64
	good  bool
	flag  string
}

// Compute projects the current source strata into derived artifacts. It is a
// pure function: identical inputs and atSeq always yield identical output,
// and it never fills sampling gaps by interpolation.
func Compute(st *State, atSeq int64) *Derived {
	d := &Derived{Params: st.Params, AtSeq: atSeq}

	if len(st.RawPoints) == 0 {
		d.Missing = append(d.Missing, MissingSource{Layer: domain.LayerRawPoints, Reason: "no raw sample points"})
	}
	if st.InitialMass == nil || !st.InitialMass.Known {
		d.Missing = append(d.Missing, MissingSource{Layer: domain.LayerInitialMass, Reason: "initial mass unknown"})
	}
	if len(st.BlankPoints) == 0 {
		d.Missing = append(d.Missing, MissingSource{Layer: domain.LayerBlank, Reason: "no blank curve"})
	}

	var notes []string
	cp, gaps := normalizePoints(st.RawPoints, st.Params, &notes)
	cp = applyBlank(cp, st.BlankPoints, st.Drift, &notes)
	segments := assignSegments(cp, gaps)
	series := buildSeries(segments, gaps, st, atSeq)
	series.Notes = notes

	d.Missing = appendMissingPoints(d.Missing, st.RawPoints)
	if len(series.Points) >= 2 && st.InitialMass != nil && st.InitialMass.Known {
		d.Series = series
		d.Candidates = findCandidates(series, st, atSeq)
		d.Residue = buildResidue(series, st, st.AppliedCandidates(), atSeq)
	} else {
		d.Series = series
	}
	return d
}

func normalizePoints(raw []domain.RawPoint, p Parameters, notes *[]string) ([]cpoint, []domain.Gap) {
	var cp []cpoint
	for _, rp := range raw {
		c := cpoint{index: rp.Index}
		if rp.T.Converted {
			c.t = rp.T.Value
		}
		if rp.Temp.Converted {
			c.temp = rp.Temp.Value
		}
		if rp.M.Converted {
			c.rawM, c.mass, c.good = rp.M.Value, rp.M.Value, true
		}
		if rp.T.Diagnostic != "" || rp.Temp.Diagnostic != "" || rp.M.Diagnostic != "" {
			c.flag = "raw-diagnostic"
		}
		cp = append(cp, c)
	}
	if len(cp) < 2 {
		return cp, nil
	}

	dts := make([]float64, 0, len(cp)-1)
	for i := 1; i < len(cp); i++ {
		if dt := cp[i].t - cp[i-1].t; dt > 0 {
			dts = append(dts, dt)
		}
	}
	sorted := append([]float64(nil), dts...)
	sort.Float64s(sorted)
	median := 0.0
	if len(sorted) > 0 {
		median = sorted[len(sorted)/2]
	}

	var gaps []domain.Gap
	for i := 1; i < len(cp); i++ {
		dt := cp[i].t - cp[i-1].t
		if median > 0 && dt > median*p.GapMultiple {
			gaps = append(gaps, domain.Gap{StartS: cp[i-1].t, EndS: cp[i].t, Reason: "sample gap; never interpolated"})
		}
	}

	// Non-monotone noise (mass upticks) is retained on the raw curve and
	// flagged rather than silently deleted.
	for i := 1; i < len(cp); i++ {
		if cp[i].good && cp[i-1].good && cp[i].t > cp[i-1].t {
			if dm := cp[i].rawM - cp[i-1].rawM; dm > 0 && dm/cp[i-1].rawM > 0.0005 {
				cp[i].flag = "non-monotone"
				*notes = append(*notes, fmt.Sprintf("non-monotone mass at t=%.1fs kept and flagged", cp[i].t))
			}
		}
	}
	return cp, gaps
}

type blankPoint struct{ t, m float64 }

// applyBlank removes the zero-aligned blank reading at each sample time. The
// blank is interpolated only inside its measured span; a sample outside the
// blank span is never extrapolated.
func applyBlank(cp []cpoint, blank []domain.RawPoint, drift *DriftSpec, notes *[]string) []cpoint {
	if drift == nil || drift.Model != "blank_zeroed" {
		return cp
	}
	type bp struct{ t, m float64 }
	var bps []blankPoint
	var b0 float64
	for i, b := range blank {
		if b.T.Converted && b.M.Converted {
			if i == 0 {
				b0 = b.M.Value
			}
			bps = append(bps, blankPoint{b.T.Value, b.M.Value})
		}
	}
	if len(bps) < 2 {
		*notes = append(*notes, "blank not usable; raw mass used")
		return cp
	}
	for i := range cp {
		if !cp[i].good {
			continue
		}
		offset, inside := blankAt(bps, cp[i].t, b0)
		if !inside {
			*notes = append(*notes, fmt.Sprintf("blank does not cover t=%.1fs; point left uncorrected", cp[i].t))
			continue
		}
		cp[i].mass = cp[i].rawM - offset
		cp[i].flag = "blank-corrected"
	}
	return cp
}

func blankAt(bps []blankPoint, t, b0 float64) (float64, bool) {
	if t < bps[0].t || t > bps[len(bps)-1].t {
		return 0, false
	}
	for i := 1; i < len(bps); i++ {
		if t <= bps[i].t {
			a, b := bps[i-1], bps[i]
			f := 0.0
			if b.t != a.t {
				f = (t - a.t) / (b.t - a.t)
			}
			return (a.m + f*(b.m-a.m)) - b0, true
		}
	}
	return bps[len(bps)-1].m - b0, true
}

func assignSegments(cp []cpoint, gaps []domain.Gap) [][]cpoint {
	if len(cp) == 0 {
		return nil
	}
	segments := [][]cpoint{{cp[0]}}
	for i := 1; i < len(cp); i++ {
		cross := false
		for _, g := range gaps {
			if cp[i-1].t <= g.StartS+1e-9 && cp[i].t >= g.EndS-1e-9 {
				cross = true
			}
		}
		if cross || !cp[i].good || !cp[i-1].good {
			segments = append(segments, []cpoint{cp[i]})
		} else {
			segments[len(segments)-1] = append(segments[len(segments)-1], cp[i])
		}
	}
	return segments
}

func buildSeries(segments [][]cpoint, gaps []domain.Gap, st *State, atSeq int64) *Series {
	var out []SPoint
	for sid, seg := range segments {
		sm := smoothSegment(seg, st.Params.SmoothWindowS)
		start := len(out)
		for _, c := range seg {
			sp := SPoint{Index: c.index, TS: c.t, TempC: c.temp, MassMg: c.mass, SmoothMg: 0, SegmentID: sid}
			if c.flag == "non-monotone" {
				sp.NonMonotone = true
			}
			out = append(out, sp)
		}
		// DTG only within a continuous segment: a gap is never bridged.
		for j := 1; j < len(seg)-1; j++ {
			dt := seg[j+1].t - seg[j-1].t
			if dt > 0 && st.InitialMass != nil && st.InitialMass.Known && st.InitialMass.MassMg != 0 {
				rateMgS := -(sm[j+1] - sm[j-1]) / dt // downward positive
				out[start+j].SmoothMg = sm[j]
				out[start+j].DTGPctPerS = rateMgS / st.InitialMass.MassMg * 100
				out[start+j].DTGValid = true
			} else {
				out[start+j].SmoothMg = sm[j]
			}
		}
		if len(seg) >= 1 {
			out[start].SmoothMg = sm[0]
			out[start+len(seg)-1].SmoothMg = sm[len(seg)-1]
		}
	}
	var src []string
	if v, ok := st.RawVersions[domain.LayerRawPoints]; ok {
		src = append(src, v.StableID)
	}
	if v, ok := st.RawVersions[domain.LayerBlank]; ok {
		src = append(src, v.StableID)
	}
	if v, ok := st.RawVersions[domain.LayerDrift]; ok {
		src = append(src, v.StableID)
	}
	if st.InitialMass != nil {
		src = append(src, st.InitialMass.StableID)
	}
	return &Series{
		ID: "series-1", Points: out, Gaps: gaps, Sources: unique(src),
		FP: Fingerprint(st.Params), ComputedSeq: atSeq,
	}
}

func smoothSegment(seg []cpoint, windowS float64) []float64 {
	out := make([]float64, len(seg))
	half := windowS / 2
	for i := range seg {
		var sum, w float64
		for j := range seg {
			if math.Abs(seg[j].t-seg[i].t) <= half {
				sum += seg[j].mass
				w++
			}
		}
		if w > 0 {
			out[i] = sum / w
		} else {
			out[i] = seg[i].mass
		}
	}
	return out
}

func tempAt(points []SPoint, t float64) float64 {
	if len(points) == 0 {
		return 0
	}
	for i := 1; i < len(points); i++ {
		if points[i].TS >= t {
			a, b := points[i-1], points[i]
			if b.TS == a.TS {
				return a.TempC
			}
			f := (t - a.TS) / (b.TS - a.TS)
			return a.TempC + f*(b.TempC-a.TempC)
		}
	}
	return points[len(points)-1].TempC
}

func massAt(points []SPoint, t float64) (float64, bool) {
	if len(points) == 0 {
		return 0, false
	}
	if t < points[0].TS || t > points[len(points)-1].TS {
		return 0, false
	}
	for i := 1; i < len(points); i++ {
		if points[i].TS >= t {
			// Refuse to interpolate across a segment boundary (gap).
			if points[i].SegmentID != points[i-1].SegmentID {
				return 0, false
			}
			a, b := points[i-1], points[i]
			if b.TS == a.TS {
				return a.SmoothMg, true
			}
			f := (t - a.TS) / (b.TS - a.TS)
			return a.SmoothMg + f*(b.SmoothMg-a.SmoothMg), true
		}
	}
	return points[len(points)-1].SmoothMg, true
}

func appendMissingPoints(missing []MissingSource, raw []domain.RawPoint) []MissingSource {
	for _, rp := range raw {
		if !rp.M.Converted {
			missing = append(missing, MissingSource{Layer: domain.LayerRawPoints,
				Reason: fmt.Sprintf("sample #%d mass unconvertible: %s (%s)", rp.Index, rp.M.Text, rp.M.Diagnostic)})
		}
	}
	return missing
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
