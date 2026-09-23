package derive

import (
	"tga-review/internal/domain"
	"tga-review/internal/ingest"
)

// nodeID builds the stable identity of a derived node: project + kind +
// parameter fingerprint + direct source identities. It is content-addressed
// so identical replay produces identical ids.
func nodeID(projectID, kind, fp string, directSources, directNodes []string) string {
	return ingest.ShortID("node", projectID, kind, fp, directSources, directNodes)
}

// LayerResults holds every normalized/derived node from one pipeline run.
type LayerResults struct {
	Nodes    []domain.Node
	Layer    *domain.SampleLayer
	Corrected []domain.Point
	DTG      []domain.Point
	Windows  []reactionWindow
	Stages   []domain.Stage
	Residue  *domain.Residue
}

// Run executes the whole derivation pipeline from the current accepted
// source versions and the supplied parameter revision.
func Run(projectID string, current map[string]*domain.Source, p domain.Parameters) LayerResults {
	var res LayerResults
	fp := p.Fingerprint
	add := func(kind string, sources, nodes []string, payload map[string]any, diags []domain.Diagnostic) domain.Node {
		n := domain.Node{
			ID: nodeID(projectID, kind, fp, sources, nodes), ProjectID: projectID,
			Kind: kind, AlgorithmVer: AlgorithmVersion, ParamFp: fp,
			DirectSources: sources, DirectNodes: nodes, Payload: payload,
			Diagnostics: diags,
		}
		res.Nodes = append(res.Nodes, n)
		return n
	}
	srcID := func(k string) string {
		if s := current[k]; s != nil {
			return s.ID
		}
		return ""
	}

	// normalized strata
	var layer *domain.SampleLayer
	if s := current[domain.KindSamples]; s != nil {
		layer, _ = normSamples(s, p.GapMaxSeconds)
		add(domain.NodeNormSample, []string{s.ID}, nil,
			map[string]any{"layer": layer}, layer.Diagnostics)
		res.Layer = layer
	}
	if s := current[domain.KindProgram]; s != nil {
		steps := normProgram(s)
		add(domain.NodeNormProgram, []string{s.ID}, nil, map[string]any{"steps": steps}, s.Diagnostics)
	}
	var gas []domain.GasEvent
	if s := current[domain.KindGasEvents]; s != nil {
		gas = normGas(s)
		add(domain.NodeNormGas, []string{s.ID}, nil, map[string]any{"events": gas}, s.Diagnostics)
	}
	var blank []domain.Point
	if s := current[domain.KindBlank]; s != nil {
		blank = normBlank(s)
		add(domain.NodeNormBlank, []string{s.ID}, nil, map[string]any{"curve": blank}, s.Diagnostics)
	}
	var drift []domain.DriftPoint
	var corrSources, corrNodes []string
	if s := current[domain.KindDrift]; s != nil {
		drift = normDrift(s)
		add(domain.NodeNormDrift, []string{s.ID}, nil, map[string]any{"anchors": drift}, s.Diagnostics)
		corrSources = append(corrSources, s.ID)
	}
	var initW *domain.InitWeight
	if s := current[domain.KindInitWeight]; s != nil && len(s.Rows) > 0 {
		initW = normInit(s)
		add(domain.NodeNormInit, []string{s.ID}, nil, map[string]any{"init": initW}, s.Diagnostics)
		corrSources = append(corrSources, s.ID)
	}

	// corrected mass depends on samples (+ drift + blank)
	corrNodeInputs := []string{}
	if layer != nil {
		corrNodeInputs = append(corrNodeInputs,
			nodeID(projectID, domain.NodeNormSample, fp, []string{srcID(domain.KindSamples)}, nil))
		corrSources = append([]string{srcID(domain.KindSamples)}, corrSources...)
		if s := current[domain.KindBlank]; s != nil {
			corrSources = append(corrSources, s.ID)
			corrNodeInputs = append(corrNodeInputs,
				nodeID(projectID, domain.NodeNormBlank, fp, []string{s.ID}, nil))
		}
		if s := current[domain.KindDrift]; s != nil {
			corrNodeInputs = append(corrNodeInputs,
				nodeID(projectID, domain.NodeNormDrift, fp, []string{s.ID}, nil))
		}
	}
	var corrected []domain.Point
	var gaps []domain.Gap
	if layer != nil {
		corrected = correctMass(layer, drift, blank)
		gaps = layer.Gaps
		add(domain.NodeCorrected, corrSources, corrNodeInputs,
			map[string]any{"series": corrected}, nil)
		res.Corrected = corrected
	}

	// DTG depends only on corrected mass
	var dtg []domain.Point
	if len(corrected) > 0 {
		dtg = computeDTG(corrected, gaps)
		corrNode := nodeID(projectID, domain.NodeCorrected, fp, corrSources, corrNodeInputs)
		add(domain.NodeDTG, nil, []string{corrNode}, map[string]any{"series": dtg}, nil)
		res.DTG = dtg
	}

	// reaction windows depend on gas events + timing parameters
	windows := reactionWindows(gas, p)
	if len(gas) > 0 {
		add(domain.NodeReactWindow, []string{srcID(domain.KindGasEvents)}, nil,
			map[string]any{"windows": windows, "gas_delay_seconds": p.GasDelaySeconds,
				"temp_lag_seconds": p.TempLagSeconds}, nil)
		res.Windows = windows
	}

	// stages depend on corrected + DTG + windows + init mass
	if len(corrected) > 0 && initW != nil {
		stages := buildStages(corrected, dtg, gaps, windows, p, projectID, initW.Mass)
		deps := []string{
			nodeID(projectID, domain.NodeCorrected, fp, corrSources, corrNodeInputs),
		}
		if len(gas) > 0 {
			deps = append(deps, nodeID(projectID, domain.NodeReactWindow, fp,
				[]string{srcID(domain.KindGasEvents)}, nil))
		}
		add(domain.NodeStages, []string{srcID(domain.KindSamples)}, deps,
			map[string]any{"stages": stages, "sum_not_forced_100": true}, nil)
		res.Stages = stages
		// residue depends on corrected mass + initial mass only
		residue := buildResidue(corrected, initW, p, projectID)
		add(domain.NodeResidue,
			[]string{srcID(domain.KindSamples), srcID(domain.KindInitWeight)},
			[]string{nodeID(projectID, domain.NodeCorrected, fp, corrSources, corrNodeInputs),
				nodeID(projectID, domain.NodeNormInit, fp, []string{srcID(domain.KindInitWeight)}, nil)},
			map[string]any{"residue": residue}, nil)
		res.Residue = &residue
	}
	return res
}
