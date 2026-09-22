package derive

import (
	"math"
	"sort"

	"tga-archive/internal/domain"
)

func buildCandidates(pts []Point, peaks []Peak, in Inputs, p domain.Params, basis Basis, paramVersion string) []StageCandidate {
	// group peaks by cluster
	clusters := map[int][]Peak{}
	for _, pk := range peaks {
		clusters[pk.Cluster] = append(clusters[pk.Cluster], pk)
	}
	keys := make([]int, 0, len(clusters))
	for k := range clusters {
		keys = append(keys, k)
	}
	sort.Ints(keys)

	var cands []StageCandidate
	srcIDs := sourceIDs(in)
	for _, ck := range keys {
		ps := clusters[ck]
		// Legal candidate A: every peak as its own stage (overlapping allowed).
		var memberKeys []string
		for j, pk := range ps {
			key := stageKey(ck, "p", j)
			_ = key
			memberKeys = append(memberKeys, key)
			win := windowAround(pts, pk.At, p.StageMarginS)
			c := candidateFromWindow(pts, key, ck, "single", win, pk, basis, in, p)
			c.SourceIDs = append([]string{}, srcIDs...)
			cands = append(cands, c)
		}
		// Legal candidate B: merged stage for the whole cluster.
		if len(ps) > 1 {
			mergedKey := stageKey(ck, "m", 0)
			win := windowAroundPeaks(pts, ps, p.StageMarginS)
			// the dominant (deepest) peak represents the merged candidate
			main := ps[0]
			for _, pk := range ps[1:] {
				if pk.DTG.Value < main.DTG.Value {
					main = pk
				}
			}
			m := candidateFromWindow(pts, mergedKey, ck, "merged", win, main, basis, in, p)
			m.MemberKeys = memberKeys
			m.Exclusives = memberKeys
			m.SourceIDs = append([]string{}, srcIDs...)
			cands = append(cands, m)
			// link overlaps/exclusives both ways
			for i := range cands {
				if contains(memberKeys, cands[i].Key) {
					cands[i].Exclusives = append(cands[i].Exclusives, mergedKey)
				}
			}
			// record temporal overlap between single stages (shoulder peaks)
			for a := 0; a < len(ps); a++ {
				for b := a + 1; b < len(ps); b++ {
					ka, kb := memberKeys[a], memberKeys[b]
					idxA, idxB := indexOfKey(cands, ka), indexOfKey(cands, kb)
					cands[idxA].Overlaps = append(cands[idxA].Overlaps, kb)
					cands[idxB].Overlaps = append(cands[idxB].Overlaps, ka)
				}
			}
		}
	}
	assignIDs(cands, paramVersion)
	return cands
}

func stageKey(cluster int, kind string, idx int) string {
	return "c" + itoa(cluster) + "_" + kind + itoa(idx)
}

type segWindow struct{ start, end, peak int }

func windowAround(pts []Point, at int, margin float64) segWindow {
	peakT := pts[at].Time.Value
	start, end := at, at
	for start > 0 && pts[start-1].DTG.Valid && !pts[start].GapBefore &&
		peakT-pts[start-1].Time.Value <= margin {
		start--
		if pts[start].DTG.Value >= 0 {
			break
		}
	}
	// expand while the curve is still descending (DTG<0) within same segment
	for start > 0 && pts[start].GapBefore == false && pts[start-1].DTG.Valid && pts[start-1].DTG.Value < 0 {
		start--
	}
	for end < len(pts)-1 && pts[end+1].DTG.Valid &&
		pts[end+1].Time.Value-peakT <= margin {
		end++
		if pts[end].DTG.Value >= 0 {
			break
		}
	}
	for end < len(pts)-1 && pts[end+1].DTG.Valid && pts[end+1].DTG.Value < 0 && !pts[end+1].GapBefore {
		end++
	}
	return segWindow{start, end, at}
}

func windowAroundPeaks(pts []Point, peaks []Peak, margin float64) segWindow {
	w0 := windowAround(pts, peaks[0].At, margin)
	w1 := windowAround(pts, peaks[len(peaks)-1].At, margin)
	w := segWindow{w0.start, w1.end, peaks[0].At}
	if peaks[len(peaks)-1].DTG.Value < peaks[0].DTG.Value {
		w.peak = peaks[len(peaks)-1].At
	}
	return w
}

func candidateFromWindow(pts []Point, key string, cluster int, kind string, w segWindow, pk Peak, basis Basis, in Inputs, p domain.Params) StageCandidate {
	s, e := pts[w.start], pts[w.end]
	c := StageCandidate{
		Key:       key,
		Cluster:   cluster,
		Kind:      kind,
		Status:    "primary",
		StartTime: s.Time,
		EndTime:   e.Time,
		StartTemp: s.Temp,
		EndTemp:   e.Temp,
		PeakTime:  pk.Time,
		PeakTemp:  pk.Temp,
		PeakDTG:   pk.DTG,
		MassStart: s.MassCorr,
		MassEnd:   e.MassCorr,
	}
	loss := s.MassCorr.Value - e.MassCorr.Value
	c.LossMg = measure(loss, domain.UnitMg)
	if basis.Mass.Valid && basis.Mass.Value != 0 {
		c.LossPct = domain.Measure{Value: loss / basis.Mass.Value * 100, Unit: "%", Valid: true}
	} else {
		c.LossPct = invalid("%")
		c.Unknown = true
	}
	if kind == "merged" {
		c.Status = "alternative"
	}
	c.Chain = []ChainStep{
		{Step: 1, Label: "window_start_corrected_mass", Value: c.MassStart, Formula: "m_corr(t0)", Inputs: []string{sourcesLayer(in, domain.LayerSamples), sourcesLayer(in, domain.LayerDrift), sourcesLayer(in, domain.LayerBlank)}},
		{Step: 2, Label: "window_end_corrected_mass", Value: c.MassEnd, Formula: "m_corr(t1)", Inputs: []string{sourcesLayer(in, domain.LayerSamples), sourcesLayer(in, domain.LayerDrift), sourcesLayer(in, domain.LayerBlank)}},
		{Step: 3, Label: "mass_loss", Value: c.LossMg, Formula: "m_corr(t0)-m_corr(t1)", Inputs: []string{key + "#step1", key + "#step2"}},
		{Step: 4, Label: "mass_loss_percent", Value: c.LossPct, Formula: "loss/m0*100 (stages may overlap; percentages are NOT forced to sum 100)", Inputs: []string{key + "#step3", sourcesLayer(in, domain.LayerInitialMass)}},
	}
	if gapInside(pts, w) {
		c.RejectReason = append(c.RejectReason, "declared_gap_inside_window: window reported but endpoints not interpolated")
	}
	return c
}

func gapInside(pts []Point, w segWindow) bool {
	for i := w.start + 1; i <= w.end; i++ {
		if pts[i].GapBefore || !pts[i].DTG.Valid {
			return true
		}
	}
	return false
}

func assignIDs(cands []StageCandidate, paramVersion string) {
	for i := range cands {
		c := &cands[i]
		c.ID = domain.HashID("stage", AlgorithmVersion, paramVersion, c.Key,
			c.StartTime.Value, c.EndTime.Value, c.PeakTime.Value, c.LossMg.Value)
	}
}

func sourceIDs(in Inputs) []string {
	var out []string
	add := func(sp *domain.SourcePayload) {
		if sp != nil {
			out = append(out, sp.SourceID)
		}
	}
	add(in.Samples)
	add(in.Drift)
	add(in.Furnace)
	add(in.Valves)
	add(in.Blank)
	add(in.InitialMass)
	return out
}

func sourcesLayer(in Inputs, layer string) string {
	switch layer {
	case domain.LayerSamples:
		if in.Samples != nil {
			return in.Samples.SourceID
		}
	case domain.LayerDrift:
		if in.Drift != nil {
			return in.Drift.SourceID
		}
	case domain.LayerFurnace:
		if in.Furnace != nil {
			return in.Furnace.SourceID
		}
	case domain.LayerGas:
		if in.Valves != nil {
			return in.Valves.SourceID
		}
	case domain.LayerBlank:
		if in.Blank != nil {
			return in.Blank.SourceID
		}
	case domain.LayerInitialMass:
		if in.InitialMass != nil {
			return in.InitialMass.SourceID
		}
	}
	return "missing"
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func indexOfKey(cs []StageCandidate, key string) int {
	for i := range cs {
		if cs[i].Key == key {
			return i
		}
	}
	return -1
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [12]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

func unknownFinalResidue(_ []Point) Residue {
	return Residue{ID: "res:final:unknown", Label: "final_residue", At: invalid(domain.UnitSecond), Temp: invalid(domain.UnitKelvin), MassCorr: invalid(domain.UnitMg), Percent: invalid("%"), Unknown: true,
		Chain: []ChainStep{{Step: 1, Label: "final_residue", Value: invalid(domain.UnitMg), Note: "cannot compute: raw samples missing"}}}
}

var _ = math.NaN
