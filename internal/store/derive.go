package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"tga-review/internal/domain"
)

// ResolveInput loads the latest-per-layer immutable submissions and builds
// the analysis input with exact source references.
func (s *Store) ResolveInput(pid string) (domain.AnalysisInput, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return domain.AnalysisInput{}, err
	}
	defer tx.Rollback()
	return resolveInputLocked(tx, pid)
}

func resolveInputLocked(tx *sql.Tx, pid string) (domain.AnalysisInput, error) {
	in := domain.AnalysisInput{Params: domain.DefaultParams()}
	for _, layer := range domain.AllLayers {
		sub, err := latestSubmissionLocked(tx, pid, layer)
		if err != nil {
			return in, err
		}
		if sub == nil {
			continue
		}
		ref := domain.SourceRef{Layer: layer, SubID: sub.ID, Hash: sub.PayloadHash}
		in.Sources = append(in.Sources, ref)
		switch layer {
		case domain.LayerRawPoints:
			in.Points = sub.Points
		case domain.LayerDrift:
			in.Drift = sub.Drift
		case domain.LayerFurnace:
			in.Furnace = sub.Furnace
		case domain.LayerGasValve:
			in.Valves = sub.Valves
		case domain.LayerBlankCurve:
			in.Blank = sub.Blank
		case domain.LayerInitialMass:
			in.InitialMass = sub.InitialMass
		case domain.LayerParams:
			if sub.Params != nil {
				in.Params = *sub.Params
			}
		}
	}
	return in, nil
}

// deriveLocked runs the immutable algorithm, stores the derivation, and emits
// machine events carrying or expiring prior decisions according to the local
// dependency scope. Stored derivations are never mutated afterwards.
func deriveLocked(tx *sql.Tx, pid, triggerEventID string, at time.Time) (*domain.Derivation, error) {
	in, err := resolveInputLocked(tx, pid)
	if err != nil {
		return nil, err
	}
	seq, err := nextSeqLocked(tx, "derivations", pid)
	if err != nil {
		return nil, err
	}
	id := domain.DerivationID(pid, seq)
	d := domain.Analyze(in, pid, seq, triggerEventID, at, id)
	data, _ := json.Marshal(d)
	if _, err := tx.Exec(
		`INSERT INTO derivations(id, project_id, seq, event_id, data) VALUES(?,?,?,?,?)`,
		d.ID, pid, seq, triggerEventID, string(data)); err != nil {
		return nil, err
	}

	// Carry / expire previous active decisions based on dependency scope.
	prev, err := latestDerivationBeforeLocked(tx, pid, d.ID)
	if err == nil && prev != nil {
		changed := domain.ChangedLayers(prev.Sources, d.Sources)
		if prev.ParamsHash != d.ParamsHash {
			changed[domain.LayerParams] = true
		}
		decisions, err := activeDecisionsAtLocked(tx, pid, maxEventSeqLocked)
		if err != nil {
			return nil, err
		}
		candidateDeps := map[string][]domain.Layer{}
		for _, c := range d.Stages {
			candidateDeps[c.Key] = c.DependsOn
		}
		curCandidates := map[string]bool{}
		for _, c := range d.Stages {
			curCandidates[c.Key] = true
		}
		curResidues := map[string]bool{}
		for _, r := range d.Residues {
			curResidues[r.Key] = true
		}
		residueChanged := changed[domain.LayerRawPoints] ||
			changed[domain.LayerBlankCurve] || changed[domain.LayerDrift] ||
			changed[domain.LayerInitialMass] || changed[domain.LayerParams]
		for _, dec := range decisions {
			if !dec.Active || dec.DerivationID != prev.ID {
				continue
			}
			eseq, err := nextEventSeqLocked(tx, pid)
			if err != nil {
				return nil, err
			}
			eid := domain.EventStableID(pid, eseq)
			affected := false
			present := false
			switch dec.TargetKind {
			case "candidate":
				deps, ok := candidateDeps[dec.TargetKey]
				present = curCandidates[dec.TargetKey]
				affected = ok && domain.DependsOn(deps, changed)
				if !ok {
					affected = true // newly absent/re-numbered => expire
				}
			case "residue":
				present = curResidues[dec.TargetKey]
				affected = residueChanged || !present
			}
			typ := "decision_carried"
			note := ""
			if affected || !present {
				typ = "decision_expired"
				note = "直接来源变化，局部依赖过期，需要重新裁定"
			}
			if _, err := tx.Exec(
				`INSERT INTO events(id, project_id, seq, type, at, payload) VALUES(?,?,?,?,?,?)`,
				eid, pid, eseq, typ, at.Format(time.RFC3339Nano),
				mustJSON(map[string]any{
					"target_kind": dec.TargetKind, "target_key": dec.TargetKey,
					"from_derivation": prev.ID, "to_derivation": d.ID,
					"prior_action": dec.Action, "decision_id": dec.ID,
					"affected_layers": layerKeys(changed), "note": note,
				})); err != nil {
				return nil, err
			}
		}
	}
	return d, nil
}

// maxEventSeqLocked is a sentinel handled inside activeDecisionsAtLocked.
const maxEventSeqLocked = -1

func layerKeys(m map[domain.Layer]bool) []string {
	var out []string
	for l, v := range m {
		if v {
			out = append(out, string(l))
		}
	}
	return out
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func latestDerivationLocked(tx *sql.Tx, pid string) (*domain.Derivation, error) {
	var data, id string
	err := tx.QueryRow(
		`SELECT data, id FROM derivations WHERE project_id=? ORDER BY seq DESC LIMIT 1`, pid).
		Scan(&data, &id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d domain.Derivation
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func latestDerivationBeforeLocked(tx *sql.Tx, pid, excludeID string) (*domain.Derivation, error) {
	var data string
	err := tx.QueryRow(
		`SELECT data FROM derivations WHERE project_id=? AND id<>? ORDER BY seq DESC LIMIT 1`,
		pid, excludeID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d domain.Derivation
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// derivationAtEventLocked returns the derivation produced by an event.
func derivationAtEventLocked(tx *sql.Tx, pid, eventID string) (*domain.Derivation, error) {
	var data string
	err := tx.QueryRow(
		`SELECT data FROM derivations WHERE project_id=? AND event_id=?`, pid, eventID).
		Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var d domain.Derivation
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *Store) GetDerivation(pid, id string) (*domain.Derivation, error) {
	var data string
	err := s.db.QueryRow(`SELECT data FROM derivations WHERE project_id=? AND id=?`, pid, id).
		Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var d domain.Derivation
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// Redrive forces a new derivation using current sources.
func (s *Store) Redrive(pid, note string) (*domain.Derivation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	eseq, err := nextEventSeqLocked(tx, pid)
	if err != nil {
		return nil, err
	}
	eid, err := appendEvent(tx, pid, eseq, "manual_recompute", s.now(), map[string]any{"note": note})
	if err != nil {
		return nil, err
	}
	d, err := deriveLocked(tx, pid, eid, s.now())
	if err != nil {
		return nil, fmt.Errorf("redrive: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return d, nil
}

func scanDerivation(data string) (*domain.Derivation, error) {
	var d domain.Derivation
	if err := json.Unmarshal([]byte(data), &d); err != nil {
		return nil, err
	}
	return &d, nil
}
