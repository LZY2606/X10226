package domain

// Decision kinds.
const (
	DecisionAcceptStage    = "accept_stage"
	DecisionRejectStage    = "reject_stage"
	DecisionAdjustParams   = "adjust_params"
	DecisionReviseInitMass = "revise_initial_mass"
	DecisionUndo           = "undo"
	DecisionConfirmResidue = "confirm_residue"
)

type Decision struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Seq        int            `json:"seq"`
	Kind       string         `json:"kind"`
	Target     string         `json:"target,omitempty"` // stage key / node id
	Payload    map[string]any `json:"payload,omitempty"`
	NodeIDs    []string       `json:"node_ids"` // derived nodes visible when decided
	ParamFP    string         `json:"param_fp"`
	Rationale  string         `json:"rationale,omitempty"`
	Supersedes string         `json:"supersedes,omitempty"` // decision undone/replaced
	Live       bool           `json:"live"`
	CreatedAt  string         `json:"created_at"`
}

// Conflict records two live incompatible human decisions (e.g. accepting both
// split and merged proposals for the same overlapping peak cluster).
type Conflict struct {
	ID         string   `json:"id"`
	ProjectID  string   `json:"project_id"`
	Seq        int      `json:"seq"`
	StageKey   string   `json:"stage_key"`
	DecisionIDs []string `json:"decision_ids"`
	Status     string   `json:"status"` // open | resolved_keep_first | resolved_keep_second | resolved_reject_both
	Note       string   `json:"note,omitempty"`
	CreatedAt  string   `json:"created_at"`
	ResolvedAt string   `json:"resolved_at,omitempty"`
}
