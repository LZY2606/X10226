package tga

// buildResidue computes final residue and records every link of its
// calculation chain. Each link has a stable ValueID so UI numbers remain
// addressable after export/delete/import.
func buildResidue(series *NormSeries, initial *NormInitialMass, tg *TGResult, p Params) *ResidueResult {
	r := &ResidueResult{Algorithm: AlgoResidue, ParamFP: p.fingerprint(), Sources: series.Sources}
	if len(series.Samples) == 0 {
		r.Unknown = "no usable samples; residue unknown"
		r.ArtifactID = artifactID(AlgoResidue, p.fingerprint(), series.Sources, map[string]any{"unknown": r.Unknown})
		return r
	}
	last := series.Samples[len(series.Samples)-1]
	final := last.MassMg
	r.FinalMassMg = &final

	step := func(label string, v float64, unit, formula, detail string, ref *SourceRef) ChainStep {
		return ChainStep{Label: label, Value: v, Unit: unit, Formula: formula, Detail: detail, Source: ref}
	}

	// Initial mass prefers the separately-declared initial_mass layer; an
	// initial-mass revision swaps that source and only expires dependents.
	var initVal float64
	var initRef *SourceRef
	if initial != nil && initial.Initial != nil && initial.Initial.Mass.OK() {
		initVal, _ = initial.Initial.Mass.NormFloat()
		cid := initial.Initial.CommitID
		initRef = &SourceRef{Kind: "raw_commit", ID: cid, Field: "mass"}
	} else {
		initVal = series.Samples[0].MassMg
		initRef = &SourceRef{Kind: "artifact", ID: series.ArtifactID, Row: series.Samples[0].Row, Field: "m", ArtifactID: series.ArtifactID}
	}
	r.InitialMassMg = &initVal
	pct := final / initVal * 100.0
	r.ResiduePct = &pct

	artPlaceholder := ""
	r.Chain = []ChainStep{
		step("initial mass", initVal, UnitMg, "initial_mass layer", "declared sample starting mass", initRef),
		step("final observed mass", final, UnitMg, "last corrected sample", "end of final segment; gaps not interpolated",
			&SourceRef{Kind: "artifact", ArtifactID: series.ArtifactID, ID: series.ArtifactID, Row: last.Row, Field: "m"}),
	}
	if p.DriftCorrect {
		r.Chain = append(r.Chain, step("drift correction applied", 1, UnitOne, "m -= rate*t", "balance baseline drift correction enabled",
			&SourceRef{Kind: "param", ID: "params:v" + itoa(p.Version), Field: "drift_correct"}))
	}
	if p.BlankCorrect {
		r.Chain = append(r.Chain, step("blank correction applied", 1, UnitOne, "m -= blank(t)", "blank curve subtraction enabled",
			&SourceRef{Kind: "param", ID: "params:v" + itoa(p.Version), Field: "blank_correct"}))
	}
	r.Chain = append(r.Chain, step("residue", pct, UnitPct, "final_mass / initial_mass * 100", "residue fraction of declared initial mass",
		&SourceRef{Kind: "artifact", ArtifactID: artPlaceholder, ID: ""}))

	// attach stable value ids now that the artifact id is about to be derived
	r.ArtifactID = artifactID(AlgoResidue, p.fingerprint(), series.Sources, map[string]any{
		"series": series.ArtifactID, "initial": initVal, "final": final,
	})
	names := []string{"initial_mass", "final_mass", "drift_on", "blank_on", "residue_pct"}
	if !p.DriftCorrect {
		names = []string{"initial_mass", "final_mass", "blank_on", "residue_pct"}
	}
	if !p.BlankCorrect {
		// recompute names compactly
		names = names[:0]
		names = append(names, "initial_mass", "final_mass")
		if p.DriftCorrect {
			names = append(names, "drift_on")
		}
		names = append(names, "residue_pct")
	}
	if len(names) != len(r.Chain) {
		names = names[:0]
		for i := range r.Chain {
			names = append(names, chainName(i, p))
		}
	}
	for i := range r.Chain {
		r.Chain[i].ValueID = valueID(r.ArtifactID, names[i])
	}
	return r
}

func chainName(i int, p Params) string {
	names := []string{"initial_mass", "final_mass"}
	if p.DriftCorrect {
		names = append(names, "drift_on")
	}
	if p.BlankCorrect {
		names = append(names, "blank_on")
	}
	names = append(names, "residue_pct")
	if i < len(names) {
		return names[i]
	}
	return "step" + itoa(i)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	n := len(b)
	for i > 0 {
		n--
		b[n] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		n--
		b[n] = '-'
	}
	return string(b[n:])
}
