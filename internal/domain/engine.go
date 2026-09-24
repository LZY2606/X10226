package domain

const AlgorithmVersion = "tga-stage-decomposition@2026.09.24"

type ChainNode struct {
	ID       string      `json:"id"`
	Label    string      `json:"label"`
	Value    Quantity    `json:"value"`
	Inputs   []SourceRef `json:"inputs,omitempty"`
	Formula  string      `json:"formula,omitempty"`
	Children []string    `json:"children,omitempty"`
	Order    int         `json:"order"`
}

type ComputedSeries struct {
	ID          string            `json:"id"`
	Kind        string            `json:"kind"`
	Label       string            `json:"label"`
	Unit        Unit              `json:"unit"`
	Points      []Quantity        `json:"points"`
	Gaps        []Gap             `json:"gaps,omitempty"`
	Revision    string            `json:"revision"`
	StaleReason string            `json:"staleReason,omitempty"`
	Deps        map[string]string `json:"deps,omitempty"`
}

type Gap struct {
	ID              string  `json:"id"`
	StartS          float64 `json:"startS"`
	EndS            float64 `json:"endS"`
	Reason          string  `json:"reason"`
	NoInterpolation bool    `json:"noInterpolation"`
}

type StageCandidate struct {
	ID               string            `json:"id"`
	RevisionID       string            `json:"revisionId"`
	Kind             string            `json:"kind"`
	Label            string            `json:"label"`
	StartS           float64           `json:"startS"`
	EndS             float64           `json:"endS"`
	PeakS            *float64          `json:"peakS,omitempty"`
	PeakMgPerMin     *float64          `json:"peakMgPerMin,omitempty"`
	MassLossMg       float64           `json:"massLossMg"`
	LossPctInitial   float64           `json:"lossPctInitial"`
	AtmosphereAtPeak string            `json:"atmosphereAtPeak,omitempty"`
	Status           string            `json:"status"`
	RejectionReasons []string          `json:"rejectionReasons,omitempty"`
	OverlapsWith     []string          `json:"overlapsWith,omitempty"`
	SourceIDs        []string          `json:"sourceIds"`
	Chain            []ChainNode       `json:"chain,omitempty"`
	StaleReason      string            `json:"staleReason,omitempty"`
	Human            bool              `json:"human,omitempty"`
	Deps             map[string]string `json:"deps,omitempty"`
}

type ResidueResult struct {
	ID          string            `json:"id"`
	RevisionID  string            `json:"revisionId"`
	Label       string            `json:"label"`
	FinalMass   Quantity          `json:"finalMass"`
	InitialMass Quantity          `json:"initialMass"`
	ResiduePct  Quantity          `json:"residuePct"`
	Chain       []ChainNode       `json:"chain"`
	StaleReason string            `json:"staleReason,omitempty"`
	Deps        map[string]string `json:"deps,omitempty"`
}

type AtmosphereMoment struct {
	ID          string            `json:"id"`
	TimeS       float64           `json:"timeS"`
	Kind        string            `json:"kind"`
	Gas         string            `json:"gas,omitempty"`
	Label       string            `json:"label"`
	SourceIDs   []string          `json:"sourceIds,omitempty"`
	RevisionID  string            `json:"revisionId"`
	StaleReason string            `json:"staleReason,omitempty"`
	Deps        map[string]string `json:"deps,omitempty"`
}

type FurnaceMoment struct {
	ID          string            `json:"id"`
	TimeS       float64           `json:"timeS"`
	ProgramC    float64           `json:"programC"`
	SampleC     float64           `json:"sampleC"`
	SourceIDs   []string          `json:"sourceIds,omitempty"`
	RevisionID  string            `json:"revisionId"`
	StaleReason string            `json:"staleReason,omitempty"`
	Deps        map[string]string `json:"deps,omitempty"`
}

type Evaluation struct {
	ID                string             `json:"id"`
	Sequence          int                `json:"sequence"`
	RequestedBy       string             `json:"requestedBy"`
	At                int64              `json:"at"`
	Algorithm         string             `json:"algorithm"`
	ParameterID       string             `json:"parameterId"`
	ParameterHash     string             `json:"parameterHash"`
	SourceFingerprint string             `json:"sourceFingerprint"`
	Deps              map[string]string  `json:"deps,omitempty"`
	Series            []ComputedSeries   `json:"series"`
	Gaps              []Gap              `json:"gaps"`
	Stages            []StageCandidate   `json:"stages"`
	Residues          []ResidueResult    `json:"residues"`
	Atmosphere        []AtmosphereMoment `json:"atmosphere"`
	Furnace           []FurnaceMoment    `json:"furnace"`
	Diagnostics       []Diagnostic       `json:"diagnostics"`
	Principles        []PrincipleEvent   `json:"principles"`
}

type ProjectState struct {
	ID                 string
	Title              string
	CaseID             string
	Batches            map[string]RawBatch
	BatchOrder         []string
	Parameters         Parameters
	ParameterHistory   []ParameterRevision
	Evaluations        []Evaluation
	Principles         []PrincipleEvent
	Decisions          []CandidateDecision
	Conflicts          []ConflictRecord
	EvaluationRequests []EvaluationRequested
}

type ResultBundle struct {
	Current Evaluation
	All     []Evaluation
	Stale   map[string]string
}

type parsedRow struct {
	SourceID string
	Row      int
	Values   map[string]float64
	Raw      map[string]string
	Invalid  map[string]string
}
