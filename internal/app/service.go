// Package app orchestrates storage, parser and engine behind an append-only
// event log, and computes current/as-of review views.
package app

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"tga-review/internal/clock"
	"tga-review/internal/domain"
	"tga-review/internal/importer"
	"tga-review/internal/storage"
)

const defaultActor = "reviewer"

type Service struct {
	Store  *storage.Store
	Clock  clock.Clock
	Parser *importer.Parser
}

func New(st *storage.Store, c clock.Clock) *Service {
	return &Service{Store: st, Clock: c, Parser: importer.New()}
}

func (s *Service) now() time.Time { return s.Clock.Now() }

func newID(prefix, seed string) string {
	h := sha1.Sum([]byte(seed))
	return fmt.Sprintf("%s-%s", prefix, hex.EncodeToString(h[:])[:12])
}

func (s *Service) CreateProject(name, sampleID, note string) (domain.Project, error) {
	id := newID("proj", name+"|"+sampleID+"|"+s.now().Format(time.RFC3339Nano))
	p := domain.Project{ID: id, Name: name, SampleID: sampleID, Note: note, CreatedAt: s.now()}
	if err := s.Store.CreateProject(p); err != nil {
		return p, err
	}
	_, _ = s.Store.AppendEvent(id, domain.EvProjectCreated, s.now(), defaultActor,
		map[string]any{"name": name, "sample_id": sampleID, "note": note})
	return p, nil
}

// CreateProjectWithID is used by export/import to preserve stable identities.
func (s *Service) CreateProjectWithID(p domain.Project) error {
	if err := s.Store.CreateProject(p); err != nil {
		return err
	}
	_, err := s.Store.AppendEvent(p.ID, domain.EvProjectCreated, p.CreatedAt, defaultActor,
		map[string]any{"name": p.Name, "sample_id": p.SampleID, "note": p.Note})
	return err
}

// Import parses and commits a submission. Half-failed batches are stored as
// quarantined records + an import_quarantined event and never advance derivations.
func (s *Service) Import(projectID, filename, mode string, raw []byte) (domain.Event, []domain.Diagnostic, error) {
	if mode != "full" && mode != "amend" {
		return domain.Event{}, nil, fmt.Errorf("mode must be full or amend")
	}
	if _, err := s.Store.GetProject(projectID); err != nil {
		return domain.Event{}, nil, err
	}
	batch := s.Parser.Parse(filename, mode, string(raw))
	evType := domain.EvImportAccepted
	if batch.Status == "quarantined" {
		evType = domain.EvImportQuarantine
	}
	payload := map[string]any{
		"filename": filename, "mode": mode, "status": batch.Status,
		"diagnostics": batch.Diagnostics, "raw_sha1": sha1Hex(raw),
	}
	ev, err := s.Store.AppendEvent(projectID, evType, s.now(), defaultActor, payload)
	if err != nil {
		return domain.Event{}, nil, err
	}
	now := s.now()
	for i := range batch.Records {
		batch.Records[i].ProjectID = projectID
		batch.Records[i].ImportSeq = ev.Seq
		batch.Records[i].CreatedAt = now
	}
	if err := s.Store.InsertRecords(batch.Records); err != nil {
		return domain.Event{}, nil, err
	}
	if batch.Status == "accepted" {
		if err := s.deriveFor(ev.Seq); err != nil {
			return domain.Event{}, nil, err
		}
	}
	return ev, batch.Diagnostics, nil
}

// ReviseParams appends a params_revised event; only dependent families are
// recomputed (gas timeline; corrected/dtg/proposals/residual/balance follow via
// the dependency graph in deriveFor).
func (s *Service) ReviseParams(projectID string, p domain.ParamsPayload) (domain.Event, error) {
	ev, err := s.Store.AppendEvent(projectID, domain.EvParamsRevised, s.now(), defaultActor, p)
	if err != nil {
		return domain.Event{}, err
	}
	if err := s.deriveFor(ev.Seq); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

func sha1Hex(b []byte) string {
	h := sha1.Sum(b)
	return hex.EncodeToString(h[:])
}

// paramsAt replays parameter revisions up to seq.
func (s *Service) paramsAt(projectID string, upTo int64, recs []domain.SourceRecord) domain.Params {
	par := domain.Params{DriftKind: "none"}
	// import-time defaults: params record and drift record of the latest layer
	for _, r := range recs {
		if r.ImportSeq > upTo || r.Status == "rejected" {
			continue
		}
		if r.Kind == "params" {
			f := r.Fields
			par.AtmosDelayS, _, _ = importer.NormalizeTime(f[2], f[3])
			par.TempLagC = atofSafe(f[5])
		}
		if r.Kind == domain.SrcDrift {
			f := r.Fields
			if f[1] == "linear" {
				par.DriftKind = "linear"
				rate := atofSafe(f[2])
				if f[3] == "mg/min" {
					rate *= 60
				}
				par.DriftRateMgH = rate
			} else {
				par.DriftKind = "none"
				par.DriftRateMgH = 0
			}
		}
	}
	events, err := s.Store.ListEvents(projectID)
	if err != nil {
		return par
	}
	for _, e := range events {
		if e.Seq > upTo {
			break
		}
		if e.Type == domain.EvUndo {
			var pl map[string]any
			_ = json.Unmarshal(e.Payload, &pl)
			// undoes a params revision: recompute excludes it below
		}
		if e.Type != domain.EvParamsRevised {
			continue
		}
		if isUndone(events, e.Seq, upTo) {
			continue
		}
		var pl domain.ParamsPayload
		_ = json.Unmarshal(e.Payload, &pl)
		if pl.AtmosDelayS != nil {
			par.AtmosDelayS = *pl.AtmosDelayS
		}
		if pl.TempLagC != nil {
			par.TempLagC = *pl.TempLagC
		}
		if pl.DriftRateMgH != nil {
			par.DriftRateMgH = *pl.DriftRateMgH
		}
		if pl.DriftKind != "" {
			par.DriftKind = pl.DriftKind
		}
		par.Seq = e.Seq
	}
	return par
}
