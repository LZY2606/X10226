package derive

import (

	"tga-review/internal/domain"
)

// ParamsID is the stable content fingerprint of a parameter revision.
func ParamsID(p domain.Params) string {
	return domain.HashID("par-", p)
}

type nodeFP struct {
	Algo     string        `json:"algo"`
	Params   domain.Params `json:"params"`
	Inputs   []domain.Ref  `json:"inputs"`
	NodeDeps []string      `json:"node_deps,omitempty"`
	Body     any           `json:"body"`
}

func (e *Engine) wrap(slot, algo, paramsID, inputFP string, sources []domain.Ref, set func(d *domain.Derived), deps ...string) *domain.Derived {
	d := &domain.Derived{
		Project: e.projectID,
		Slot:    slot,
		Kind:    slot,
		ParamsID: paramsID,
		Prov: domain.Provenance{
			Algo: algo, ParamsID: paramsID, Sources: sources, InputFP: inputFP, ComputedAt: e.now,
		},
		Created: e.now,
	}
	set(d)
	fpBody := nodeFP{Algo: algo, Params: e.params, Inputs: sources, NodeDeps: deps, Body: d.Correct}
	var body any
	switch slot {
	case domain.SlotCorrect:
		body = d.Correct
	case domain.SlotDTG:
		body = d.DTG
	case domain.SlotStages:
		body = d.Stages
	case domain.SlotResidue:
		body = d.Residue
	}
	fpBody.Body = body
	d.ID = domain.HashID("nd-", fpBody)
	d.Prov.InputFP = d.ID
	return d
}

// Run executes corrected -> dtg -> stages -> residue. Missing layers block only
// the nodes that need them; partial imports therefore yield partial graphs.
func (e *Engine) Run(projectID string) *Result {
	e.projectID = projectID
	paramsID := ParamsID(e.params)

	srcRefs := func(ks ...domain.RawKind) []domain.Ref {
		var refs []domain.Ref
		for _, k := range ks {
			if r, ok := e.rawRef(k); ok {
				refs = append(refs, r)
			}
		}
		return refs
	}

	res := &Result{ParamsID: paramsID}
	if e.optional(domain.KindSamples) == nil {
		return res
	}

	cn := e.runCorrect()
	res.Correct = e.wrap(domain.SlotCorrect, domain.AlgoCorrect, paramsID, "",
		srcRefs(domain.KindSamples, domain.KindBlank, domain.KindDrift),
		func(d *domain.Derived) { d.Correct = cn })

	dn := e.runDTG(cn)
	res.DTG = e.wrap(domain.SlotDTG, domain.AlgoDTG, paramsID, "",
		append(srcRefs(domain.KindSamples, domain.KindInitMass), domain.Ref{Kind: "node", ID: res.Correct.ID, Label: domain.AlgoCorrect}),
		func(d *domain.Derived) { d.DTG = dn }, res.Correct.ID)

	sn := e.runStages(cn, dn)
	res.Stages = e.wrap(domain.SlotStages, domain.AlgoStages, paramsID, "",
		append(srcRefs(domain.KindSamples, domain.KindGas, domain.KindInitMass), domain.Ref{Kind: "node", ID: res.DTG.ID, Label: domain.AlgoDTG}),
		func(d *domain.Derived) { d.Stages = sn }, res.Correct.ID, res.DTG.ID)

	rn := e.runResidue(cn, dn, sn)
	res.Residue = e.wrap(domain.SlotResidue, domain.AlgoResidue, paramsID, "",
		append(srcRefs(domain.KindSamples, domain.KindBlank, domain.KindDrift, domain.KindInitMass),
			domain.Ref{Kind: "node", ID: res.Correct.ID, Label: domain.AlgoCorrect}),
		func(d *domain.Derived) { d.Residue = rn }, res.Correct.ID)

	return res
}

// AffectedSlots reports which derived slots must recompute when a layer changes.
// Local input changes invalidate only dependent results.
func AffectedSlots(changed domain.RawKind) []string {
	switch changed {
	case domain.KindSamples:
		return []string{domain.SlotCorrect, domain.SlotDTG, domain.SlotStages, domain.SlotResidue}
	case domain.KindDrift, domain.KindBlank:
		return []string{domain.SlotCorrect, domain.SlotDTG, domain.SlotStages, domain.SlotResidue}
	case domain.KindGas:
		return []string{domain.SlotStages}
	case domain.KindInitMass:
		return []string{domain.SlotDTG, domain.SlotStages, domain.SlotResidue}
	case domain.KindProgram:
		// furnace program is displayed for review; no numeric node depends on it
		return nil
	}
	return nil
}

// Ensure Result IDs are logged nicely in tests.
