package engine

import (
	"sort"

	"tga-review/internal/domain"
)

// ProgramTemperature evaluates the furnace set temperature at furnace time t
// (minutes), interpolating within ramp segments. Unknown outside coverage.
func ProgramTemperature(program []domain.ProgramSegment, tMin float64) (float64, bool) {
	type seg struct {
		t0, t1, p0, p1 float64
		isotherm       bool
	}
	var segs []seg
	for _, ps := range program {
		t0, e0 := normQ(ps.StartTime)
		t1, e1 := normQ(ps.EndTime)
		p0, e2 := normQ(ps.StartTemp)
		p1, e3 := normQ(ps.EndTemp)
		if firstErr(e0, e1, e2, e3) != nil {
			continue
		}
		segs = append(segs, seg{t0, t1, p0, p1, ps.Kind == "isothermal"})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].t0 < segs[j].t0 })
	for _, s := range segs {
		switch {
		case tMin < s.t0 || tMin > s.t1:
			continue
		case s.isotherm || s.t1 == s.t0:
			return s.p0, true
		default:
			return s.p0 + (s.p1-s.p0)*(tMin-s.t0)/(s.t1-s.t0), true
		}
	}
	return 0, false
}

// ReactionWindows shifts every gas valve event by the transport delay to reach
// the sample, then evaluates program temperature and subtracts thermal lag to
// estimate the sample temperature. Valve time is never reaction time.
func ReactionWindowsBuild(projectID, batchID string, fp domain.Fingerprint, gasVer, programVer *domain.SourceVersion, p domain.Params) (*domain.DerivedNode, error) {
	if gasVer == nil {
		return fail("no gas layer")
	}
	var windows []domain.ReactionWindow
	for _, g := range gasVer.Gases {
		if g.Action != "switch" {
			continue
		}
		tv, err := normQ(g.Time)
		if err != nil {
			return fail("gas row " + itoa(g.Row) + ": " + err.Error())
		}
		reach := tv + p.AtmosphereDelayMin
		w := domain.ReactionWindow{
			GasEventRow: g.Row, Gas: g.Gas, ValveTMin: round6(tv),
			DelayMin: p.AtmosphereDelayMin, ReachTMin: round6(reach),
			ThermalLagK: p.ThermalLagK,
		}
		if programVer != nil {
			if pt, ok := ProgramTemperature(programVer.Program, reach); ok {
				w.ProgramTK = round6(pt)
				w.SampleTK = round6(pt - p.ThermalLagK)
			}
		}
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].ReachTMin < windows[j].ReachTMin })
	payload := domain.ReactionWindowsPayload{Windows: windows}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKReactionWindow), struct {
		Gas     string
		Program string
		FP      domain.Fingerprint
		P       domain.ReactionWindowsPayload
	}{gasVer.ID, versionID(programVer), fp, payload})
	if err != nil {
		return nil, err
	}
	srcIDs := []string{gasVer.ID}
	if programVer != nil {
		srcIDs = append(srcIDs, programVer.ID)
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKReactionWindow, BatchID: batchID,
		Fingerprint: fp, SourceVersionIDs: srcIDs, Payload: payload,
	}, nil
}
