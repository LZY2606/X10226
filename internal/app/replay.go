package app

import (
	"encoding/json"
	"fmt"
	"strconv"

	"tga-review/internal/domain"
	"tga-review/internal/engine"
)

// deriveFor recomputes artifacts as of the current event seq and stores any
// new stable ids. Decisions already in the log are re-applied to proposal
// statuses each time, so re-import replay yields identical ids.
func (s *Service) deriveFor(seq int64) error {
	events, err := s.Store.ListEvents("")
	_ = events
	projects, err := s.Store.ListProjects()
	if err != nil {
		return err
	}
	for _, p := range projects {
		if err := s.deriveProjectAt(p.ID, 0); err != nil {
			return err
		}
	}
	return nil
}

// deriveProjectAt computes artifacts as of seq upTo (0 == latest) and stores
// newly seen artifacts. It is pure given the event log and raw records.
func (s *Service) deriveProjectAt(projectID string, upTo int64) error {
	events, err := s.Store.ListEvents(projectID)
	if err != nil {
		return err
	}
	maxSeq := int64(0)
	lastImport := int64(0)
	for _, e := range events {
		if upTo > 0 && e.Seq > upTo {
			break
		}
		if effective(events, e, upTo) && e.Type == domain.EvImportAccepted {
			lastImport = e.Seq
		}
		maxSeq = e.Seq
	}
	if upTo > 0 {
		maxSeq = upTo
	}
	if lastImport == 0 {
		return nil
	}
	recs, err := s.Store.ListRecords(projectID)
	if err != nil {
		return err
	}
	params := s.paramsAt(projectID, maxSeq, recs)
	snap := engine.BuildSnapshot(recs, lastImport, params)
	arts, err := engine.Derive(snap, lastImport)
	if err != nil {
		return err
	}
	// Apply decision history to the candidate set payload, producing adjusted
	// artifacts. All versions stay stored; views pick the newest at each seq.
	arts, err = s.applyDecisions(projectID, arts, events, maxSeq)
	if err != nil {
		return err
	}
	for i := range arts {
		arts[i].ProjectID = projectID
		if arts[i].CreatedSeq == 0 || arts[i].CreatedSeq > maxSeq {
			arts[i].CreatedSeq = maxSeq
		}
		if err := s.Store.PutArtifact(arts[i]); err != nil {
			return err
		}
	}
	return nil
}

// effective reports whether an event contributes to the state at upTo, i.e.
// it is not later undone.
func effective(events []domain.Event, e domain.Event, upTo int64) bool {
	if e.Seq > upTo && upTo > 0 {
		return false
	}
	return !isUndone(events, e.Seq, upTo)
}

func isUndone(events []domain.Event, seq, upTo int64) bool {
	for _, e := range events {
		if upTo > 0 && e.Seq > upTo {
			break
		}
		if e.Type != domain.EvUndo {
			continue
		}
		var pl struct {
			TargetSeq int64 `json:"target_seq"`
		}
		_ = json.Unmarshal(e.Payload, &pl)
		if pl.TargetSeq == seq {
			return true
		}
	}
	return false
}

func atofSafe(x string) float64 {
	v, _ := strconv.ParseFloat(x, 64)
	return v
}

var _ = fmt.Sprintf
