package domain

import ()

type Point struct {
	Index        int
	Time         float64
	RawMass      float64
	Temperature  *float64
	Blank        *float64
	Drift        float64
	Corrected    *float64
	DTG          *float64
	GapAfter     bool
	NonMonotonic bool
}

type StageCandidate struct {
	ID                string
	Kind              string
	PeakTime          *float64
	StartObserved     float64
	EndObserved       float64
	StartID           string
	EndID             string
	MassLoss          *float64
	PercentLoss       *float64
	AtmosphereOverlap string
	Status            string
	RejectionReason   string
	UnknownReason     string
	Overlaps          []string
}

type RuleEvent struct {
	Code     string         `json:"code"`
	Status   string         `json:"status"`
	Basis    string         `json:"basis"`
	At       *float64       `json:"at,omitempty"`
	Unit     string         `json:"unit,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
	SourceID string         `json:"source_id,omitempty"`
}

type DerivationOutput struct {
	DerivationID  string
	ParameterHash string
	Nodes         []ValueNode
	Bindings      map[string]string
}

type engineBuilder struct {
	project  *Project
	sources  map[string]*Source
	parsed   map[string]any
	hash     string
	nodes    []ValueNode
	bindings map[string]string
	rules    []RuleEvent
	points   []*Point
	blank    []ParsedBlankPoint
	params   ParsedParameters
	stages   []StageCandidate
}

func Derive(project *Project) (DerivationOutput, []Diagnostic) {
	b := &engineBuilder{project: project, sources: map[string]*Source{}, parsed: map[string]any{}, bindings: map[string]string{}}
	var diagnostics []Diagnostic
	for layer, source := range project.CurrentSources {
		b.sources[layer] = source
		parsed, layerDiagnostics := ParseLayer(layer, source.Raw)
		diagnostics = append(diagnostics, layerDiagnostics...)
		b.parsed[layer] = parsed
		for i := range diagnostics {
			if diagnostics[i].Path != "" {
				diagnostics[i].Path = layer + "." + diagnostics[i].Path
			}
		}
	}
	hashParts := []any{AlgorithmVersion}
	for _, layer := range orderedLayers() {
		if source, ok := b.sources[layer]; ok {
			hashParts = append(hashParts, layer, source.ID, source.Version, string(CanonicalJSON(source.Raw)))
		}
	}
	b.hash = Fingerprint(hashParts...)
	b.defaultParams()
	if parsed, ok := b.parsed[LayerAnalysisParameters].(ParsedParameters); ok {
		b.params = parsed
	}
	b.addRequiredRules()
	b.buildPoints()
	b.buildBlank()
	b.applyCorrections()
	b.markGapsAndNoise()
	b.calculateDTG()
	b.furnaceAndGas()
	b.detectStages()
	b.finalResidue()
	b.addRuleNodes()
	id := "derivation:" + ShortFingerprint(project.ID, b.hash)
	return DerivationOutput{DerivationID: id, ParameterHash: b.hash, Nodes: b.nodes, Bindings: b.bindings}, diagnostics
}

func orderedLayers() []string {
	return []string{LayerRawSamples, LayerDriftCorrection, LayerFurnaceProgram, LayerGasValveEvents, LayerBlankCurve, LayerInitialMass, LayerAnalysisParameters}
}
