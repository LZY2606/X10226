package domain

// NodeKind enumerates derived layers. Each recomputation produces at most one
// current node of each kind; old nodes stay readable but become stale.
type NodeKind string

const (
	NodeCorrectedSeries NodeKind = "corrected_series"
	NodeFurnaceTrace    NodeKind = "furnace_trace"
	NodeGasTimeline     NodeKind = "gas_timeline"
	NodeDTG             NodeKind = "dtg"
	NodeStages          NodeKind = "stages"
	NodeResidue         NodeKind = "residue_chain"
)

// Node is an immutable, content-addressed derived result.
type Node struct {
	ID         string         `json:"id"`
	ProjectID  string         `json:"project_id"`
	Kind       NodeKind       `json:"kind"`
	Algo       string         `json:"algo"`
	AlgoVer    string         `json:"algo_version"`
	ParamFP    string         `json:"param_fingerprint"`
	Inputs     []string       `json:"input_ids"` // source + node ids directly used
	Payload    map[string]any `json:"payload"`
	Diag       []Diag         `json:"diag,omitempty"`
	Status     string         `json:"status"` // ok | partial | unknown | error
	Current    bool           `json:"current"`
	SupersedBy string         `json:"superseded_by,omitempty"`
	CreatedEventSeq int       `json:"created_event_seq"`
	CreatedAt  string         `json:"created_at"`
}

// Params are reviewer-adjustable algorithm parameters. Changes create a new
// versioned parameter set with a fingerprint; only downstream nodes expire.
type Params struct {
	AtmoDelayS      map[string]float64 `json:"atmo_delay_s"` // gas -> delay seconds
	TempLagS        float64            `json:"temp_lag_s"`
	DTGSmoothWindow int                `json:"dtg_smooth_window"`
	DTGThreshold    float64            `json:"dtg_threshold_percent_per_min"`
	OverlapSplit    bool               `json:"overlap_split"`
	Version         int                `json:"version"`
	ParentFP        string             `json:"parent_fp,omitempty"`
}

// DefaultParams returns the v1 parameter set used by the case pack.
func DefaultParams() Params {
	return Params{
		AtmoDelayS:      map[string]float64{"O2": 45, "N2": 0},
		TempLagS:        30,
		DTGSmoothWindow: 5,
		DTGThreshold:    0.6,
		OverlapSplit:    true,
		Version:         1,
	}
}

// Fingerprint hashes the parameter content excluding the version bookkeeping.
func (p Params) Fingerprint() string {
	return Fingerprint(map[string]any{
		"atmo_delay_s":       p.AtmoDelayS,
		"temp_lag_s":         p.TempLagS,
		"dtg_smooth_window":  p.DTGSmoothWindow,
		"dtg_threshold":      p.DTGThreshold,
		"overlap_split":      p.OverlapSplit,
	})
}
