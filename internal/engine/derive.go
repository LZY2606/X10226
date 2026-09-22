package engine

import (
	"math"
	"sort"
	"strconv"

	"tga/internal/model"
)

// derive rebuilds every derived artifact from the current raw state.
//
// Artifact identity encodes the direct-source versions (event seqs) and the
// parameter fingerprint. Therefore a local source change yields NEW artifact
// ids only for artifacts that actually depend on it; the previous artifacts
// stay in the log viewable but are marked stale. Nothing old is mutated except
// its stale flag.
func (e *Engine) derive(st *State) error {
	params := st.ActiveParams()

	blankSub := st.ActiveSubmission(model.LayerBlank)
	sampleSub := st.ActiveSubmission(model.LayerSamples)
	m0Sub := st.ActiveSubmission(model.LayerM0)

	if blankSub != nil {
		e.upsert(st, e.blankModelArtifact(blankSub, params))
	}
	if sampleSub == nil {
		e.buildJudgments(st)
		return nil
	}

	cs := e.correctedSeriesArtifact(sampleSub, blankSub, params)
	e.upsert(st, cs)

	gaps := e.gapArtifacts(sampleSub, params)
	for _, g := range gaps {
		e.upsert(st, g)
	}
	for _, a := range e.nonMonotoneArtifacts(cs) {
		e.upsert(st, a)
	}

	dts := e.dtgArtifacts(cs, params, gaps)
	for _, a := range dts {
		e.upsert(st, a)
	}
	peaks := e.peakArtifacts(cs, dts, params)
	for _, a := range peaks {
		e.upsert(st, a)
	}

	stages := e.stageArtifacts(cs, peaks, dts, params)
	atm := e.atmosphereFor(st, params)
	associateStagesWithAtmosphere(stages, atm)
	for _, a := range stages {
		e.upsert(st, a)
	}
	for _, a := range atm {
		e.upsert(st, a)
	}

	if m0Sub != nil {
		m0a := e.initialMassArtifact(m0Sub, sampleSub)
		e.upsert(st, m0a)
		for _, a := range e.residueArtifacts(cs, stages, m0a, params) {
			e.upsert(st, a)
		}
	}

	e.buildJudgments(st)
	return nil
}

func (e *Engine) srcRef(sub *model.Submission, role string) model.SourceRef {
	if sub == nil {
		return model.SourceRef{Role: role}
	}
	return model.SourceRef{EventSeq: sub.EventSeq, SubmissionID: sub.ID, Layer: sub.Layer, Role: role}
}

func artRef(a *model.Artifact, role string) model.SourceRef {
	return model.SourceRef{EventSeq: a.CreatedSeq, SubmissionID: a.ID, Role: role}
}

func num(v string, def float64) float64 {
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return f
}

// sourceKey is the stable identity input for an artifact.
func sourceKey(kind string, sources []model.SourceRef) string {
	s := append([]model.SourceRef(nil), sources...)
	sort.Slice(s, func(i, j int) bool {
		if s[i].EventSeq != s[j].EventSeq {
			return s[i].EventSeq < s[j].EventSeq
		}
		return s[i].Role < s[j].Role
	})
	key := kind
	for _, r := range s {
		key += ":" + r.Role + "@" + strconv.FormatInt(r.EventSeq, 10)
	}
	return key
}

// upsert registers an artifact. Because the id already encodes source versions
// and parameters, an unchanged input reproduces the SAME id and is a no-op; a
// changed input produces a new id and the old current artifact is demoted.
func (e *Engine) upsert(st *State, a *model.Artifact) {
	if existing := st.Artifacts[a.ID]; existing != nil {
		return // deterministic reproduction: historical record untouched
	}
	if a.Status == "" {
		a.Status = "current"
	}
	st.Artifacts[a.ID] = a
	st.ArtifactIDs = append(st.ArtifactIDs, a.ID)
	e.demotePredecessors(st, a)
}

// demotePredecessors marks older current artifacts of the same kind whose
// direct source version is now superseded.
func (e *Engine) demotePredecessors(st *State, fresh *model.Artifact) {
	for _, oldID := range st.ArtifactIDs {
		if oldID == fresh.ID {
			continue
		}
		old := st.Artifacts[oldID]
		if old.Status != "current" || old.Kind != fresh.Kind {
			continue
		}
		if sameLogicalFamily(old, fresh) && sourcesSuperseded(old, fresh) {
			old.Status = "stale"
			old.SupersededBy = fresh.ID
		}
	}
}

// sameLogicalFamily groups revisions of one logical result (same role slots).
func sameLogicalFamily(a, b *model.Artifact) bool {
	ra := roleSet(a)
	rb := roleSet(b)
	if len(ra) != len(rb) {
		return false
	}
	for r := range ra {
		if !rb[r] {
			return false
		}
	}
	return true
}

func roleSet(a *model.Artifact) map[string]bool {
	m := map[string]bool{}
	for _, s := range a.Sources {
		m[s.Role] = true
	}
	return m
}

// sourcesSuperseded reports whether fresh uses a strictly newer version for at
// least one shared source role and never an older one.
func sourcesSuperseded(old, fresh *model.Artifact) bool {
	older, newer := false, false
	oldByRole, freshByRole := map[string]model.SourceRef{}, map[string]model.SourceRef{}
	for _, s := range old.Sources {
		oldByRole[s.Role] = s
	}
	for _, s := range fresh.Sources {
		freshByRole[s.Role] = s
	}
	for role, fo := range freshByRole {
		oo, ok := oldByRole[role]
		if !ok {
			continue
		}
		if fo.EventSeq > oo.EventSeq {
			newer = true
		}
		if fo.EventSeq < oo.EventSeq {
			older = true
		}
	}
	if !newer {
		// parameter-fingerprint change with identical source seqs
		if old.ParamFP != fresh.ParamFP {
			return true
		}
	}
	return newer && !older
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
