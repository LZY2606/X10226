package app

import (
	"fmt"

	"gotga/internal/calc"
	"gotga/internal/domain"
)

// recompute rebuilds the derived surface at head. Identity is content-addressed
// so unchanged objects keep stable ids; the snapshot marks only the recomputed
// kinds and records why prior versions went stale.
func (a *App) recompute(projectID string, eventSeq int64) error {
	params, err := a.CurrentParams(projectID)
	if err != nil {
		return err
	}
	docs, err := a.effectiveDocs(projectID, eventSeq)
	if err != nil {
		return err
	}
	in, diags, err := calc.BuildInputs(docs)
	if err != nil {
		return err
	}
	b := calc.Compute(in, params, eventSeq, diags)
	reason := fmt.Sprintf("recomputed at event %d", eventSeq)
	return a.Store.SaveDerivedSnapshot(projectID, b,
		[]string{"series", "peak", "stage", "residue", "judgment"}, reason)
}

func (a *App) effectiveDocs(projectID string, seq int64) (map[domain.DocKind]*domain.RawDoc, error) {
	kinds := []domain.DocKind{
		domain.KindSamples, domain.KindDrift, domain.KindFurnace,
		domain.KindGas, domain.KindBlank, domain.KindMass0,
	}
	docs := map[domain.DocKind]*domain.RawDoc{}
	for _, k := range kinds {
		d, err := a.Store.LatestDocAsOf(projectID, k, seq)
		if err != nil {
			return nil, err
		}
		if d != nil {
			docs[k] = d
		}
	}
	return docs, nil
}

// Snapshot is the fully traceable view current at one event, including the
// effective documents/params, decisions and stale objects.
type Snapshot struct {
	ProjectID string               `json:"project_id"`
	EventSeq  int64                `json:"event_seq"`
	Params    domain.Params        `json:"params"`
	ParamFP   string               `json:"param_fp"`
	Bundle    *domain.ResultBundle `json:"bundle"`
	Docs      []*domain.RawDoc     `json:"docs"`
	Decisions []domain.Decision    `json:"decisions"`
	Stale     []StaleObject        `json:"stale"`
	Events    []domain.Event       `json:"events"`
	Conflict  bool                 `json:"conflict_present"`
}

// StaleObject exposes an expired derived value with its staleness reason; it
// remains readable but is never returned as current.
type StaleObject struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	BornSeq  int64  `json:"born_seq"`
	StaleSeq int64  `json:"stale_seq"`
	Reason   string `json:"reason"`
	JSON     string `json:"json"`
}

// Head returns the current (head event) snapshot with decisions applied.
func (a *App) Head(projectID string) (*Snapshot, error) {
	events, err := a.Store.Events(projectID)
	if err != nil {
		return nil, err
	}
	var head int64
	if len(events) > 0 {
		head = events[len(events)-1].Seq
	}
	return a.ViewAt(projectID, head)
}

// ViewAt rebuilds the view at any historical event. It recomputes from the
// documents and parameter version effective then, never overwriting history
// with newer results.
func (a *App) ViewAt(projectID string, seq int64) (*Snapshot, error) {
	params := domain.DefaultParams()
	events, err := a.Store.Events(projectID)
	if err != nil {
		return nil, err
	}
	var upto []domain.Event
	for _, e := range events {
		if e.Seq <= seq {
			upto = append(upto, e)
			if e.Kind == domain.EvParamRevised {
				var pl domain.ParamRevisePayload
				if err := jsonU(e.Payload, &pl); err == nil {
					params = pl.Params
				}
			}
		}
	}
	docs, err := a.effectiveDocs(projectID, seq)
	if err != nil {
		return nil, err
	}
	in, diags, err := calc.BuildInputs(docs)
	if err != nil {
		return nil, err
	}
	bundle := calc.Compute(in, params, seq, diags)

	// Apply non-revoked decisions effective at seq onto the fresh bundle.
	allDec, _ := a.Store.Decisions(projectID)
	var decs []domain.Decision
	conflict := false
	for _, d := range allDec {
		if d.EventSeq <= seq {
			decs = append(decs, d)
			if len(d.ConflictWith) > 0 && !d.Revoked {
				conflict = true
			}
		}
	}
	applyDecisions(bundle, decs)

	var docList []*domain.RawDoc
	for _, d := range docs {
		docList = append(docList, d)
	}
	stale, err := a.staleAsOf(projectID, seq)
	if err != nil {
		return nil, err
	}
	return &Snapshot{
		ProjectID: projectID, EventSeq: seq, Params: params,
		ParamFP: domain.ParamFingerprint(params), Bundle: bundle,
		Docs: docList, Decisions: decs, Stale: stale, Events: upto, Conflict: conflict,
	}, nil
}

func applyDecisions(b *domain.ResultBundle, ds []domain.Decision) {
	verdict := map[string]string{}
	reasons := map[string]string{}
	for _, d := range ds {
		key := d.SubjectType + ":" + d.SubjectID
		if d.Revoked {
			continue
		}
		verdict[key] = d.Verdict
		reasons[key] = d.Reason
	}
	for i := range b.Stages {
		if v := verdict["stage:"+b.Stages[i].ID]; v != "" {
			if v == "accepted" {
				b.Stages[i].Status = "accepted"
			} else {
				b.Stages[i].Status = "rejected"
				b.Stages[i].RejectReason = reasons["stage:"+b.Stages[i].ID]
			}
		}
	}
	for i := range b.Residues {
		if v := verdict["residue:"+b.Residues[i].ID]; v != "" {
			if v == "rejected" {
				b.Residues[i].Status = "rejected"
			}
		}
	}
}

func (a *App) staleAsOf(projectID string, seq int64) ([]StaleObject, error) {
	rows, err := a.Store.DerivedList(projectID)
	if err != nil {
		return nil, err
	}
	var out []StaleObject
	for _, r := range rows {
		if r.StaleSeq.Valid && r.StaleSeq.Int64 <= seq {
			out = append(out, StaleObject{
				ID: r.ID, Kind: r.Kind, BornSeq: r.BornSeq,
				StaleSeq: r.StaleSeq.Int64, Reason: r.StaleReason.String, JSON: r.JSON,
			})
		}
	}
	return out, nil
}

func jsonU(s string, v any) error {
	return jsonUnmarshal([]byte(s), v)
}
