package engine

import (
	"fmt"
	"math"
	"sort"

	"tga-review/internal/ident"
	"tga-review/internal/types"
)

type stage struct {
	ID                 string
	OnsetT, EndsetT    float64
	OnsetC, EndsetC    float64
	PeakT, PeakC       float64
	PeakDTG            float64
	MassLossPct        float64
	Bounds             [2]float64 // onset/endset uncertainty
	OverlapsWith       []string
	Ambiguous          bool
	Alternatives       []types.Alternative
	RejectReasons      []string
	GasAtReaction      string
	ValveEvidence      []string
}

// analyzeInitialMass resolves the stratified initial-mass record:
// tare + gross give net; a net_revision overrides and must be explained by a
// new initial_mass source version. Then TG % is produced.
func (w *work) analyzeInitialMass() {
	n := w.n
	chain := w.chain("initial_mass")
	var ref float64
	basis := "mean_of_first_points"
	tare, hasTare := n.layer("tare")
	gross, hasGross := n.layer("gross")
	rev, hasRev := n.layer("net_revision")

	switch {
	case hasTare && hasGross:
		ref = gross.Mass - tare.Mass
		basis = "gross_minus_tare"
		chain.add("subtract", map[string]string{
			"gross_mg": fragRef(w.src(types.InitialMass), gross.Frag),
			"tare_mg":  fragRef(w.src(types.InitialMass), tare.Frag),
		}, fmt.Sprintf("%.4f mg", ref), units_mg,
			"net initial mass = gross - tare")
	case hasGross:
		ref = gross.Mass
		basis = "gross_only"
	default:
		ref = w.initialRefGuess()
		basis = "mean_of_first_points"
	}
	if hasRev {
		old := ref
		ref = rev.Mass
		basis = "net_revision"
		chain.add("revise", map[string]string{
			"previous_mg": fmt.Sprintf("%.4f", old),
			"revision_mg": fragRef(w.src(types.InitialMass), rev.Frag),
		}, fmt.Sprintf("%.4f mg", ref), units_mg,
			"initial mass revised from stratified net_revision layer")
	}
	w.initialMass = ref
	w.initialBasis = basis

	// TG percent series.
	tg := types.Series{ID: w.run.ID + ":tg", Name: "TG", XUnit: units_c, YUnit: units_pct}
	for i, p := range w.corr {
		pct := math.NaN()
		if ref > 0 {
			pct = p.CorrMass / ref * 100
		}
		tg.Points = append(tg.Points, types.Point{
			ID:    ident.PointID(w.run.ID, "tg", i),
			T:     p.T,
			X:     p.Temp,
			Y:     pct,
			Gap:   p.Gap,
			Flags: p.NonMonotonicFlag(),
		})
	}
	w.tgSeries = tg
}

func (w *work) src(kind string) *types.SourceVersion { return w.in.Versions[kind] }

func fragRef(v *types.SourceVersion, localID int) string {
	if v == nil {
		return fmt.Sprintf("fragment:%d", localID)
	}
	return fmt.Sprintf("%s#frag%d", v.ID, localID)
}

func (p corrPoint) NonMonotonicFlag() string {
	if p.NonMonotonic {
		return "nonmonotonic"
	}
	return ""
}

type chainBuilder struct{ w *work; stage string }

func (w *work) chain(stage string) chainBuilder { return chainBuilder{w: w, stage: stage} }

func (c chainBuilder) add(op string, inputs map[string]string, output, unit, note string) types.ChainStep {
	n := len(c.w.steps[c.stage])
	s := types.ChainStep{
		StepID: ident.ChainStepID(c.w.run.ID, c.stage, n),
		Op:     op, Inputs: inputs, Output: output, Unit: unit, Note: note,
	}
	for _, v := range inputs {
		s.Refs = append(s.Refs, v)
	}
	c.w.steps[c.stage] = append(c.w.steps[c.stage], s)
	return s
}

// detectStages finds loss stages from DTG troughs, merging unresolved close
// troughs as legal alternatives (overlapping peaks never forced to 100%).
func (w *work) detectStages() []stage {
	pts := w.corr
	dtg := w.dtgSeries.Points
	minDTG := -w.p["min_dtg_pct_min"]
	prom := w.p["peak_prominence_pctmin"]
	minSpan := w.p["min_stage_span_c"]

	var troughs []int
	for i := 1; i < len(dtg)-1; i++ {
		if dtg[i].Flags == "gap:no_derivative" ||
			(containsTag(dtg[i].Flags, "gap:no_derivative")) {
			continue
		}
		if dtg[i].Y <= minDTG && dtg[i].Y <= dtg[i-1].Y && dtg[i].Y <= dtg[i+1].Y {
			troughs = append(troughs, i)
		}
	}

	var stages []stage
	used := map[int]bool{}
	for _, idx := range troughs {
		if used[idx] {
			continue
		}
		// Merge nearby troughs into an ambiguous overlapping-peak cluster.
		cluster := []int{idx}
		for _, j := range troughs {
			if j == idx || used[j] {
				continue
			}
			if math.Abs(dtg[j].Y-dtg[idx].Y) < prom &&
				math.Abs(pts[j].Temp-pts[idx].Temp) < 3*minSpan {
				cluster = append(cluster, j)
			}
		}
		sort.Ints(cluster)
		for _, c := range cluster {
			used[c] = true
		}
		stageNum := len(stages) + 1
		id := fmt.Sprintf("stage-%d", stageNum)

		lo, hi := stageBounds(pts, dtg, cluster[0], cluster[len(cluster)-1])
		st := stage{
			ID:       id,
			OnsetT:   pts[lo].T, EndsetT: pts[hi].T,
			OnsetC: pts[lo].Temp, EndsetC: pts[hi].Temp,
		}
		pk := cluster[0]
		for _, c := range cluster {
			if dtg[c].Y < dtg[pk].Y {
				pk = c
			}
		}
		st.PeakT, st.PeakC, st.PeakDTG = pts[pk].T, pts[pk].Temp, dtg[pk].Y

		// Mass loss: nearest accepted points at the two edges, not interpolated
		// across a gap.
		loss, lossNote, alt := w.stageMassLoss(id, lo, hi, cluster, minSpan)
		st.MassLossPct = loss
		if lossNote != "" {
			st.RejectReasons = append(st.RejectReasons, lossNote)
			st.Ambiguous = true
		}
		st.Alternatives = alt
		if len(cluster) > 1 {
			st.Ambiguous = true
			for _, c := range cluster {
				st.Alternatives = append(st.Alternatives, types.Alternative{
					StageID: id,
					Value:   -dtg[c].Y,
					Unit:    units_pctm,
					Reason:  fmt.Sprintf("overlapping DTG trough at %.1f degC", pts[c].Temp),
				})
			}
		}
		if st.EndsetC-st.OnsetC < minSpan {
			st.RejectReasons = append(st.RejectReasons,
				fmt.Sprintf("stage width %.2f degC below minimum %.2f", st.EndsetC-st.OnsetC, minSpan))
		}
		st.Bounds = [2]float64{st.OnsetC, st.EndsetC}
		stages = append(stages, st)
	}

	// Record overlaps (stages whose temperature spans intersect).
	for i := range stages {
		for j := i + 1; j < len(stages); j++ {
			a, b := stages[i], stages[j]
			if a.OnsetC <= b.EndsetC && b.OnsetC <= a.EndsetC {
				stages[i].OverlapsWith = append(stages[i].OverlapsWith, b.ID)
				stages[j].OverlapsWith = append(stages[j].OverlapsWith, a.ID)
			}
		}
	}

	// Provenance / calculation chain for each stage.
	for i := range stages {
		s := &stages[i]
		c := w.chain(s.ID)
		c.add("find_dtg_trough", map[string]string{
			"dtg_series":   w.dtgSeries.ID,
			"min_pct_min":  fmt.Sprintf("%.3f", w.p["min_dtg_pct_min"]),
			"sample_point": fragRef(w.src(types.SampleSeries), pts[indexAtTime(pts, s.PeakT)].Frag),
		}, fmt.Sprintf("%.3f %s at %.2f %s", s.PeakDTG, units_pctm, s.PeakC, units_c),
			units_pctm, "DTG minimum; derivatives across gaps are not computed")
		c.add("stage_window", map[string]string{
			"onset_point":  fragRef(w.src(types.SampleSeries), pts[indexAtTime(pts, s.OnsetT)].Frag),
			"endset_point": fragRef(w.src(types.SampleSeries), pts[indexAtTime(pts, s.EndsetT)].Frag),
		}, fmt.Sprintf("%.2f..%.2f %s", s.OnsetC, s.EndsetC, units_c), units_c,
			"stage boundaries from DTG return to near zero; overlapping allowed")
		c.add("mass_loss", map[string]string{
			"initial_ref_mg": fmt.Sprintf("%.4f", w.initialMass),
			"basis":          w.initialBasis,
		}, fmt.Sprintf("%.3f %%", s.MassLossPct), units_pct,
			"loss uses edge points only; stages are not forced to sum to 100%")
	}
	return stages
}

func containsTag(flags, tag string) bool {
	for _, f := range splitFlags(flags) {
		if f == tag {
			return true
		}
	}
	return false
}

func splitFlags(f string) []string {
	var out []string
	cur := ""
	for _, r := range f {
		if r == ',' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func indexAtTime(pts []corrPoint, t float64) int {
	for i := range pts {
		if pts[i].T == t {
			return i
		}
	}
	return 0
}

// stageBounds walks outward from the peak cluster while DTG is below half
// depth; a gap edge terminates the bound without interpolation.
func stageBounds(pts []corrPoint, dtg []types.Point, first, last int) (int, int) {
	depth := dtg[first].Y
	for _, c := range []int{first, last} {
		if dtg[c].Y < depth {
			depth = dtg[c].Y
		}
	}
	threshold := depth / 2
	lo := first
	for lo > 0 {
		if pts[lo].Gap || dtg[lo-1].Y > threshold {
			break
		}
		lo--
	}
	hi := last
	for hi < len(pts)-1 {
		if pts[hi+1].Gap || dtg[hi+1].Y > threshold {
			break
		}
		hi++
	}
	return lo, hi
}

// stageMassLoss computes percent mass loss between edges. A gap overlapping
// the window yields alternatives for each side instead of an interpolated loss.
func (w *work) stageMassLoss(id string, lo, hi int, cluster []int, minSpan float64) (float64, string, []types.Alternative) {
	pts := w.corr
	ref := w.initialMass
	var alt []types.Alternative
	note := ""
	gapInWindow := false
	for i := lo + 1; i <= hi; i++ {
		if pts[i].Gap {
			gapInWindow = true
		}
	}
	loss := (pts[lo].CorrMass - pts[hi].CorrMass) / ref * 100
	if gapInWindow {
		note = "sampling gap inside stage window; loss is a bound, not interpolated"
		// Legal alternatives: loss assuming change happened before/after gap.
		prev := lo
		for i := lo + 1; i <= hi; i++ {
			if pts[i].Gap && prev >= 0 {
				alt = append(alt, types.Alternative{
					StageID: id,
					Value:   (pts[lo].CorrMass - pts[i-1].CorrMass) / ref * 100,
					Unit:    units_pct,
					Reason:  "all loss completed before the sampling gap",
				})
			}
			if pts[i].Gap {
				prev = i
			}
		}
		alt = append(alt, types.Alternative{
			StageID: id,
			Value:   loss,
			Unit:    units_pct,
			Reason:  "all loss observed across the window (gap treated as zero loss)",
		})
	}
	return loss, note, alt
}
