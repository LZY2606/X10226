package httpapi

import (
	"tga/internal/engine"
	"tga/internal/model"
)

type submissionView struct {
	model.Submission
	Active bool `json:"active"`
}

type artifactView struct {
	*model.Artifact
	Decision *model.Decision `json:"decision,omitempty"`
}

type view struct {
	Project      *model.Project              `json:"project"`
	AtSeq        int64                       `json:"at_seq"`
	HeadSeq      int64                       `json:"head_seq"`
	IsHistorical bool                        `json:"is_historical"`
	Submissions  map[string][]submissionView `json:"submissions"`
	Params       *model.Params               `json:"params"`
	Decisions    []*model.Decision           `json:"decisions"`
	Artifacts    []artifactView              `json:"artifacts"`
	Judgments    []model.Judgment            `json:"judgments"`
	Kinds        map[string][]string         `json:"kinds"`
	Defaults     map[string]string           `json:"defaults"`
}

func buildView(st *engine.State, headSeq int64) view {
	v := view{
		Project:      st.Project,
		AtSeq:        st.AtSeq,
		HeadSeq:      headSeq,
		IsHistorical: headSeq > 0 && st.AtSeq < headSeq,
		Submissions:  map[string][]submissionView{},
		Params:       st.ActiveParams(),
		Decisions:    st.Decisions,
		Judgments:    st.Judgments,
		Kinds:        map[string][]string{},
		Defaults:     engine.DefaultParams(),
	}
	for _, layer := range model.AllLayers() {
		active := st.ActiveSubmission(layer)
		for _, sub := range st.Submissions[layer] {
			v.Submissions[string(layer)] = append(v.Submissions[string(layer)],
				submissionView{Submission: *sub, Active: active != nil && active.ID == sub.ID})
		}
	}
	dec := st.ActiveDecisions()
	for _, id := range st.ArtifactIDs {
		a := st.Artifacts[id]
		v.Artifacts = append(v.Artifacts, artifactView{Artifact: a, Decision: dec[a.ID]})
		v.Kinds[a.Kind] = append(v.Kinds[a.Kind], a.ID)
	}
	return v
}
