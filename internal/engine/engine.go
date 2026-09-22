// Package engine performs deterministic TGA stage decomposition. It records
// every calculation step, never interpolates across sampling gaps, keeps all
// legal alternatives when stages cannot be uniquely separated, and treats the
// atmosphere valve timeline as distinct from the sample reaction timeline.
package engine

import (
	"fmt"
	"sort"

	"tga-review/internal/ident"
	"tga-review/internal/types"
)

// Inputs bundles everything one derived run needs.
type Inputs struct {
	ProjectID string
	Seq       int
	Sources   map[string]string // kind -> source version id
	Versions  map[string]*types.SourceVersion
	Ruleset   *types.Ruleset
	At        string
}

type work struct {
	in    Inputs
	p     map[string]float64
	n     normalized
	run   *types.Run
	steps map[string][]types.ChainStep // stageID -> steps
	notes []string
}

// Derive executes one versioned calculation pass. It always returns a run;
// fatal problems are reported via run.Status=blocked, while data-quality issues
// are first-class notes/flags on the results.
func Derive(in Inputs) (*types.Run, error) {
	if in.Ruleset == nil {
		return nil, fmt.Errorf("nil ruleset")
	}
	runID, err := ident.RunID(in.ProjectID, in.Seq, in.Sources, in.Ruleset.ID)
	if err != nil {
		return nil, err
	}
	w := &work{
		in:    in,
		p:     in.Ruleset.Params,
		steps: map[string][]types.ChainStep{},
		run: &types.Run{
			ID:          runID,
			ProjectID:   in.ProjectID,
			Seq:         in.Seq,
			RulesetID:   in.Ruleset.ID,
			Sources:     copySources(in.Sources),
			AlgoVersion: AlgoVersion,
			ParamFP:     in.Ruleset.Fingerprint,
			At:          in.At,
			Series:      map[string]types.Series{},
		},
	}
	w.provOf()

	n, normDiags, err := normalize(versionBundle{byKind: in.Versions})
	if err != nil {
		w.run.Status = "blocked"
		w.run.BlockedBy = []string{err.Error()}
		return w.run, nil
	}
	w.n = n
	w.notes = append(w.notes, normDiags...)
	w.recordRejectedFragments()

	w.runSeriesAndCorrections()
	w.analyzeInitialMass()
	stages := w.detectStages()
	w.attachAtmosphere(stages)
	w.buildCandidates(stages)
	w.finalizeRun(stages)
	return w.run, nil
}

func copySources(s map[string]string) map[string]string {
	out := make(map[string]string, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

func (w *work) provOf() {
	// provenance constructed per candidate; placeholder to keep structs ready.
}

func (w *work) provenance() types.Provenance {
	return types.Provenance{
		AlgoVersion: AlgoVersion,
		ParamFP:     w.in.Ruleset.Fingerprint,
		RulesetID:   w.in.Ruleset.ID,
		RunID:       w.run.ID,
		Direct:      copySources(w.in.Sources),
	}
}

func (w *work) recordRejectedFragments() {
	for _, v := range w.in.Versions {
		for _, f := range v.Fragments {
			if f.Status == types.FragRejected {
				w.notes = append(w.notes,
					fmt.Sprintf("rejected fragment src=%s row=%d: %s",
						v.ID, f.LocalID, joinDiags(f.Diagnostics)))
			}
		}
	sort.Strings(w.notes)
	}
}

func joinDiags(d []string) string {
	if len(d) == 0 {
		return "rejected"
	}
	out := d[0]
	for _, x := range d[1:] {
		out += "; " + x
	}
	return out
}

type corrPoint struct {
	idx                    int
	T, Temp                float64
	RawMass                float64
	CorrMass               float64
	Gap, NonMonotonic      bool
	Frag                   int
}

func (w *work) runSeriesAndCorrections() {
	n := w.n

	// Raw sample series (original representation retained in fragments).
	raw := types.Series{ID: w.run.ID + ":raw", Name: "raw_sample_mass",
		XUnit: units_s, YUnit: units_mg}
	corr := types.Series{ID: w.run.ID + ":corrected", Name: "drift_blank_corrected_mass",
		XUnit: units_s, YUnit: units_mg}

	// Step 1: balance baseline drift correction (linear interpolation of the
	// drift anchors; never extrapolated beyond the anchor range).
	driftStart, driftEnd := -1.0, -1.0
	if len(n.drift) > 0 {
		driftStart, driftEnd = n.drift[0].T, n.drift[len(n.drift)-1].T
	}
	// Step 2: blank subtraction uses the blank curve anchor at each sample time.
	blankAt := func(t float64) (float64, bool) {
		if len(n.blank) == 0 {
			return 0, false
		}
		return interpAnchor(n.blank, t), true
	}

	gapMult := w.p["gap_multiple"]
	jitter := w.p["noise_jitter_mg"]

	medianDT := medianDelta(n.sample)
	var pts []corrPoint
	for i, s := range n.sample {
		var offset float64
		covered := false
		if len(n.drift) > 0 && t >= driftStart - 1e-9 && t <= driftEnd + 1e-9 {
			offset = interpDrift(n.drift, s.T)
			covered = true
		}
		bl, hasBlank := blankAt(s.T)
		cm := s.Mass - offset
		if hasBlank {
			cm -= bl
		}
		gap := i > 0 && medianDT > 0 && (s.T-n.sample[i-1].T) > medianDT*gapMult
		nm := false
		if i > 0 {
			if rise := cm - pts[i-1].CorrMass; rise > jitter {
				nm = true
			}
		}
		pt := corrPoint{idx: i, T: s.T, Temp: s.Temp, RawMass: s.Mass,
			CorrMass: cm, Gap: gap, NonMonotonic: nm, Frag: s.Frag}
		pts = append(pts, pt)

		raw.Points = append(raw.Points, types.Point{
			ID: ident.PointID(w.run.ID, "raw", i), T: s.T, X: s.T, Y: s.Mass,
			Gap: gap,
		})
		corr.Points = append(corr.Points, types.Point{
			ID: ident.PointID(w.run.ID, "corrected", i), T: s.T, X: s.T, Y: cm,
			Gap: gap,
			Flags: flagString(nm, "nonmonotonic", covered, hasBlank),
		})
	}
	w.corr = pts
	w.rawSeries = raw
	w.corrSeries = corr
	w.medianDT = medianDT

	// TG = percent of corrected mass vs. the reference initial mass (filled in
	// after initial-mass analysis, so compute there).
	w.dtgSeries = w.computeDTG(pts)
	w.programSeries = w.computeProgramSeries()
	w.gasSeries = w.computeGasSeries()
}

func flagString(nm bool, nmTag string, driftCovered, hasBlank bool) string {
	var f string
	if nm {
		f = nmTag
	}
	if !driftCovered {
		f = appendFlag(f, "drift:out_of_range")
	}
	if !hasBlank {
		f = appendFlag(f, "blank:missing")
	}
	return f
}

func appendFlag(f, tag string) string {
	if f == "" {
		return tag
	}
	return f + "," + tag
}

const (
	units_s    = "s"
	units_mg   = "mg"
	units_pct  = "%"
	units_pctm = "%/min"
	units_c    = "degC"
)

func medianDelta(sp []samplePoint) float64 {
	if len(sp) < 2 {
		return 0
	}
	dts := make([]float64, 0, len(sp)-1)
	for i := 1; i < len(sp); i++ {
		dts = append(dts, sp[i].T-sp[i-1].T)
	}
	sort.Float64s(dts)
	return dts[len(dts)/2]
}

func interpDrift(dp []driftPoint, t float64) float64 {
	return interpGeneric(len(dp), func(i int) (float64, float64) {
		return dp[i].T, dp[i].Offset
	}, t)
}

func interpAnchor(bp []blankPoint, t float64) float64 {
	return interpGeneric(len(bp), func(i int) (float64, float64) {
		return bp[i].T, bp[i].Mass
	}, t)
}

// interpGeneric linearly interpolates inside the anchor range and clamps at
// the ends; gaps are detected elsewhere, not bridged here for derived values.
func interpGeneric(n int, at func(i int) (x, y float64), x float64) float64 {
	x0, y0 := at(0)
	if x <= x0 {
		return y0
	}
	xn, yn := at(n - 1)
	if x >= xn {
		return yn
	}
	for i := 1; i < n; i++ {
		xi, yi := at(i)
		if x <= xi {
			xp, yp := at(i - 1)
			if xi == xp {
				return yi
			}
			f := (x - xp) / (xi - xp)
			return yp + f*(yi-yp)
		}
	}
	return yn
}
