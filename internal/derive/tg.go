package derive

import "tgaline/internal/domain"

// resolveInitialMass applies the stratification: net masses are used directly;
// gross is reduced by pan/tare. Revisions (new initial_mass versions) simply
// replace the selected document; the provenance records the exact version.
func resolveInitialMass(in Inputs) (float64, string, []domain.ChainNode) {
	chain := []domain.ChainNode{}
	if in.InitialMass == nil {
		// fallback: first valid corrected sample is used later and flagged.
		return 0, "unknown", chain
	}
	var net, pan, tare float64
	haveNet, havePan, haveTare := false, false, false
	docID := in.InitialMass.ID
	for _, m := range in.InitialMass.Masses {
		if len(m.Invalid) > 0 {
			continue
		}
		chain = append(chain, domain.ChainNode{
			ValueID: ValueID("initial_mass:"+docID, "input", m.Basis+":"+m.MassRaw),
			Label:   "initial mass stratum (" + m.Basis + ")",
			Value:   fkv(m.MassMg) + " mg",
			Formula: m.MassRaw + " " + unitDecl(in.InitialMass, "mass"),
			SourceID: docID, SourceKind: string(domain.KindInitialMass), Status: "ok",
		})
		switch m.Basis {
		case "net":
			net, haveNet = m.MassMg, true
		case "gross":
			net, haveNet = m.MassMg, true // gross with no pan info stands as best estimate
		case "pan":
			pan, havePan = m.MassMg, true
		case "tare":
			tare, haveTare = m.MassMg, true
		}
	}
	status := "ok"
	switch {
	case haveNet && (havePan || haveTare):
		net = net - pan - tare
	case haveNet:
	case havePan && haveTare:
		status = "unknown"
	}
	if !haveNet {
		status = "unknown"
	}
	return net, status, chain
}

func unitDecl(d *domain.Document, col string) string {
	if u := d.DeclaredUnits[col]; u != "" {
		return u
	}
	if col == "mass" {
		return "mg"
	}
	return ""
}

func buildTG(projectID string, in Inputs, g *Graph, corr *domain.Artifact) *domain.Artifact {
	im0, imStatus, imChain := resolveInitialMass(in)
	g.InitialMassMg, g.InitialMassStatus, g.InitialMassChain = im0, imStatus, imChain
	corrPts := asPoints(corr)
	status := corr.Status
	diags := append([]domain.Diagnostic(nil), corr.Diagnostics...)
	fallbackUsed := false
	if imStatus == "unknown" || im0 <= 0 {
		if len(corrPts) > 0 {
			im0 = corrPts[0].MassMg
			fallbackUsed = true
		}
		status = demote(status)
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "initial_mass_fallback",
			Message: "no stratifiable initial mass; using first corrected sample mass",
			Severity: domain.SeverityWarning})
	}
	out := make([]domain.Point, len(corrPts))
	for i, p := range corrPts {
		p.MassPct = 100.0 * p.MassMg / im0
		out[i] = p
	}
	docs := []*domain.Document{in.Samples, in.Drift, in.Blank, in.InitialMass}
	ih := inputsHash(docs, in.Params, "tg")
	id := ArtifactID("tg", ih)
	data := map[string]any{
		"points": out, "initial_mass_mg": im0,
		"initial_mass_status": imStatus, "fallback_used": fallbackUsed,
		"initial_mass_chain": imChain,
	}
	return newArtifact(id, projectID, domain.ArtTGSeries, status, data,
		prov(docs, in.Params, g.ParamsFP, ih, corr.ID), diags)
}
