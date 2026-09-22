package engine

import (
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// SelectInitMass chooses the weighing record that denominates percentages.
// A revision of the init-mass layer only invalidates downstream results; the
// earlier choice remains visible as a stale node.
func SelectInitMass(projectID, batchID string, fp domain.Fingerprint, initVer *domain.SourceVersion, p domain.Params) (*domain.DerivedNode, error) {
	if initVer == nil || len(initVer.Masses) == 0 {
		return fail("no initial-mass records")
	}
	type rec struct {
		row    int
		t, m   float64
		ver    string
	}
	var recs []rec
	for _, im := range initVer.Masses {
		t, e1 := normQ(im.Time)
		m, e2 := normQ(im.Mass)
		if firstErr(e1, e2) != nil {
			continue
		}
		recs = append(recs, rec{im.Row, t, m, initVer.ID})
	}
	if len(recs) == 0 {
		return fail("init-mass layer has no convertible records")
	}
	sort.Slice(recs, func(i, j int) bool { return recs[i].t < recs[j].t })
	pick := 0
	reason := "first weighing record (strategy=first)"
	switch p.InitMassStrategy {
	case "last":
		pick = len(recs) - 1
		reason = "last weighing record (strategy=last)"
	case "manual":
		reason = fmt.Sprintf("manually selected row %d", p.InitMassRow)
		found := false
		for i, r := range recs {
			if r.row == p.InitMassRow {
				pick = i
				found = true
				break
			}
		}
		if !found {
			return fail(fmt.Sprintf("manual init mass row %d not present", p.InitMassRow))
		}
	}
	choices := make([]domain.InitMassChoice, len(recs))
	for i, r := range recs {
		choices[i] = domain.InitMassChoice{
			VersionID: r.ver, Row: r.row, MassMg: round6(r.m),
			Selected: i == pick,
			Reason: map[bool]string{true: reason, false: "unselected weighing record"}[i == pick],
		}
	}
	payload := domain.InitMassChoicesPayload{Choices: choices}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKInitMassChoice), struct {
		Src string
		FP  domain.Fingerprint
		P   domain.InitMassChoicesPayload
	}{initVer.ID, fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKInitMassChoice, BatchID: batchID,
		Fingerprint: fp, SourceVersionIDs: []string{initVer.ID}, Payload: payload,
	}, nil
}

// ChosenMass extracts the selected initial mass in mg.
func ChosenMass(node *domain.DerivedNode) (float64, int, string, error) {
	cp, ok := node.Payload.(domain.InitMassChoicesPayload)
	if !ok {
		return 0, 0, "", fmt.Errorf("init mass node mismatch")
	}
	for _, c := range cp.Choices {
		if c.Selected {
			return c.MassMg, c.Row, c.VersionID, nil
		}
	}
	return 0, 0, "", fmt.Errorf("no selected init mass")
}

// BuildResidue computes final residue with its fully expandable calculation
// chain. Unknown is first-class: a gap before the endpoint, missing init mass,
// or a non-monotonic terminal region yields Unknown=true with a reason instead
// of a silently fabricated percentage.
func BuildResidue(projectID, batchID string, fp domain.Fingerprint, correctedNode, gapNode, initNode *domain.DerivedNode, stageNode *domain.DerivedNode) (*domain.DerivedNode, error) {
	corr, ok := correctedNode.Payload.(domain.CorrectedPayload)
	if !ok {
		return fail("corrected node payload mismatch")
	}
	gp, ok := gapNode.Payload.(domain.GapMapPayload)
	if !ok {
		return fail("gap node payload mismatch")
	}
	if len(corr.Points) == 0 {
		return fail("no corrected points")
	}
	initMg, initRow, initVerID, err := ChosenMass(initNode)
	if err != nil {
		return fail(err.Error())
	}
	end := corr.Points[len(corr.Points)-1]

	r := domain.Residue{
		ID: "", AtTMin: round6(end.TMin), AtTK: round6(end.TK),
		ResidualMassMg: round6(end.CorrectedMg), InitialMassMg: round6(initMg),
		InitMassVersion: initVerID,
	}
	if initMg > 0 {
		r.ResidualPct = round6(end.CorrectedMg / initMg * 100)
	}

	// Unknown conditions (kept, never dropped)
	lastGapRow := -1
	for _, g := range gp.Gaps {
		if g.EndRow <= end.Row && g.StartRow > lastGapRow {
			lastGapRow = g.StartRow
		}
	}
	// terminal non-monotonic noise: last three corrected masses increase
	if len(corr.Points) >= 3 {
		a := corr.Points[len(corr.Points)-3].CorrectedMg
		b := corr.Points[len(corr.Points)-2].CorrectedMg
		c := end.CorrectedMg
		if c > b && b > a {
			r.Unknown = true
			r.UnknownReason = "terminal mass increases twice: non-monotonic noise, endpoint not stable"
		}
	}
	// error band from the terminal swing
	if len(corr.Points) >= 3 {
		mx, mn := end.CorrectedMg, end.CorrectedMg
		for i := len(corr.Points) - 3; i < len(corr.Points); i++ {
			if corr.Points[i].CorrectedMg > mx {
				mx = corr.Points[i].CorrectedMg
			}
			if corr.Points[i].CorrectedMg < mn {
				mn = corr.Points[i].CorrectedMg
			}
		}
		r.ErrorMg = round6((mx - mn) / 2)
	}

	chain := []domain.CalcChainStep{
		{Order: 1, Label: "选定初重 m0", Value: fmt.Sprintf("%.4f mg", initMg), NodeID: initNode.ID, Source: initVerID, Note: fmt.Sprintf("weighing row %d", initRow)},
		{Order: 2, Label: "终点炉时间 t_end", Value: fmt.Sprintf("%.3f min", end.TMin), Source: correctedNode.ID, Note: fmt.Sprintf("sample row %d", end.Row)},
		{Order: 3, Label: "原始终点质量", Value: fmt.Sprintf("%.4f mg", end.RawMassMg), Source: correctedNode.ID},
	}
	if end.DriftMg != 0 {
		chain = append(chain, domain.CalcChainStep{Order: 4, Label: "天平漂移扣除", Value: fmt.Sprintf("%.4f mg", -end.DriftMg), Source: correctedNode.ID, Note: "drift offset at t_end"})
	}
	if end.BlankApplied {
		chain = append(chain, domain.CalcChainStep{Order: 5, Label: "空白曲线扣除", Value: fmt.Sprintf("%.4f mg", -end.BlankMg), Source: correctedNode.ID})
	}
	chain = append(chain, domain.CalcChainStep{
		Order: 6, Label: "校正后残留质量 m_res = m_raw − drift − blank",
		Value: fmt.Sprintf("%.4f mg", end.CorrectedMg), NodeID: correctedNode.ID,
	})
	step := domain.CalcChainStep{Order: 7, Label: "残留率 = m_res / m0 × 100%", Value: fmt.Sprintf("%.3f %%", r.ResidualPct)}
	if r.Unknown {
		step.Unknown = true
		step.Note = r.UnknownReason
	}
	if lastGapRow >= 0 {
		step.Note = fmt.Sprintf("%s; last gap starts at row %d (no interpolation across it)", step.Note, lastGapRow)
	}
	chain = append(chain, step)
	r.Chain = chain

	if stageNode != nil {
		if sp, ok := stageNode.Payload.(domain.StagesPayload); ok {
			for _, st := range sp.Stages {
				if st.Status == domain.StageAccepted || st.Status == domain.StageCandidate {
					r.StageIDs = append(r.StageIDs, st.ID)
				}
			}
		}
	}

	payload := domain.ResiduesPayload{Residues: []domain.Residue{r}}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKResidue), struct {
		Corr string
		Gap  string
		Init string
		Stg  string
		FP   domain.Fingerprint
		P    domain.ResiduesPayload
	}{correctedNode.ID, gapNode.ID, initNode.ID, nodeID(stageNode), fp, payload})
	if err != nil {
		return nil, err
	}
	payload.Residues[0].ID = id
	inputs := []string{correctedNode.ID, gapNode.ID, initNode.ID}
	if stageNode != nil {
		inputs = append(inputs, stageNode.ID)
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKResidue, BatchID: batchID,
		Fingerprint: fp, InputNodeIDs: inputs, Payload: payload,
	}, nil
}

func nodeID(n *domain.DerivedNode) string {
	if n == nil {
		return ""
	}
	return n.ID
}
