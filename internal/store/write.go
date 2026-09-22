package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"tga-review/internal/domain"
	"tga-review/internal/importer"
)

// InsertBatch persists a batch and its accepted layers. It is idempotent on
// batch id, so exporting, deleting the DB and re-importing reproduces the same
// primary keys and order.
func (s *Store) InsertBatch(ctx context.Context, projectID string, b domain.Batch, r *importer.Result) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM batches WHERE id=?`, b.ID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		acc, _ := json.Marshal(b.AcceptedLayers)
		rej, _ := json.Marshal(b.RejectedLayers)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO batches (id,project_id,created_at,kind,source_ref,accepted_layers,rejected_layers,note,payload)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			b.ID, projectID, b.CreatedAt, b.Kind, b.SourceRef, string(acc), string(rej), b.Note, string(b.Payload)); err != nil {
			return err
		}
	}
	for _, p := range r.Data.Samples {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO samples (project_id,batch_id,id,ordinal,raw_t,unit_t,t,raw_mass,unit_mass,mass,raw_temp,unit_temp,temp,convertible,diagnostic)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, b.ID, p.ID, p.Ordinal,
			p.T.Raw, p.T.Unit, nanNil(p.T.ConvertedValue),
			p.Mass.Raw, p.Mass.Unit, nanNil(p.Mass.ConvertedValue),
			p.Temp.Raw, p.Temp.Unit, nanNil(p.Temp.ConvertedValue),
			allConvertible(p.T, p.Mass, p.Temp), firstDiag(p.T, p.Mass, p.Temp)); err != nil {
			return err
		}
	}
	for _, d := range r.Data.Drift {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO drift (project_id,batch_id,id,ordinal,kind,raw_t,unit_t,t,raw_offset,unit_offset,offset_v,convertible,diagnostic)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, b.ID, d.ID, d.Ordinal, d.Kind,
			d.T.Raw, d.T.Unit, nanNil(d.T.ConvertedValue),
			d.Offset.Raw, d.Offset.Unit, nanNil(d.Offset.ConvertedValue),
			bothConvertible(d.T, d.Offset), firstDiag(d.T, d.Offset)); err != nil {
			return err
		}
	}
	for _, p := range r.Data.Program {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO program (project_id,batch_id,id,ordinal,label,t_start,t_end,temp_start,temp_end)
			 VALUES (?,?,?,?,?,?,?,?,?)`,
			projectID, b.ID, p.ID, p.Ordinal, p.Label,
			nanNil(p.TStart.ConvertedValue), nanNil(p.TEnd.ConvertedValue),
			nanNil(p.TempStart.ConvertedValue), nanNil(p.TempEnd.ConvertedValue)); err != nil {
			return err
		}
	}
	for _, g := range r.Data.Gas {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO gas (project_id,batch_id,id,ordinal,from_gas,to_gas,t,flow,raw_t,unit_t,convertible,diagnostic)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, b.ID, g.ID, g.Ordinal, g.FromGas, g.ToGas,
			nanNil(g.T.ConvertedValue), nanNil(g.Flow.ConvertedValue),
			g.T.Raw, g.T.Unit, btt(g.T.Convertible), g.T.Diagnostic); err != nil {
			return err
		}
	}
	for _, bl := range r.Data.Blank {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO blank (project_id,batch_id,id,ordinal,t,mass,raw_t,unit_t,raw_mass,unit_mass,convertible,diagnostic)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, b.ID, bl.ID, bl.Ordinal,
			nanNil(bl.T.ConvertedValue), nanNil(bl.Mass.ConvertedValue),
			bl.T.Raw, bl.T.Unit, bl.Mass.Raw, bl.Mass.Unit,
			bothConvertible(bl.T, bl.Mass), firstDiag(bl.T, bl.Mass)); err != nil {
			return err
		}
	}
	for _, im := range r.Data.Initial {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO initial_mass (project_id,batch_id,id,parent_id,ordinal,raw_mass,unit_mass,mass,reason,convertible,diagnostic)
			 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, b.ID, im.ID, im.ParentID, im.Ordinal,
			im.Mass.Raw, im.Mass.Unit, nanNil(im.Mass.ConvertedValue),
			im.Reason, btt(im.Mass.Convertible), im.Mass.Diagnostic); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func nanNil(v float64) *float64 {
	if v != v {
		return nil
	}
	return &v
}

func btt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func allConvertible(qs ...domain.Quantity) int { return btt(everyGood(qs)) }
func bothConvertible(a, b domain.Quantity) int  { return btt(a.Convertible && b.Convertible && a.Raw != "" && b.Raw != "") }
func everyGood(qs []domain.Quantity) bool {
	for _, q := range qs {
		if !q.Convertible || q.Raw == "" {
			return false
		}
	}
	return true
}
func firstDiag(qs ...domain.Quantity) string {
	var ds []string
	for _, q := range qs {
		if q.Diagnostic != "" {
			ds = append(ds, q.Diagnostic)
		}
	}
	return strings.Join(ds, "; ")
}

// CreateProject records a new project.
func (s *Store) CreateProject(ctx context.Context, id, name, createdAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects (id,name,created_at) VALUES (?,?,?)`, id, name, createdAt)
	return err
}

// AppendEvent stores a ledger entry at the given sequence.
func (s *Store) AppendEvent(ctx context.Context, ev domain.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events (id,project_id,seq,type,occurred_at,reviewer,undoes,conflict_of,payload)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		ev.ID, ev.ProjectID, ev.Seq, ev.Type, ev.OccurredAt, ev.Reviewer, ev.Undoes, ev.ConflictOf, string(ev.Payload))
	return err
}

// NextSeq returns the next event sequence for a project.
func (s *Store) NextSeq(ctx context.Context, projectID string) (int64, error) {
	var maxSeq sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&maxSeq); err != nil {
		return 0, err
	}
	if !maxSeq.Valid {
		return 1, nil
	}
	return maxSeq.Int64 + 1, nil
}
