package tga

import "time"

type SourceKind string

const (
	SrcSamples SourceKind = "samples"
	SrcDrift   SourceKind = "driftcorr"
	SrcFurnace SourceKind = "furnace"
	SrcGas     SourceKind = "gas"
	SrcBlank   SourceKind = "blank"
	SrcInitial SourceKind = "initial"
)

func SourceKinds() []SourceKind {
	return []SourceKind{SrcSamples, SrcDrift, SrcFurnace, SrcGas, SrcBlank, SrcInitial}
}

func (k SourceKind) Title() string {
	switch k {
	case SrcSamples:
		return "原始采样点"
	case SrcDrift:
		return "天平漂移校正"
	case SrcFurnace:
		return "炉程序"
	case SrcGas:
		return "气体阀事件"
	case SrcBlank:
		return "空白曲线"
	case SrcInitial:
		return "样品初重"
	}
	return string(k)
}

// SourceVersion is an immutable commit of one direct source. Revisions are
// new versions pointing at a parent version; committed raw text is never
// overwritten.
type SourceVersion struct {
	ID          string     `json:"id"`
	Kind        SourceKind `json:"kind"`
	ParentID    string     `json:"parent_id,omitempty"`
	Hash        string     `json:"hash"`
	Raw         string     `json:"raw"`
	Note        string     `json:"note,omitempty"`
	CommittedAt time.Time  `json:"committed_at"`
}

// Diag records an import problem; Fragment keeps the offending original
// representation in place instead of discarding it for plotting.
type Diag struct {
	Code    string `json:"code"`
	Where   string `json:"where"`
	Message string `json:"message"`
	Raw     string `json:"raw,omitempty"`
}

type Fragment struct {
	SourceID string `json:"source_id"`
	Where    string `json:"where"`
	Raw      string `json:"raw"`
	Reason   string `json:"reason"`
}

type Decision struct {
	ID          string    `json:"id"`
	CandidateID string    `json:"candidate_id"`
	Verdict     string    `json:"verdict"`
	Note        string    `json:"note,omitempty"`
	At          time.Time `json:"at"`
}

type Conflict struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Detail      string   `json:"detail"`
	DecisionIDs []string `json:"decision_ids"`
}
