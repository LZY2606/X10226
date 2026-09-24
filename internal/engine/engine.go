package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"tga-review/internal/derive"
	"tga-review/internal/domain"
	"tga-review/internal/store"
)

// FixedSeed is the required deterministic seed for the offline case.
const FixedSeed int64 = 20260924

// Engine binds the virtual clock, store and pure projector.
type Engine struct {
	Store *store.Store
	Clock *VirtualClock
}

func New(st *store.Store, start time.Time) *Engine {
	return &Engine{Store: st, Clock: NewVirtualClock(start)}
}

// CreateProject opens a new review project with a fixed seed.
func (e *Engine) CreateProject(ctx context.Context, id, name string) error {
	if exists, err := e.Store.ProjectExists(ctx, id); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("project %s already exists", id)
	}
	now := e.Clock.Now()
	if err := e.Store.CreateProject(ctx, id, name, FixedSeed, now.UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	seq, err := e.Store.NextSeq(ctx, id)
	if err != nil {
		return err
	}
	return e.append(ctx, &domain.Event{
		ID:        eventID(domain.EvProjectCreated, seq),
		ProjectID: id,
		Type:      domain.EvProjectCreated,
		AtVirtual: now,
		Payload: map[string]any{
			"project_id": id,
			"name":       name,
			"seed":       FixedSeed,
		},
	})
}

func (e *Engine) append(ctx context.Context, ev *domain.Event) error {
	if ev.AtVirtual.IsZero() {
		ev.AtVirtual = e.Clock.Now()
	}
	return e.Store.AppendEvent(ctx, ev)
}

// ImportBatchResult reports per-record status so a half-failed import keeps
// the good fragments and diagnostics for the bad ones.
type ImportBatchResult struct {
	ImportID string             `json:"import_id"`
	Accepted []string           `json:"accepted"`
	Failed   []RecordFailure    `json:"failed"`
}

type RecordFailure struct {
	LogicalID string `json:"logical_id"`
	Name      string `json:"name"`
	Layer     string `json:"layer"`
	Error     string `json:"error"`
}

// ImportBatch ingests raw documents. Every record (accepted or failed) is
// stored first as an immutable raw_record_added; only accepted records are
// then installed into strata as layer version 1. One bad record never aborts
// the whole batch.
func (e *Engine) ImportBatch(ctx context.Context, projectID string, docs []RawDoc) (*ImportBatchResult, error) {
	events, err := e.Store.Events(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	st, err := derive.Replay(events)
	if err != nil {
		return nil, err
	}
	if st.ReviewStage != domain.StageImporting && st.ReviewStage == "" {
		return nil, fmt.Errorf("project not found or not importing")
	}

	now := e.Clock.Now()
	seq, err := e.Store.NextSeq(ctx, projectID)
	if err != nil {
		return nil, err
	}
	importID := batchID(FixedSeed, now)
	res := &ImportBatchResult{ImportID: importID}

	if err := e.append(ctx, &domain.Event{
		ID:        eventID(domain.EvImportOpened, seq),
		ProjectID: projectID,
		Type:      domain.EvImportOpened,
		AtVirtual: now,
		Payload:   map[string]any{"import_id": importID},
	}); err != nil {
		return nil, err
	}

	type accepted struct {
		doc    RawDoc
		strata parsedStrata
		rec    domain.RawRecord
	}
	var good []accepted
	for i, doc := range docs {
		seq, _ = e.Store.NextSeq(ctx, projectID)
		rec := domain.RawRecord{
			ID:     recordID(importID, logicalID(mapFromDoc(doc)), i),
			Layer:  domain.Layer(doc.Layer),
			Name:   doc.Name,
			Format: doc.Format,
			Body:   doc.Body,
		}
		par, perr := parseDoc(doc, len(st.RawPoints))
		if perr != nil {
			rec.OK = false
			rec.Error = perr.Error()
			res.Failed = append(res.Failed, RecordFailure{
				LogicalID: doc.LogicalID, Name: doc.Name, Layer: doc.Layer, Error: perr.Error(),
			})
		} else {
			rec.OK = true
			res.Accepted = append(res.Accepted, rec.ID)
			good = append(good, accepted{doc: doc, strata: par, rec: rec})
		}
		if err := e.appendRawRecord(ctx, projectID, importID, rec); err != nil {
			return nil, err
		}
	}

	// Install accepted strata. For the initial import these are version 1;
	// re-importing a layer during the review is a revision (new version with a
	// parent) handled by ReviseLayer.
	for _, g := range good {
		if current, ok := st.RawVersions[domain.Layer(g.doc.Layer)]; ok {
			if err := e.installLayerRevision(ctx, projectID, g.doc, g.strata, current.Version+1, current.StableID,
				"re-imported record "+g.rec.ID); err != nil {
				return nil, err
			}
		} else {
			if err := e.installLayerRevision(ctx, projectID, g.doc, g.strata, 1, "",
				"initial import "+g.rec.ID); err != nil {
				return nil, err
			}
		}
	}

	// Default parameters + drift selection are installed once on first import.
	events, err = e.Store.Events(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	st, err = derive.Replay(events)
	if err != nil {
		return nil, err
	}
	if len(st.ParamsHistory) == 0 {
		p := derive.DefaultParameters(layerStableID("params", "default-v1"))
		if err := e.installParameters(ctx, projectID, p, "default analysis parameters"); err != nil {
			return nil, err
		}
	if err := e.installDrift(ctx, projectID, "blank_zeroed", "天平基线漂移校正（默认）"); err != nil {
			return nil, err
		}
	}

	seq, _ = e.Store.NextSeq(ctx, projectID)
	if err := e.append(ctx, &domain.Event{
		ID:        eventID(domain.EvImportClosed, seq),
		ProjectID: projectID,
		Type:      domain.EvImportClosed,
		AtVirtual: e.Clock.Now(),
		Payload: map[string]any{
			"import_id": importID,
			"next_stage": domain.StageInputCheck,
			"accepted":   res.Accepted,
			"failed":     failuresToAny(res.Failed),
		},
	}); err != nil {
		return nil, err
	}

	if _, err := e.Recompute(ctx, projectID); err != nil {
		return nil, err
	}
	return res, nil
}

func mapFromDoc(doc RawDoc) map[string]any {
	b, _ := json.Marshal(doc)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func failuresToAny(fs []RecordFailure) []any {
	out := make([]any, 0, len(fs))
	for _, f := range fs {
		b, _ := json.Marshal(f)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		out = append(out, m)
	}
	return out
}

func (e *Engine) appendRawRecord(ctx context.Context, projectID, importID string, rec domain.RawRecord) error {
	seq, err := e.Store.NextSeq(ctx, projectID)
	if err != nil {
		return err
	}
	var recAny map[string]any
	b, _ := json.Marshal(rec)
	_ = json.Unmarshal(b, &recAny)
	return e.append(ctx, &domain.Event{
		ID:        eventID(domain.EvRawRecordAdded, seq),
		ProjectID: projectID,
		Type:      domain.EvRawRecordAdded,
		AtVirtual: e.Clock.Now(),
		Payload: map[string]any{
			"import_id": importID,
			"record":    recAny,
		},
	})
}
