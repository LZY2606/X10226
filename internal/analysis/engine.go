// Package analysis implements the TGA domain calculations (algorithm "tga-v1"):
// balance baseline-drift correction, gap-preserving DTG, overlapping peak and
// stage candidates, atmosphere-delay/thermal-lag evidence and residual-fraction
// calculation chains.
//
// Domain rules encoded here:
//   - The valve switch time is NOT the reaction time: atmosphere delay and
//     thermal lag shift gas arrival at the sample.
//   - Stages may overlap and their percentage losses are relative to each
//     stage's entry mass; they are never forced to sum to 100%.
//   - Sampling gaps are never bridged by interpolation; DTG across a gap is
//     marked and ambiguous boundaries keep every legal alternative.
//   - Candidates, reject reasons, error/unknown states are first-class data.
package analysis

import (
	"tga-review/internal/domain"
)

// AlgorithmV1 is the current algorithm version tag stamped on every bundle.
const AlgorithmV1 = "tga-v1"

// Engine computes derived bundles from immutable source layers.
type Engine struct {
	version string
}

// NewEngine returns the v1 engine.
func NewEngine() *Engine { return &Engine{version: AlgorithmV1} }

// Version returns the algorithm version.
func (e *Engine) Version() string { return e.version }

// DepVersions carries the committed version of each source layer plus the
// parameter version; empty values mean the dependency is absent.
type DepVersions struct {
	SamplePoints string
	DriftCorr    string
	FurnaceProg  string
	GasValves    string
	BlankCurve   string
	InitialMass  string
	Params       string
}

// Inputs bundles the latest committed source payloads and parameters.
type Inputs struct {
	SamplePoints *domain.SamplePointsLayer
	Drift        *domain.DriftCorrectionLayer
	Furnace      *domain.FurnaceProgramLayer
	GasValves    *domain.GasValvesLayer
	Blank        *domain.BlankCurveLayer
	InitialMass  *domain.InitialMassLayer
	Params       domain.Params
}

// Compute runs the whole pipeline. Missing dependencies are recorded as
// judgements rather than causing the whole analysis to fail, mirroring the
// "local invalidation" rule.
func (e *Engine) Compute(in Inputs, deps DepVersions) *domain.AnalysisBundle {
	p := in.Params
	b := &domain.AnalysisBundle{
		Algorithm:        e.version,
		ParamVersion:     deps.Params,
		ParamFingerprint: domain.ParamFingerprint(e.version, p),
	}
	add := func(layer, ver string) {
		if ver != "" {
			b.SourceLayers = append(b.SourceLayers, domain.LayerRef{Layer: layer, Version: ver})
		}
	}
	add(domain.LayerSamplePoints, deps.SamplePoints)
	add(domain.LayerDriftCorr, deps.DriftCorr)
	add(domain.LayerFurnaceProg, deps.FurnaceProg)
	add(domain.LayerGasValves, deps.GasValves)
	add(domain.LayerBlankCurve, deps.BlankCurve)
	add(domain.LayerInitialMass, deps.InitialMass)

	if in.SamplePoints == nil {
		b.Judgements = append(b.Judgements, j(domain.SevError, "missing_sample_points", "no sample points; analysis cannot run", "trace", nil))
		return b
	}

	b.Trace = correctTrace(in, p)
	b.Judgements = append(b.Judgements, traceJudgements(b.Trace, p)...)
	b.DTG = computeDTG(b.Trace, p)
	b.Peaks = findPeaks(b.DTG, p)
	if in.GasValves != nil && in.Furnace != nil {
		b.Atmosphere = mapAtmosphere(in, b.Trace)
		b.Judgements = append(b.Judgements, atmosphereJudgements(b.Atmosphere, p)...)
	} else {
		b.Judgements = append(b.Judgements, j(domain.SevInfo, "atmosphere_unavailable",
			"gas valves or furnace program absent; gas timing not derived", "atmosphere", nil))
	}
	b.Stages = buildStages(in, b, p)
	b.Residues = buildResidues(in, b)
	b.Judgements = append(b.Judgements, stageJudgements(b.Stages)...)
	b.Judgements = append(b.Judgements, residueJudgements(b.Residues)...)
	return b
}

func j(sev, code, msg, subject string, data map[string]any) domain.Judgement {
	return domain.Judgement{Severity: sev, Code: code, Message: msg, Subject: subject, Data: data}
}
