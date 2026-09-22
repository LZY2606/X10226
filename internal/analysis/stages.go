package analysis

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

// peak is a DTG minimum region (mass loss is negative dM/dt).
type peak struct {
	center  float64
	start   float64
	end     float64
	centerI int
	prom    float64
	startI  int
	endI    int
	seg     int
	noisy   bool
	reasons []string
}

// findPeaks detects loss regions per segment. A boxcar-smoothed DTG (window
// in minutes) is used ONLY for region/prominence detection; stage mass values
// are read from the corrected TG itself. Overlapping regions become multiple
// legal candidates - nothing is forced into a single decomposition.
func findPeaks(dtg model.Artifact, windowMin, prom float64) []peak {
	bySeg := map[int][]model.Point{}
	for _, p := range dtg.Series {
		bySeg[p.Seg] = append(bySeg[p.Seg], p)
	}
	var segs []int
	for k := range bySeg {
		segs = append(segs, k)
	}
	sort.Ints(segs)

	var peaks []peak
	for _, si := range segs {
		ps := bySeg[si]
		if len(ps) < 3 {
			continue
		}
		sm := smooth(ps, windowMin)
		// region = contiguous samples where smoothed DTG is negative;
		// within each region find the strongest local minimum.
		i := 0
		for i < len(sm) {
			if sm[i] >= 0 {
				i++
				continue
			}
			j := i
			for j < len(sm) && sm[j] < 0 {
				j++
			}
			// split wide regions into local-minimum sub-peaks
			for _, pk := range localMinima(ps[i:j], sm[i:j], prom) {
				pk.start = ps[i+pk.startI].TMin
				pk.end = ps[i+pk.endI].TMin
				pk.center = ps[i+pk.centerI].TMin
				pk.seg = si
				peaks = append(peaks, pk)
			}
			i = j
		}
	}
	return peaks
}

func smooth(ps []model.Point, windowMin float64) []float64 {
	out := make([]float64, len(ps))
	for i := range ps {
		var sum float64
		n := 0
		for j := i; j >= 0 && ps[i].TMin-ps[j].TMin <= windowMin/2; j-- {
			sum += ps[j].Y
			n++
		}
		for j := i + 1; j < len(ps) && ps[j].TMin-ps[i].TMin <= windowMin/2; j++ {
			sum += ps[j].Y
			n++
		}
		out[i] = sum / float64(n)
	}
	return out
}

func localMinima(points []model.Point, sm []float64, prom float64) []peak {
	var out []peak
	n := len(sm)
	gMin := 0
	for i := 1; i < n; i++ {
		if sm[i] < sm[gMin] {
			gMin = i
		}
	}
	edgeMax := math.Max(sm[0], sm[n-1])
	if edgeMax-sm[gMin] < prom {
		return nil
	}
	noisy := false
	for _, p := range points {
		for _, f := range p.Flags {
			if f == "noisy" {
				noisy = true
			}
		}
	}
	mk := func(c, s0, e1 int, reasons ...string) peak {
		pk := peak{centerI: c, prom: edgeMax - sm[c], startI: s0, endI: e1, noisy: noisy,
			reasons: reasons}
		if noisy {
			pk.reasons = append(pk.reasons, "region contains non-monotonic samples; boundaries are uncertain")
		}
		return pk
	}
	out = append(out, mk(gMin, 0, n-1))
	// shoulder before the primary minimum
	for i := 1; i < gMin; i++ {
		if sm[i] <= sm[i-1] && sm[i] <= sm[i+1] && edgeMax-sm[i] >= prom {
			out = append(out, mk(i, 0, gMin, "overlapping peak: shoulder before primary minimum"))
			break
		}
	}
	// shoulder after the primary minimum
	for i := gMin + 1; i < n-1; i++ {
		if sm[i] <= sm[i-1] && sm[i] <= sm[i+1] && edgeMax-sm[i] >= prom {
			out = append(out, mk(i, gMin, n-1, "overlapping peak: shoulder after primary minimum"))
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].centerI < out[j].centerI })
	return out
}

func minSlice(xs []float64) float64 {
	m := xs[0]
	for _, x := range xs {
		if x < m {
			m = x
		}
	}
	return m
}

func massAt(series []model.Point, t float64) (float64, bool) {
	// no interpolation: nearest reported sample within the same data region
	idx := sort.Search(len(series), func(i int) bool { return series[i].TMin >= t })
	best := -1
	bestD := math.MaxFloat64
	for i := idx - 2; i <= idx+2; i++ {
		if i < 0 || i >= len(series) {
			continue
		}
		if d := math.Abs(series[i].TMin - t); d < bestD {
			bestD, best = d, i
		}
	}
	if best < 0 {
		return 0, false
	}
	return series[best].Y, true
}

func tempAt(segs []seg, t float64) (float64, bool) {
	for _, s := range segs {
		for _, p := range s.pts {
			if math.Abs(p.t-t) < 1e-9 {
				return p.temp, true
			}
		}
	}
	return 0, false
}

func stageArtifacts(in Input, tg, dtg model.Artifact, segs []seg) (model.Artifact, []model.Judgment) {
	peaks := findPeaks(dtg, in.Params.Peak.WindowMin, in.Params.Peak.Prominence)
	gasWindow := gasTimeline(in)
	gasAt := func(t float64) string {
		g := "unknown"
		for _, w := range gasWindow {
			if t >= w.ValveAtMin {
				g = w.Gas
			}
		}
		return g
	}

	a := model.Artifact{
		UID: artUID(model.ArtStages, in, tg.UID, dtg.UID), Project: in.Samples.Project,
		Kind: model.ArtStages, Alg: model.AnalysisAlgVersion,
		ParamUID: in.Params.UID, ParamsFP: paramsFingerprint(in.Params),
		Direct:    []string{tg.UID, dtg.UID, in.Params.UID},
		DependsOn: []string{tg.UID, dtg.UID},
		Status:    "current",
	}
	m0 := initialMass(in.Initial)
	var js []model.Judgment
	for i, pk := range peaks {
		st := model.Stage{
			UID: identity.Hash("stg_", a.UID, strconv.Itoa(i)),
			Index: i + 1, StartMin: pk.start, EndMin: pk.end, PeakMin: pk.center,
			GasAtStart: gasAt(pk.start), GasAtPeak: gasAt(pk.center),
			EffStartMin: pk.start + in.Params.Atmosphere.GasDelayMin,
			EffEndMin:   pk.end + in.Params.Atmosphere.GasDelayMin,
			Certain:     "certain", Status: "candidate",
			Reasons: append([]string{}, pk.reasons...),
		}
		if v, ok := dtgValueAt(dtg, pk.center, pk.seg); ok {
			st.DTGPeak = v
		}
		if t, ok := tempAt(segs, pk.center); ok {
			st.PeakTempC = t
		} else {
			st.Certain = "unknown"
			st.Reasons = append(st.Reasons, "peak temperature not measured at peak time")
		}
		if ms, ok := massAt(tg.Series, pk.start); ok {
			st.StartMassMg = ms
		}
		if me, ok := massAt(tg.Series, pk.end); ok {
			st.EndMassMg = me
		}
		st.LossMg = st.StartMassMg - st.EndMassMg
		if m0 != nil {
			lp := st.LossMg / *m0 * 100
			st.LossPct = &lp
		} else {
			st.Certain = "unknown"
			st.Reasons = append(st.Reasons, "initial mass unavailable: percent loss undefined")
		}
		if pk.noisy {
			st.Certain = "uncertain"
		}
		if touchesGap(tg.Series, pk.start, pk.end) {
			st.GapLimited = true
			if st.Certain == "certain" {
				st.Certain = "uncertain"
			}
			st.Reasons = append(st.Reasons, "stage window reaches a sampling gap; no interpolation across the gap")
		}
		a.Stages = append(a.Stages, st)
	}
	// mark overlaps explicitly and emit the domain judgment: stages may
	// overlap and their percentages are NOT required to sum to 100.
	for i := range a.Stages {
		for j := i + 1; j < len(a.Stages); j++ {
			x, y := a.Stages[i], a.Stages[j]
			if x.StartMin < y.EndMin && y.StartMin < x.EndMin {
				a.Stages[i].OverlapsWith = append(a.Stages[i].OverlapsWith, y.Index)
				a.Stages[j].OverlapsWith = append(a.Stages[j].OverlapsWith, x.Index)
				a.Stages[i].Certain = "uncertain"
				a.Stages[j].Certain = "uncertain"
				a.Stages[i].Reasons = append(a.Stages[i].Reasons, "overlaps another candidate; retained as a legal decomposition")
				a.Stages[j].Reasons = append(a.Stages[j].Reasons, "overlaps another candidate; retained as a legal decomposition")
			}
		}
	}
	if len(a.Stages) > 0 {
		var sum float64
		havePct := false
		for _, s := range a.Stages {
			if s.LossPct != nil {
				sum += *s.LossPct
				havePct = true
			}
		}
		stmt := fmt.Sprintf("candidate losses are reported independently; no normalization forces them to sum to 100%%")
		if havePct {
			stmt = fmt.Sprintf("candidate losses sum to %.2f%% of initial mass; they are not renormalized to 100%% because overlapping stages can double-count", sum)
		}
		js = append(js, model.Judgment{
			Code: "stages.no_hundred_percent", Severity: "info", Subject: a.UID,
			Statement: stmt, Rule: "stages.independent_sums.v1",
		})
	}
	if in.Params.Atmosphere.GasDelayMin != 0 || in.Params.Atmosphere.ThermalLagMin != 0 {
		js = append(js, model.Judgment{
			Code: "atmosphere.delay", Severity: "info", Subject: a.UID,
			Statement: fmt.Sprintf("valve switch time is not reaction time: gas front delayed %.2g min, thermal lag %.2g min",
				in.Params.Atmosphere.GasDelayMin, in.Params.Atmosphere.ThermalLagMin),
			Rule: "atmosphere.delay.v1",
		})
	}
	return a, js
}

func touchesGap(series []model.Point, t0, t1 float64) bool {
	for _, p := range series {
		for _, f := range p.Flags {
			if f == "gap_edge" && p.TMin >= t0-1e-9 && p.TMin <= t1+1e-9 {
				return true
			}
		}
	}
	return false
}

func dtgValueAt(dtg model.Artifact, t float64, seg int) (float64, bool) {
	// most negative point in the same segment within 0.6 min
	best := 0.0
	found := false
	for _, p := range dtg.Series {
		if p.Seg != seg {
			continue
		}
		if math.Abs(p.TMin-t) <= 0.6 && (!found || p.Y < best) {
			best, found = p.Y, true
		}
	}
	return best, found
}

func initialMass(lv *model.LayerVersion) *float64 {
	if lv == nil {
		return nil
	}
	// newest usable record (versions contain the revised value)
	for i := len(lv.Records) - 1; i >= 0; i-- {
		r := lv.Records[i]
		if r.OK {
			if v, ok := r.Parsed["initial_mg"]; ok {
				return &v
			}
		}
	}
	return nil
}
