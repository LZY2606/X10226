package domain

type Unit struct {
	System          string `json:"system"`
	Symbol          string `json:"symbol"`
	CoordinateBasis string `json:"coordinateBasis,omitempty"`
}

type Diagnostic struct {
	Code            string   `json:"code"`
	Severity        string   `json:"severity"`
	Message         string   `json:"message"`
	Location        string   `json:"location,omitempty"`
	AffectedObjects []string `json:"affectedObjects,omitempty"`
}

type Quantity struct {
	ID          string       `json:"id"`
	Label       string       `json:"label,omitempty"`
	Raw         string       `json:"raw"`
	Value       *float64     `json:"value"`
	Unit        Unit         `json:"unit"`
	Status      string       `json:"status"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	SourceIDs   []string     `json:"sourceIds,omitempty"`
	Algorithm   string       `json:"algorithm,omitempty"`
	RevisionID  string       `json:"revisionId,omitempty"`
	StaleReason string       `json:"staleReason,omitempty"`
}

type SourceRef struct {
	SourceID   string `json:"sourceId"`
	SourceKind string `json:"sourceKind"`
	Row        int    `json:"row,omitempty"`
	Field      string `json:"field,omitempty"`
}

type RawBatch struct {
	ID           string              `json:"id"`
	Kind         string              `json:"kind"`
	ParentID     string              `json:"parentId,omitempty"`
	FileName     string              `json:"fileName"`
	ContentType  string              `json:"contentType"`
	RawText      string              `json:"rawText"`
	Rows         []map[string]string `json:"rows"`
	Units        map[string]Unit     `json:"units"`
	Accepted     bool                `json:"accepted"`
	Diagnostics  []Diagnostic        `json:"diagnostics,omitempty"`
	SubmittedAt  int64               `json:"submittedAt"`
	Order        int                 `json:"order"`
	Fragment     bool                `json:"fragment,omitempty"`
	RevisionNote string              `json:"revisionNote,omitempty"`
}

type Parameters struct {
	MainDTGThresholdMgPerMin float64 `json:"mainDtgThresholdMgPerMin"`
	WeakDTGThresholdMgPerMin float64 `json:"weakDtgThresholdMgPerMin"`
	MaxSampleGapS            float64 `json:"maxSampleGapS"`
	ValveDelayS              float64 `json:"valveDelayS"`
	TemperatureLagS          float64 `json:"temperatureLagS"`
	BalanceDriftMgPerMin     float64 `json:"balanceDriftMgPerMin"`
}

func DefaultParameters() Parameters {
	return Parameters{
		MainDTGThresholdMgPerMin: -0.25,
		WeakDTGThresholdMgPerMin: -0.08,
		MaxSampleGapS:            90,
		ValveDelayS:              35,
		TemperatureLagS:          45,
		BalanceDriftMgPerMin:     0.001,
	}
}

type ParameterRevision struct {
	ID            string     `json:"id"`
	ParentID      string     `json:"parentId,omitempty"`
	Parameters    Parameters `json:"parameters"`
	ChangedFields []string   `json:"changedFields"`
	Reason        string     `json:"reason"`
	At            int64      `json:"at"`
}

type PrincipleEvent struct {
	ID         string   `json:"id"`
	Code       string   `json:"code"`
	Rule       string   `json:"rule"`
	Evidence   string   `json:"evidence"`
	AppliesTo  []string `json:"appliesTo,omitempty"`
	RecordedAt int64    `json:"recordedAt"`
}

type ProjectCreated struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	CaseID string `json:"caseId,omitempty"`
}

type SourceSubmitted struct {
	Batch RawBatch `json:"batch"`
}

type ParameterRevisionRecorded struct {
	Revision ParameterRevision `json:"revision"`
}

type EvaluationRequested struct {
	ID               string `json:"id"`
	ParentEvaluation string `json:"parentEvaluation,omitempty"`
	Reason           string `json:"reason"`
	At               int64  `json:"at"`
}

type CandidateDecision struct {
	ID                   string   `json:"id"`
	TargetKind           string   `json:"targetKind"`
	TargetID             string   `json:"targetId"`
	Action               string   `json:"action"`
	Reason               string   `json:"reason"`
	At                   int64    `json:"at"`
	Actor                string   `json:"actor"`
	SupersededDecisionID string   `json:"supersededDecisionId,omitempty"`
	AdjustedStartS       *float64 `json:"adjustedStartS,omitempty"`
	AdjustedEndS         *float64 `json:"adjustedEndS,omitempty"`
	Undone               bool     `json:"undone"`
	UndoneAt             int64    `json:"undoneAt,omitempty"`
	ConflictID           string   `json:"conflictId,omitempty"`
}

type ConflictRecord struct {
	ID          string   `json:"id"`
	TargetKind  string   `json:"targetKind"`
	TargetID    string   `json:"targetId"`
	DecisionIDs []string `json:"decisionIds"`
	Status      string   `json:"status"`
	Resolution  string   `json:"resolution,omitempty"`
	ResolvedBy  string   `json:"resolvedBy,omitempty"`
	ResolvedAt  int64    `json:"resolvedAt,omitempty"`
	CreatedAt   int64    `json:"createdAt"`
}

type ConflictResolved struct {
	Conflict ConflictRecord `json:"conflict"`
}

type PrincipleRecorded struct {
	Principle PrincipleEvent `json:"principle"`
}

type EventEnvelope struct {
	Seq         int64  `json:"seq"`
	ID          string `json:"id"`
	ProjectID   string `json:"projectId"`
	Type        string `json:"type"`
	Actor       string `json:"actor"`
	At          int64  `json:"at"`
	PayloadJSON string `json:"payloadJson"`
	PayloadHash string `json:"payloadHash"`
}

type Event struct {
	Seq     int64
	ID      string
	Type    string
	Actor   string
	At      int64
	Payload any
}
