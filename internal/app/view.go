package app

import (
	"tga-review/internal/calc"
	"tga-review/internal/domain"
)

// StageView is a stage candidate paired with its live decision state.
type StageView struct {
	calc.StageCandidate `json:",inline"`
	StageKey  string `json:"stage_key"`
	Live      string `json:"live_decision"` // accept | reject | ""
	DecisionID string `json:"decision_id,omitempty"`
}

// ViewModel is the synchronized review page model at one timeline point.
type ViewModel struct {
	Project      domain.Project      `json:"project"`
	AtSeq        int                 `json:"at_seq"`
	CurrentSeq   int                 `json:"current_seq"`
	Historical   bool                `json:"historical"`
	Params       domain.Params       `json:"params"`
	ParamsFP     string              `json:"params_fp"`
	Sources      []domain.Source     `json:"sources"`
	Nodes        []NodeView          `json:"nodes"`
	Stages       []StageView         `json:"stages"`
	Residue      *ResidueView        `json:"residue,omitempty"`
	Decisions    []domain.Decision   `json:"decisions"`
	Conflicts    []domain.Conflict   `json:"conflicts"`
	Sessions     []domain.Session    `json:"sessions"`
	Archive      []domain.ArchiveEntry `json:"archive"`
	ReviewState  string              `json:"review_state"`
}

type NodeView struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Algo      string         `json:"algo"`
	AlgoVer   string         `json:"algo_version"`
	ParamFP   string         `json:"param_fingerprint"`
	Inputs    []string       `json:"input_ids"`
	Status    string         `json:"status"`
	Current   bool           `json:"current"`
	SupersedBy string        `json:"superseded_by,omitempty"`
	CreatedEventSeq int       `json:"created_event_seq"`
	Diag      []domain.Diag  `json:"diag,omitempty"`
	Payload   map[string]any `json:"payload"`
}

type ResidueView struct {
	NodeID     string             `json:"node_id"`
	ResiduePct float64            `json:"residue_pct"`
	Chain      any                `json:"chain"`
	Status     string             `json:"status"`
	Inputs     []string           `json:"input_ids"`
	Stale      bool               `json:"stale"`
}
