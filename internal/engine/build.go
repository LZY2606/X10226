package engine

import (
	"encoding/json"
	"fmt"
	"tgareview/internal/domain"
	"time"
)

// Sources is the resolved, versioned input set for a build.
type Sources struct {
	Sample      *domain.Source
	Blank       *domain.Source
	Drift       *domain.Source
	Program     *domain.Source
	Gas         *domain.Source
	InitialMass *domain.Source
}

// Build runs the full layered analysis deterministically.
func Build(p Params, src Sources, at time.Time) (*Report, error) {
	if src.Sample == nil {
		return nil, fmt.Errorf("sample series source is required")
	}
	var sample domain.SampleSeries
	if err := json.Unmarshal(src.Sample.Payload, &sample); err != nil {
		return nil, fmt.Errorf("sample payload: %w", err)
	}
	r := &Report{
		ProjectID:         src.Sample.ProjectID,
		Algo:              AlgoVersion,
		ParamFP:           p.Fingerprint(),
		At:                at,
		InitialMassStatus: StatusUnknown,
		SampleSrc:         src.Sample.ID,
	}
	add := func(s *domain.Source) {
		if s != nil {
			r.Sources = append(r.Sources, s.ID)
		}
	}
	add(src.Sample)
	add(src.Blank)
	add(src.Drift)
	add(src.Program)
	add(src.Gas)
	add(src.InitialMass)
	r.BlankSrc = idOrEmpty(src.Blank)
	r.DriftSrc = idOrEmpty(src.Drift)
	r.ProgramSrc = idOrEmpty(src.Program)
	r.GasSrc = idOrEmpty(src.Gas)
	r.InitialMassSrc = idOrEmpty(src.InitialMass)

	// 1. Resolve initial mass (raw layer preserved inside InitialMass).
	initMg := resolveInitialMass(src.InitialMass, r)
	if initMg != nil {
		r.InitialMassMg = initMg
		r.InitialMassStatus = StatusOK
	}

	// 2. Normalize sample points and build raw timeline entries.
	entries := make([]entry, 0, len(sample.Points))
	for _, pt := range sample.Points {
		e := entry{idx: pt.Index, rawM: pt.M.Text, rawT: pt.T.Text}
		if pt.T.OK {
			e.tOK, e.t = true, pt.T.Value
		} else {
			r.Notes = append(r.Notes, ReportNote{"warning", "time_unconvertible",
				fmt.Sprintf("point %d kept without time: %s", pt.Index, pt.T.Diag)})
		}
		if pt.FurnaceT.OK {
			v := pt.FurnaceT.Value - p.TempLagC
			e.furn = &v
		}
		if pt.SampleT.OK {
			v := pt.SampleT.Value
			e.samp = &v
		}
		if pt.M.OK {
			v := pt.M.Value
			e.mg = &v
		} else {
			r.Notes = append(r.Notes, ReportNote{"warning", "mass_unconvertible",
				fmt.Sprintf("point %d kept without mass: %s", pt.Index, pt.M.Diag)})
		}
		entries = append(entries, e)
	}

	// 3. Split into gap segments. Consecutive valid-time points are split
	//    when the interval exceeds gap_factor * median interval, or when a
	//    bad row sits between them. Derived values never cross a segment.
	type seg struct{ from, to int }
	segs := splitSegments(entries, p.GapFactor)
	segOf := make([]int, len(entries))
	for i := range segOf {
		segOf[i] = -1
	}
	for si, s := range segs {
		for i := s.from; i <= s.to; i++ {
			segOf[i] = si
		}
	}
	for gi := 0; gi < len(segs)-1; gi++ {
		a, b := entries[segs[gi].to], entries[segs[gi+1].from]
		r.Gaps = append(r.Gaps, GapSegment{
			UID:       fmt.Sprintf("%s-%d", domain.UIDGap, gi),
			Index:     gi + 1,
			AfterIdx:  a.idx,
			BeforeIdx: b.idx,
			StartT:    a.t,
			EndT:      b.t,
			DurationS: b.t - a.t,
		})
	}

	// 4. Blank and drift correction (piecewise linear over valid rows only).
	blankPts := blankInterp(src.Blank, r)
	driftPts := driftInterp(src.Drift, r)
	corrected := make([]*float64, len(entries))
	blankVals := make([]*float64, len(entries))
	driftVals := make([]*float64, len(entries))
	for i, e := range entries {
		if e.mg == nil {
			continue
		}
		v := *e.mg
		if p.BlankApplied {
			if bv, ok := interpAt(blankPts, e.t); ok {
				blankVals[i] = &bv
				v -= bv
			} else if src.Blank != nil {
				r.Notes = append(r.Notes, ReportNote{"info", "blank_unavailable",
					fmt.Sprintf("point %d outside blank coverage; left uncorrected", e.idx)})
			}
		}
		if p.DriftApplied {
			if dv, ok := interpAt(driftPts, e.t); ok {
				driftVals[i] = &dv
				v -= dv
			} else if src.Drift != nil {
				r.Notes = append(r.Notes, ReportNote{"info", "drift_unavailable",
					fmt.Sprintf("point %d outside drift coverage; left uncorrected", e.idx)})
			}
		}
		corrected[i] = &v
	}

	// 5. Percent of initial mass.
	pct := make([]*float64, len(entries))
	if initMg != nil && *initMg != 0 {
		for i, cv := range corrected {
			if cv != nil {
				v := *cv / *initMg * 100
				pct[i] = &v
			}
		}
	} else {
		r.Notes = append(r.Notes, ReportNote{"warning", "initial_mass_unknown",
			"residue percentages are unknown until initial mass is supplied"})
	}

	// 6. DTG within segments (central difference, never across gaps).
	dtg := make([]*float64, len(entries))
	for _, s := range segs {
		for i := s.from; i <= s.to; i++ {
			if pct[i] == nil || !entries[i].tOK {
				continue
			}
			if i-1 >= s.from && i+1 <= s.to &&
				pct[i-1] != nil && pct[i+1] != nil &&
				entries[i-1].tOK && entries[i+1].tOK {
				dt := (entries[i+1].t - entries[i-1].t) / 60.0
				if dt > 0 {
					v := (*pct[i+1] - *pct[i-1]) / dt
					dtg[i] = &v
				}
			}
		}
	}

	// 7. Non-monotonic noise flags (upward runs against an overall loss).
	nonMono := make([]bool, len(entries))
	for _, s := range segs {
		run := 0
		for i := s.from + 1; i <= s.to; i++ {
			if corrected[i] != nil && corrected[i-1] != nil &&
				*corrected[i] > *corrected[i-1] {
				run++
				if run >= p.NoiseRunMax {
					for j := i - run + 1; j <= i; j++ {
						nonMono[j] = true
					}
					r.Notes = append(r.Notes, ReportNote{"info", "non_monotonic_noise",
						fmt.Sprintf("points %d-%d rise against overall loss", entries[i-run+1].idx, entries[i].idx)})
				}
			} else {
				run = 0
			}
		}
	}

	// 8. Assemble synchronized timeline.
	for i, e := range entries {
		tp := TimelinePoint{
			UID:         fmt.Sprintf("%s-%d", domain.UIDPoint, e.idx),
			Index:       e.idx,
			SegIndex:    segOf[i] + 1,
			T:           e.t,
			FurnaceTC:   e.furn,
			SampleTC:    e.samp,
			RawMg:       e.mg,
			BlankMg:     blankVals[i],
			DriftMg:     driftVals[i],
			CorrectedMg: corrected[i],
			Pct:         pct[i],
			DTGPctMin:   dtg[i],
			NonMono:     nonMono[i],
			RawMText:    e.rawM,
			RawTText:    e.rawT,
		}
		if !e.tOK {
			tp.SegIndex = 0
		}
		r.Timeline = append(r.Timeline, tp)
	}

	// 9. Furnace program and gas valve timeline.
	r.Program = buildProgram(src.Program, p.TempLagC)
	r.Switches = buildSwitches(src.Gas, p.AtmDelayS)

	// 10. Stage candidates and residues.
	stageDefs := findStages(entries, pct, dtg, nonMono, segs, p, r)
	r.Stages = stageDefs.stages
	r.Rejected = stageDefs.rejected
	r.Residues = buildResidues(stageDefs, entries, pct, corrected, src, p)

	return r, nil
}

func idOrEmpty(s *domain.Source) string {
	if s == nil {
		return ""
	}
	return s.ID
}

func interpAt(pts [][2]float64, t float64) (float64, bool) {
	if len(pts) == 0 {
		return 0, false
	}
	if t <= pts[0][0] {
		return pts[0][1], true
	}
	if t >= pts[len(pts)-1][0] {
		return pts[len(pts)-1][1], true
	}
	for i := 0; i+1 < len(pts); i++ {
		if t >= pts[i][0] && t <= pts[i+1][0] {
			x0, y0 := pts[i][0], pts[i][1]
			x1, y1 := pts[i+1][0], pts[i+1][1]
			if x1 == x0 {
				return y0, true
			}
			return y0 + (y1-y0)*(t-x0)/(x1-x0), true
		}
	}
	return 0, false
}
