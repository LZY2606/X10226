package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"tgaaudit/internal/domain"
	"tgaaudit/internal/pipeline"
)

// ExportBundle is the portable, events-only representation of a project.
type ExportBundle struct {
	Format      string         `json:"format"`
	Version     int            `json:"version"`
	ExportedAt  string         `json:"exported_at"`
	ProjectID   string         `json:"project_id"`
	ProjectName string         `json:"project_name"`
	Seed        int64          `json:"seed"`
	Events      []domain.Event `json:"events"`
	Checksum    string         `json:"checksum"`
}

// Export writes the complete event log of a project.
func (s *Store) Export(ctx context.Context, projectID string) (*ExportBundle, error) {
	events, err := s.eventsUntil(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	var name string
	var seed int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT name, seed FROM projects WHERE id=?`, projectID).
		Scan(&name, &seed); err != nil {
		return nil, err
	}
	b := &ExportBundle{
		Format: "tgaaudit-events", Version: 1,
		ExportedAt: s.now().Format(timeLayout),
		ProjectID:  projectID, ProjectName: name, Seed: seed,
		Events: events,
	}
	b.Checksum = bundleChecksum(events)
	return b, nil
}

func bundleChecksum(events []domain.Event) string {
	h := sha256.New()
	for _, ev := range events {
		fmt.Fprintf(h, "%d:%s:", ev.Seq, ev.Hash)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyChain re-computes the hash chain of an export and returns the first
// broken sequence, or -1 when intact.
func VerifyChain(b *ExportBundle) (int, error) {
	prev := ""
	for _, ev := range b.Events {
		sum := sha256.Sum256(append(append([]byte(prev), ev.Payload...),
			[]byte(ev.At)...))
		if hex.EncodeToString(sum[:]) != ev.Hash {
			return ev.Seq, nil
		}
		prev = ev.Hash
	}
	if bundleChecksum(b.Events) != b.Checksum {
		return -1, fmt.Errorf("bundle checksum mismatch")
	}
	return -1, nil
}

// ImportEvents restores a project from an event-only bundle into this store,
// preserving event ids, order and hashes. Projections are rebuilt by replay,
// and the latest derivation is recomputed deterministically.
func (s *Store) ImportEvents(ctx context.Context, b *ExportBundle, newName string) (string, error) {
	if seq, err := VerifyChain(b); err != nil {
		return "", fmt.Errorf("bundle verification failed: %w", err)
	} else if seq > 0 {
		return "", fmt.Errorf("bundle hash chain broken at event seq %d", seq)
	}
	projectID := b.ProjectID
	name := b.ProjectName
	if newName != "" {
		name = newName
	}
	// Insert events verbatim in a single transaction (no new hashing).
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at,seed,note) VALUES(?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		projectID, name, firstAt(b), b.Seed, "imported"); err != nil {
		return "", err
	}
	for _, ev := range b.Events {
		if ev.ProjectID != projectID {
			return "", fmt.Errorf("event %s belongs to another project", ev.ID)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(seq,id,project_id,at,type,payload,prev_hash,hash)
			 VALUES(?,?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO NOTHING`,
			ev.Seq, ev.ID, ev.ProjectID, ev.At, ev.Type, string(ev.Payload),
			ev.PrevHash, ev.Hash); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	// Rebuild projections by replaying the log.
	if err := s.rebuildProjections(ctx, projectID); err != nil {
		return "", err
	}
	return projectID, nil
}

func firstAt(b *ExportBundle) string {
	for _, ev := range b.Events {
		if ev.Type == domain.EvProjectCreated {
			return ev.At
		}
	}
	if len(b.Events) > 0 {
		return b.Events[0].At
	}
	return time.Now().UTC().Format(timeLayout)
}

// rebuildProjections materialises all tables from the event log. It is the
// single code path used after an export/import round trip.
func (s *Store) rebuildProjections(ctx context.Context, projectID string) error {
	events, err := s.eventsUntil(ctx, projectID, 0)
	if err != nil {
		return err
	}
	var name string
	var seed int64
	for _, ev := range events {
		switch ev.Type {
		case domain.EvProjectCreated:
			var pl struct {
				Name string `json:"name"`
				Seed int64  `json:"seed"`
				Note string `json:"note"`
			}
			_ = json.Unmarshal(ev.Payload, &pl)
			name, seed = pl.Name, pl.Seed
		case domain.EvCommitted:
			var bat domain.Batch
			if err := json.Unmarshal(ev.Payload, &bat); err != nil {
				return err
			}
			pj := string(ev.Payload)
			if _, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO batches(project_id,id,kind,version,parent_id,
				 committed_at,event_seq,payload) VALUES(?,?,?,?,?,?,?,?)`,
				projectID, bat.ID, bat.Kind, bat.Version, bat.ParentID,
				bat.CommittedAt, ev.Seq, pj); err != nil {
				return err
			}
		case domain.EvParamRevised:
			var pl struct {
				Params      domain.Params `json:"params"`
				Fingerprint string        `json:"fingerprint"`
			}
			if err := json.Unmarshal(ev.Payload, &pl); err != nil {
				return err
			}
			pj, _ := json.Marshal(pl.Params)
			if _, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO params_versions(project_id,fingerprint,
				 version_no,created_event_seq,payload) VALUES(?,?,?,?,?)`,
				projectID, pl.Fingerprint,
				paramVersionNo(events, ev.Seq), ev.Seq, string(pj)); err != nil {
				return err
			}
		case domain.EvImportAttempted:
			var att domain.ImportAttempt
			if err := json.Unmarshal(ev.Payload, &att); err != nil {
				return err
			}
			pj := string(ev.Payload)
			if _, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO attempts(project_id,id,at,ok,partial,
				 bundle_checksum,error,payload) VALUES(?,?,?,?,?,?,?,?)`,
				projectID, att.ID, att.At, b(att.OK), b(att.Partial),
				att.BundleChecksum, att.Error, pj); err != nil {
				return err
			}
		case domain.EvDecision:
			var dec domain.Decision
			if err := json.Unmarshal(ev.Payload, &dec); err != nil {
				return err
			}
			adj, _ := json.Marshal(dec.Adjustment)
			if _, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO decisions(project_id,id,event_seq,at,
				 by_name,target_kind,target_id,verdict,rationale,adjustment,
				 supersedes) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
				projectID, deterministicID("dec", projectID, dec.TargetID,
					fmt.Sprint(ev.Seq)),
				ev.Seq, ev.At, dec.By, dec.TargetKind, dec.TargetID,
				dec.Verdict, dec.Rationale, string(adj), dec.Supersedes); err != nil {
				return err
			}
		case domain.EvConflict:
			var c domain.Conflict
			if err := json.Unmarshal(ev.Payload, &c); err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO conflicts(project_id,id,at,decision_a,
				 decision_b,target_id) VALUES(?,?,?,?,?,?)`,
				projectID, c.ID, c.At, c.DecisionA, c.DecisionB, c.TargetID); err != nil {
				return err
			}
		case domain.EvResolved:
			var c domain.Conflict
			if err := json.Unmarshal(ev.Payload, &c); err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE conflicts SET resolved_event_seq=?, winner=?, reason=?
				 WHERE project_id=? AND id=?`,
				ev.Seq, c.Winner, c.Reason, projectID, c.ID); err != nil {
				return err
			}
		case domain.EvUndid:
			var u domain.UndoRecord
			if err := json.Unmarshal(ev.Payload, &u); err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx,
				`INSERT OR IGNORE INTO undos(project_id,id,at,event_seq,
				 undone_event_seq,kind,reason) VALUES(?,?,?,?,?,?,?)`,
				projectID, deterministicID("undo", projectID, fmt.Sprint(ev.Seq)),
				ev.At, ev.Seq, u.UndoneSeq, u.Kind, u.Reason); err != nil {
				return err
			}
			if _, err := s.db.ExecContext(ctx,
				`UPDATE decisions SET undone_event_seq=? WHERE project_id=?
				 AND event_seq=?`, ev.Seq, projectID, u.UndoneSeq); err != nil {
				return err
			}
		}
	}
	if name != "" {
		_, _ = s.db.ExecContext(ctx,
			`UPDATE projects SET name=?, seed=? WHERE id=?`, name, seed, projectID)
	}

	// Recompute the deterministic derivation and store nodes/rule events,
	// but do NOT append new derived events: imported event seq identity must
	// be preserved exactly.
	cur, err := s.currentBatches(ctx, projectID)
	if err != nil {
		return err
	}
	if cur[domain.KindSamples] == nil {
		return nil
	}
	params, err := s.currentParams(ctx, projectID)
	if err != nil {
		return err
	}
	der := pipeline.Derive(cur, params)
	latestDerivedSeq := 0
	for _, ev := range events {
		if ev.Type == domain.EvDerived {
			latestDerivedSeq = ev.Seq
		}
	}
	if latestDerivedSeq == 0 {
		latestDerivedSeq = len(events)
	}
	for _, n := range der.Nodes {
		n.EventSeq = latestDerivedSeq
		pj, _ := json.Marshal(n)
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO nodes(project_id,id,event_seq,set_id,kind,
			 algo,param_fp,payload) VALUES(?,?,?,?,?,?,?,?)`,
			projectID, n.ID, latestDerivedSeq, der.SetID, n.Kind,
			n.AlgorithmVersion, n.ParamFingerprint, string(pj)); err != nil {
			return err
		}
	}
	for i, re := range der.RuleEvents {
		refs, _ := json.Marshal(re.Refs)
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR IGNORE INTO rule_events(project_id,node_id,seq_in_set,
			 code,subject,severity,message,refs,set_id)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			projectID, der.SetID, i, re.Code, re.Subject, re.Severity,
			re.Message, string(refs), der.SetID); err != nil {
			return err
		}
	}
	return nil
}

func paramVersionNo(events []domain.Event, upToSeq int) int {
	n := 0
	for _, ev := range events {
		if ev.Seq > upToSeq {
			break
		}
		if ev.Type == domain.EvParamRevised {
			n++
		}
	}
	return n
}
