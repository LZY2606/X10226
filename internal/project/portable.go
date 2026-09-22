package project

import (
	"context"
	"database/sql"
	"encoding/json"
)

// ExportBundle is the self-contained, portable representation. Raw layer
// revisions and the event log are authoritative; cached artifacts are embedded
// so even historical derived views survive delete-and-reimport with stable ids.
type ExportBundle struct {
	FormatVersion string     `json:"format_version"`
	Project       ExpProject `json:"project"`
	Revisions     []ExpRev   `json:"revisions"`
	Events        []ExpEvent `json:"events"`
	Artifacts     []ExpArt   `json:"artifacts"`
	Fragments     []ExpFrag  `json:"fragments"`
	Decisions     []ExpDec   `json:"decisions"`
}

type ExpProject struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}
type ExpRev struct {
	RevID        string `json:"rev_id"`
	Kind         string `json:"kind"`
	ParentRev    string `json:"parent_rev"`
	Seq          int64  `json:"seq"`
	SourceFile   string `json:"source_file"`
	RawCanonical string `json:"raw_canonical"`
	Status       string `json:"status"`
}
type ExpEvent struct {
	Seq     int64  `json:"seq"`
	Type    string `json:"type"`
	At      string `json:"at"`
	Payload string `json:"payload"`
}
type ExpArt struct {
	ArtifactID string `json:"artifact_id"`
	BasisSeq   int64  `json:"basis_seq"`
	AlgVersion string `json:"alg_version"`
	ParamsFP   string `json:"params_fp"`
	Content    string `json:"content"`
	Current    int    `json:"current"`
}
type ExpFrag struct {
	Seq      int64  `json:"seq"`
	Kind     string `json:"kind"`
	Row      *int   `json:"row,omitempty"`
	Field    string `json:"field"`
	Message  string `json:"message"`
	RawValue string `json:"raw_value"`
}
type ExpDec struct {
	ID         string `json:"id"`
	Seq        int64  `json:"seq"`
	ArtifactID string `json:"artifact_id"`
	TargetID   string `json:"target_id"`
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Rationale  string `json:"rationale"`
	Supersedes string `json:"supersedes"`
	Undone     int    `json:"undone"`
}

const ExportFormat = "tga.export/v1"

// Export reads the whole project into a portable bundle.
func (s *Service) Export(ctx context.Context, projectID string) (*ExportBundle, error) {
	out := &ExportBundle{FormatVersion: ExportFormat}
	if err := s.st.DB.QueryRowContext(ctx,
		`SELECT id,name,description,created_at FROM projects WHERE id=?`, projectID).
		Scan(&out.Project.ID, &out.Project.Name, &out.Project.Description, &out.Project.CreatedAt); err != nil {
		return nil, err
	}

	revRows, err := s.st.DB.QueryContext(ctx,
		`SELECT rev_id,kind,parent_rev,seq,source_file,raw_canonical,status
		 FROM layer_revisions WHERE project_id=? ORDER BY seq ASC, kind ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer revRows.Close()
	for revRows.Next() {
		var r ExpRev
		if err := revRows.Scan(&r.RevID, &r.Kind, &r.ParentRev, &r.Seq, &r.SourceFile, &r.RawCanonical, &r.Status); err != nil {
			return nil, err
		}
		out.Revisions = append(out.Revisions, r)
	}

	evRows, err := s.st.DB.QueryContext(ctx,
		`SELECT seq,type,at,payload FROM events WHERE project_id=? ORDER BY seq ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer evRows.Close()
	for evRows.Next() {
		var e ExpEvent
		if err := evRows.Scan(&e.Seq, &e.Type, &e.At, &e.Payload); err != nil {
			return nil, err
		}
		out.Events = append(out.Events, e)
	}

	arRows, err := s.st.DB.QueryContext(ctx,
		`SELECT artifact_id,basis_seq,alg_version,params_fp,content,current FROM artifacts WHERE project_id=? ORDER BY basis_seq ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer arRows.Close()
	for arRows.Next() {
		var a ExpArt
		if err := arRows.Scan(&a.ArtifactID, &a.BasisSeq, &a.AlgVersion, &a.ParamsFP, &a.Content, &a.Current); err != nil {
			return nil, err
		}
		out.Artifacts = append(out.Artifacts, a)
	}

	frRows, err := s.st.DB.QueryContext(ctx,
		`SELECT seq,kind,row_idx,field,message,raw_value FROM fragments WHERE project_id=? ORDER BY id ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer frRows.Close()
	for frRows.Next() {
		var f ExpFrag
		var row sql.NullInt64
		if err := frRows.Scan(&f.Seq, &f.Kind, &row, &f.Field, &f.Message, &f.RawValue); err != nil {
			return nil, err
		}
		if row.Valid {
			v := int(row.Int64)
			f.Row = &v
		}
		out.Fragments = append(out.Fragments, f)
	}

	dcRows, err := s.st.DB.QueryContext(ctx,
		`SELECT id,seq,artifact_id,target_id,kind,value,rationale,supersedes,undone FROM decisions WHERE project_id=? ORDER BY seq ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer dcRows.Close()
	for dcRows.Next() {
		var d ExpDec
		if err := dcRows.Scan(&d.ID, &d.Seq, &d.ArtifactID, &d.TargetID, &d.Kind, &d.Value, &d.Rationale, &d.Supersedes, &d.Undone); err != nil {
			return nil, err
		}
		out.Decisions = append(out.Decisions, d)
	}
	return out, nil
}

// Restore writes an exported bundle back verbatim, preserving ids and seq
// ordering. Returns ErrExists if the project id already exists.
func (s *Service) Restore(ctx context.Context, b *ExportBundle) error {
	var existing string
	err := s.st.DB.QueryRowContext(ctx, `SELECT id FROM projects WHERE id=?`, b.Project.ID).Scan(&existing)
	if err == nil {
		return ErrExists
	}
	tx, err := s.st.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,description,created_at) VALUES(?,?,?,?)`,
		b.Project.ID, b.Project.Name, b.Project.Description, b.Project.CreatedAt); err != nil {
		return err
	}
	for _, r := range b.Revisions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO layer_revisions(project_id,rev_id,kind,parent_rev,seq,source_file,raw_canonical,status)
			 VALUES(?,?,?,?,?,?,?,?)`,
			b.Project.ID, r.RevID, r.Kind, r.ParentRev, r.Seq, r.SourceFile, r.RawCanonical, r.Status); err != nil {
			return err
		}
	}
	for _, e := range b.Events {
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(project_id,seq,type,at,payload) VALUES(?,?,?,?,?)`,
			b.Project.ID, e.Seq, e.Type, e.At, e.Payload); err != nil {
			return err
		}
	}
	for _, a := range b.Artifacts {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO artifacts(project_id,artifact_id,basis_seq,alg_version,params_fp,content,current)
			 VALUES(?,?,?,?,?,?,?)`,
			b.Project.ID, a.ArtifactID, a.BasisSeq, a.AlgVersion, a.ParamsFP, a.Content, a.Current); err != nil {
			return err
		}
	}
	for _, f := range b.Fragments {
		var row sql.NullInt64
		if f.Row != nil {
			row = sql.NullInt64{Int64: int64(*f.Row), Valid: true}
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO fragments(project_id,seq,kind,row_idx,field,message,raw_value) VALUES(?,?,?,?,?,?,?)`,
			b.Project.ID, f.Seq, f.Kind, row, f.Field, f.Message, f.RawValue); err != nil {
			return err
		}
	}
	for _, d := range b.Decisions {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO decisions(project_id,id,seq,artifact_id,target_id,kind,value,rationale,supersedes,undone)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			b.Project.ID, d.ID, d.Seq, d.ArtifactID, d.TargetID, d.Kind, d.Value, d.Rationale, d.Supersedes, d.Undone); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ParseExport decodes an export bundle.
func ParseExport(raw []byte) (*ExportBundle, error) {
	var b ExportBundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	if b.FormatVersion != ExportFormat {
		return nil, &jsonSyntaxError{msg: "unsupported export format: " + b.FormatVersion}
	}
	return &b, nil
}

type jsonSyntaxError struct{ msg string }

func (e *jsonSyntaxError) Error() string { return e.msg }
