package analysis

import (
	"sort"

	"tga-evidence/internal/domain"
)

type point struct {
	recID          string
	timeS          float64
	tempC          float64
	rawMg          float64
	correctedMg    float64
	normalized     float64
	dtgMgPerMin    float64
	smoothDTG      float64
	segment        int
	nonMonotonic   bool
	correctionNote string
}

type blankPoint struct{ timeS, valueMg float64; recID string }
type driftPoint struct{ timeS, valueMg float64; recID string }
type furnaceEvent struct {
	recID string
	timeS float64
	target float64
	ramp float64
	segment string
}
type gasEvent struct {
	recID string
	timeS float64
	gas string
	note string
}

type initialMass struct {
	valueMg float64
	recID string
	version int
}

type Engine struct{}

func New() Engine { return Engine{} }

func (e Engine) Analyze(projectID string, seq int64, state *domain.State) domain.Analysis {
	recs := state.ActiveRecords()
	params := state.CurrentParams()
	prepared := prepare(recs, params)
	timeline := buildTimeline(prepared, params)
	stages, rejected, judgments := buildCandidates(prepared, params, state)
	residues := buildResidues(prepared, params)
	analysisID := domain.AnalysisID(projectID, params, state.SourceSetHash())
	a := domain.Analysis{
		ID: analysisID, ProjectID: projectID, AlgorithmVersion: domain.AlgorithmVersion,
		ParameterVersion: params.Version, ParameterFingerprint: params.Fingerprint,
		SourceSetHash: state.SourceSetHash(), Current: true, CreatedFromSeq: seq,
		Stages: stages, RejectedStages: rejected, Residues: residues, Timeline: timeline,
		MachineJudgments: judgments,
	}
	applyDecisions(&a, state, prepared, params)
	return a
}

type preparedData struct {
	points   []point
	blanks   []blankPoint
	drifts   []driftPoint
	furnaces []furnaceEvent
	gases    []gasEvent
	initial  *initialMass
	diags    []string
}

func prepare(recs []domain.Record, p domain.Params) preparedData {
	var d preparedData
	initialsByVersion := map[int]domain.Record{}
	var initialVersions []int
	for _, rec := range recs {
		switch rec.Layer {
		case "sample":
			if rec.Time.Value == 0 && !validFloat(rec.Time.Raw) {
				continue
			}
		case "blank":
			if rec.Time.Raw != "" && rec.Mass.Raw != "" {
				d.blanks = append(d.blanks, blankPoint{rec.Time.Value, rec.Mass.Value, rec.ID})
			}
		case "drift", "balance_drift", "balance":
			if rec.Time.Raw != "" && rec.Drift.Raw != "" {
				d.drifts = append(d.drifts, driftPoint{rec.Time.Value, rec.Drift.Value, rec.ID})
			}
		case "furnace", "furnace_program":
			d.furnaces = append(d.furnaces, furnaceEvent{rec.ID, rec.Time.Value, rec.TargetTemp.Value, rec.RampRate.Value, rec.Segment})
		case "gas", "gas_valve", "atmosphere":
			d.gases = append(d.gases, gasEvent{rec.ID, rec.Time.Value, rec.Gas, rec.Note})
		case "initial_mass", "initial":
			version := 1
			if rec.Supersedes != "" { version++ }
			if _, ok := initialsByVersion[version]; !ok { initialVersions = append(initialVersions, version) }
			initialsByVersion[version] = rec
		}
	}
	sort.Slice(d.blanks, func(i,j int) bool { return d.blanks[i].timeS < d.blanks[j].timeS })
	sort.Drift := sortDrift(d.drifts)
	d.drifts = sortDrift(d.drifts)
	sort.Slice(d.furnaces, func(i,j int) bool { return d.furnaces[i].timeS < d.furnaces[j].timeS })
	sort.Slice(d.gases, func(i,j int) bool { return d.gases[i].timeS < d.gases[j].timeS })
	sort.Slice(initialVersions, func(i,j int) bool { return initialVersions[i] < initialVersions[j] })
	if len(initialVersions) > 0 {
		rec := initialsByVersion[initialVersions[len(initialVersions)-1]]
		d.initial = &initialMass{rec.InitialMass.Value, rec.ID, initialVersions[len(initialVersions)-1]}
	}
	for _, rec := range recs {
		if rec.Layer != "sample" || rec.Time.Raw == "" || rec.Mass.Raw == "" || rec.Temperature.Raw == "" {
			continue
		}
		d.points = append(d.points, point{recID: rec.ID, timeS: rec.Time.Value, tempC: rec.Temperature.Value, rawMg: rec.Mass.Value})
	}
	sort.Slice(d.points, func(i,j int) bool { return d.points[i].timeS < d.points[j].timeS })
	assignSegments(&d, p)
	correctPoints(&d)
	computeDTG(&d, p)
	return d
}

func validFloat(raw string) bool { return raw != "" }

func sortDrift(in []driftPoint) []driftPoint {
	sort.Slice(in, func(i,j int) bool { return in[i].timeS < in[j].timeS })
	return in
}
