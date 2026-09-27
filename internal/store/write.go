package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tga/internal/domain"
)

// WithTx runs fn inside a transaction.
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateProjectTx(tx *sql.Tx, p *domain.Project) error {
	_, err := tx.Exec(
		`INSERT INTO projects(id,name,note,seed,created_at) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.Note, p.Seed, p.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) NextSourceSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0) FROM sources WHERE project_id=?`, projectID).Scan(&max)
	return max.Int64 + 1, err
}

func (s *Store) InsertSourceTx(tx *sql.Tx, src *domain.Source) error {
	_, err := tx.Exec(
		`INSERT INTO sources(id,project_id,kind,version,parent_source_id,label,raw_name,raw_text,raw_encoding,checksum,status,rejected_rows,accepted_rows,seq,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, src.Kind, src.Version, src.ParentSourceID, src.Label,
		src.RawName, src.RawText, src.RawEncoding, src.Checksum, src.Status,
		src.RejectedRows, src.AcceptedRows, src.Seq,
		src.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) InsertRowTx(tx *sql.Tx, r *domain.SourceRow) error {
	fields := r.Fields
	if fields == "" {
		fields = "{}"
	}
	_, err := tx.Exec(
		`INSERT INTO source_rows(source_id,row_index,raw_line,fields_json,parse_ok,diagnostic)
		 VALUES(?,?,?,?,?,?)`,
		r.SourceID, r.RowIndex, r.RawLine, fields, r.ParseOK, r.Diagnostic)
	return err
}

func (s *Store) InsertDiagnosticTx(tx *sql.Tx, d *domain.Diagnostic) error {
	if d.RowIndex == 0 {
		d.RowIndex = -1
	}
	_, err := tx.Exec(
		`INSERT INTO diagnostics(project_id,source_id,run_id,row_index,severity,code,message,raw_ref)
		 VALUES(?,?,?,?,?,?,?,?)`,
		d.ProjectID, d.SourceID, d.RunID, d.RowIndex, d.Severity, d.Code, d.Message, d.RawRef)
	return err
}

func (s *Store) AppendEventTx(tx *sql.Tx, e *domain.Event) (int64, error) {
	payload := e.Payload
	if payload == nil {
		payload = map[string]string{}
	}
	pb, _ := json.Marshal(payload)
	res, err := tx.Exec(
		`INSERT INTO events(project_id,type,at,actor,subject_id,payload_json)
		 VALUES(?,?,?,?,?,?)`,
		e.ProjectID, e.Type, e.At.UTC().Format(time.RFC3339Nano), e.Actor, e.SubjectID, string(pb))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) InsertParamsTx(tx *sql.Tx, p *domain.Parameters) error {
	_, err := tx.Exec(
		`INSERT INTO parameters(id,project_id,version,parent_id,fingerprint,algorithm_version,
		   atmos_delay_s,thermal_lag_s,dtg_peak_prominence,dtg_peak_min_sep_s,smooth_window_s,
		   allow_overlap,gap_max_interp_s,note,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.ID, p.ProjectID, p.Version, p.ParentID, p.Fingerprint, p.AlgorithmVer,
		p.AtmosDelayS, p.ThermalLagS, p.PeakProminence, p.PeakMinSepS, p.SmoothWindowS,
		p.MergeOverlap, p.GapMaxS, p.Note, p.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) MarkRunCurrentTx(tx *sql.Tx, projectID, newRunID string) error {
	if _, err := tx.Exec(
		`UPDATE runs SET current=0, superseded_by=? WHERE project_id=? AND current=1`,
		newRunID, projectID); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE runs SET current=1 WHERE id=?`, newRunID)
	return err
}

func (s *Store) InsertRunTx(tx *sql.Tx, r *domain.Run) error {
	_, err := tx.Exec(
		`INSERT INTO runs(id,project_id,seq,algorithm_version,param_id,param_fingerprint,source_set,current,superseded_by,note,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.Seq, r.AlgorithmVer, r.ParamID, r.ParamFP, r.SourceSet,
		false, r.SupersededBy, r.Note, r.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	for _, ref := range r.Sources {
		ri, _ := json.Marshal(ref.RowIndexes)
		if _, err := tx.Exec(
			`INSERT INTO run_sources(run_id,project_id,kind,source_id,version,row_indexes_json)
			 VALUES(?,?,?,?,?,?)`,
			r.ID, r.ProjectID, ref.Kind, ref.SourceID, ref.Version, string(ri)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) InsertStageTx(tx *sql.Tx, c *domain.StageCandidate) error {
	ov, _ := json.Marshal(c.OverlapsWith)
	sr, _ := json.Marshal(c.Sources)
	_, err := tx.Exec(
		`INSERT INTO stage_candidates(id,run_id,idx,start_s,end_s,peak_s,peak_dtg,mass_loss_pct,
		   mass_loss_err_pct,overlaps_json,forced_sum_100,state,rejected_reason,unknown,unknown_reason,
		   atmos_reaction_s,valve_s,sources_json)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.RunID, c.Index, c.StartS, c.EndS, c.PeakS, c.PeakDTG, c.MassLossPct,
		c.MassLossErrPct, string(ov), c.ForcedSum, c.State, c.RejectedReason, c.Unknown, c.UnknownReason,
		c.AtmosReactionS, c.ValveS, string(sr))
	return err
}

func (s *Store) InsertResidueTx(tx *sql.Tx, r *domain.ResidueResult) error {
	ch, err := json.Marshal(r.Chain)
	if err != nil {
		return err
	}
	sr, err := json.Marshal(r.Sources)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		`INSERT INTO residues(id,run_id,at_s,residue_pct,err_pct,unknown,unknown_reason,chain_json,sources_json)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.RunID, r.AtS, r.Percent, r.ErrPct, r.Unknown, r.UnknownReason, string(ch), string(sr))
	return err
}

func (s *Store) InsertRuleTx(tx *sql.Tx, r *domain.RuleEvidence) error {
	data := r.Data
	if data == "" {
		data = "{}"
	}
	_, err := tx.Exec(
		`INSERT INTO rule_evidence(project_id,code,statement,subject_id,run_id,data_json,created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		r.ProjectID, r.Code, r.Statement, r.SubjectID, r.RunID, data,
		r.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) NextDecisionSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0) FROM decisions WHERE project_id=?`, projectID).Scan(&max)
	return max.Int64 + 1, err
}

func (s *Store) InsertDecisionTx(tx *sql.Tx, d *domain.Decision) error {
	_, err := tx.Exec(
		`INSERT INTO decisions(id,project_id,run_id,target_kind,target_id,verdict,note,param_id,revoked,revokes_id,conflict_of,seq,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.RunID, d.TargetKind, d.TargetID, d.Verdict, d.Note, d.ParamID,
		d.Revoked, d.RevokesID, d.ConflictOf, d.Seq, d.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) RevokeDecisionTx(tx *sql.Tx, id string) error {
	res, err := tx.Exec(`UPDATE decisions SET revoked=1 WHERE id=? AND revoked=0`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return fmt.Errorf("decision %s already revoked or missing", id)
	}
	return nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	return s.WithTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(
			`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
			key, value)
		return err
	})
}
