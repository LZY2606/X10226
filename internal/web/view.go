package web

import "tga-lineage/internal/project"

// View is the stable JSON contract consumed by the review page.
type View struct {
	Project         ProjectView    `json:"project"`
	Seq             int64          `json:"seq"`
	MaxSeq          int64          `json:"max_seq"`
	Historical      bool           `json:"historical"`
	Params          any            `json:"params"`
	Layers          []LayerView    `json:"layers"`
	Artifacts       []ArtifactView `json:"artifacts"`
	Stages          []StageView    `json:"stages"`
	Residuals       []ResidualView `json:"residuals"`
	Valves          []ValveView    `json:"valves"`
	Judgments       []JudgmentView `json:"judgments"`
	Decisions       []DecisionView `json:"decisions"`
	History         []EventView    `json:"history"`
	Counterexamples []CounterView  `json:"counterexamples"`
	Charts          ChartsView     `json:"charts"`
}

type ProjectView struct {
	UID, Name, Description, CreatedAt string
}
type LayerView struct {
	UID, Kind, Version, Parent, Source, SHA256, Status string
	RecordCount                                        int               `json:"record_count"`
	DiagCount                                          int               `json:"diag_count"`
	Diagnostics                                        []DiagView        `json:"diagnostics"`
	Basis                                              map[string]string `json:"basis"`
}
type DiagView struct {
	Ref, Field, Raw, Code, Message string
}
type ArtifactView struct {
	UID, Kind, Alg, ParamUID, ParamsFP, Status string
	Direct, DependsOn                          []string
	Fresh                                      bool
	Note                                       string
	PointCount                                 int            `json:"point_count"`
	Reasons                                    []string       `json:"reasons,omitempty"`
	Decisions                                  []DecisionView `json:"decisions,omitempty"`
}
type StageView struct {
	UID                                                                           string
	Index                                                                         int
	StartMin, EndMin, PeakMin, PeakTempC, DTGPeak, StartMassMg, EndMassMg, LossMg float64
	LossPct                                                                       *float64
	OverlapsWith                                                                  []int
	GasAtStart, GasAtPeak                                                         string
	EffStartMin, EffEndMin                                                        float64
	Certain, Status                                                               string
	Reasons                                                                       []string
	GapLimited                                                                    bool
	ArtifactUID                                                                   string
	Fresh                                                                         bool
	Decision                                                                      *DecisionView
}
type ChainNodeView struct {
	UID, Label, Value, Unit, Source, Note string
	Number                                *float64
}
type ResidualView struct {
	UID, Label, Certain, ArtifactUID string
	AtMin, WindowMin                 float64
	ValuePct                         *float64
	Reasons                          []string
	Chain                            []ChainNodeView
	Fresh                            bool
	Decision                         *DecisionView
}
type ValveView struct {
	UID, Gas                       string
	ValveAtMin, EffAtMin, DelayMin float64
	Fresh                          bool
}
type JudgmentView struct {
	Code, Severity, Subject, Statement, Rule string
	Refs                                     []string
}
type DecisionView struct {
	UID, Target, Kind, Verdict, Note, ParamsUID, ConflictsWith, At string
}
type EventView struct {
	Seq                              int64 `json:"seq"`
	UID, Kind, At, Note, PayloadType string
}
type CounterView struct {
	UID, Scenario, Project, DirectSource, Verdict string
	Affected, Unaffected, ExpectedScope           []string
	BlastOK, Adopted                              bool
	RejectionNote                                 string
}
type ChartsView struct {
	TG, DTG, Furnace, Timeline string
}
