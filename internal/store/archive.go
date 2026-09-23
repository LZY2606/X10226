package store

import (
	"encoding/json"
)

// Archive is the portable project package. It carries every table verbatim so
// identity, ordering and provenance survive delete-database + re-import.
type Archive struct {
	Format     string        `json:"format"`
	Version    int           `json:"version"`
	Project    ProjectRow    `json:"project"`
	Events     []EventRow    `json:"events"`
	Documents  []DocRow      `json:"documents"`
	Batches    []BatchRow    `json:"batches"`
	Derived    []DerivedRows `json:"derived"`
	Decisions  []DecisionRow `json:"decisions"`
	KV         []KVRow       `json:"kv"`
}

const ArchiveFormat = "gotga-tga-archive"
const ArchiveVersion = 1

type ProjectRow struct {
	ID, Name, CreatedTS, Meta string
}
type EventRow struct {
	Seq       int64
	ID, Kind, Payload, Revokes, Actor, Note, TS string
}
type DocRow struct {
	ID, Kind, Label, Source, ParentID, RawText, Payload, Diags, IngestTS string
	DocSeq, BornSeq int64
}
type DecisionRow struct {
	ID string
	EventSeq int64
	SubjectType, SubjectID, Verdict, Reason string
	Revoked int
	ConflictWith, ResolvedBy string
	Winner int
	Adjustment *string
	TS string
}
type KVRow struct{ Key, Value string }

func (s *Store) ExportProject(projectID string) (*Archive, error) {
	ar := &Archive{Format: ArchiveFormat, Version: ArchiveVersion}
	if err := s.DB.QueryRow(`SELECT id,name,created_ts,meta FROM projects WHERE id=?`, projectID).
		Scan(&ar.Project.ID, &ar.Project.Name, &ar.Project.CreatedTS, &ar.Project.Meta); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(`SELECT seq,id,kind,payload,COALESCE(revokes,''),COALESCE(actor,''),COALESCE(note,''),ts FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r EventRow
		if err := rows.Scan(&r.Seq, &r.ID, &r.Kind, &r.Payload, &r.Revokes, &r.Actor, &r.Note, &r.TS); err != nil {
			rows.Close()
			return nil, err
		}
		ar.Events = append(ar.Events, r)
	}
	rows.Close()

	drows, err := s.DB.Query(`SELECT id,kind,COALESCE(label,''),COALESCE(source,''),doc_seq,COALESCE(parent_id,''),born_seq,raw_text,payload,diags,ingest_ts FROM documents WHERE project_id=? ORDER BY kind,doc_seq`, projectID)
	if err != nil {
		return nil, err
	}
	for drows.Next() {
		var r DocRow
		if err := drows.Scan(&r.ID, &r.Kind, &r.Label, &r.Source, &r.DocSeq, &r.ParentID, &r.BornSeq, &r.RawText, &r.Payload, &r.Diags, &r.IngestTS); err != nil {
			drows.Close()
			return nil, err
		}
		ar.Documents = append(ar.Documents, r)
	}
	drows.Close()

	ar.Batches, _ = s.Batches(projectID)
	ar.Derived, err = s.DerivedList(projectID)
	if err != nil {
		return nil, err
	}
	crows, err := s.DB.Query(`SELECT id,event_seq,subject_type,subject_id,verdict,COALESCE(reason,''),revoked,COALESCE(conflict_with,'[]'),COALESCE(resolved_by,''),winner,adjustment,ts FROM decisions WHERE project_id=? ORDER BY event_seq`, projectID)
	if err != nil {
		return nil, err
	}
	for crows.Next() {
		var r DecisionRow
		if err := crows.Scan(&r.ID, &r.EventSeq, &r.SubjectType, &r.SubjectID, &r.Verdict, &r.Reason, &r.Revoked, &r.ConflictWith, &r.ResolvedBy, &r.Winner, &r.Adjustment, &r.TS); err != nil {
			crows.Close()
			return nil, err
		}
		ar.Decisions = append(ar.Decisions, r)
	}
	crows.Close()
	krows, err := s.DB.Query(`SELECT key,value FROM kv WHERE project_id=?`, projectID)
	if err != nil {
		return nil, err
	}
	for krows.Next() {
		var r KVRow
		if err := krows.Scan(&r.Key, &r.Value); err != nil {
			krows.Close()
			return nil, err
		}
		ar.KV = append(ar.KV, r)
	}
	krows.Close()
	return ar, nil
}

// ImportProject restores an archive into an empty database, preserving seq and
// all content-addressed ids.
func (s *Store) ImportProject(ar *Archive) error {
	if ar.Format != ArchiveFormat {
		return &ArchiveError{Msg: "unsupported archive format: " + ar.Format}
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cleanup := []string{"kv", "decisions", "derived", "batches", "documents", "events", "projects"}
	for _, t := range cleanup {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO projects(id,name,created_ts,meta) VALUES(?,?,?,?)`,
		ar.Project.ID, ar.Project.Name, ar.Project.CreatedTS, ar.Project.Meta); err != nil {
		return err
	}
	for _, r := range ar.Events {
		if _, err := tx.Exec(`INSERT INTO events(seq,id,project_id,kind,payload,revokes,actor,note,ts) VALUES(?,?,?,?,?,?,?,?,?)`,
			r.Seq, r.ID, ar.Project.ID, r.Kind, r.Payload, r.Revokes, r.Actor, r.Note, r.TS); err != nil {
			return err
		}
	}
	for _, r := range ar.Documents {
		if _, err := tx.Exec(`INSERT INTO documents(id,project_id,kind,label,source,doc_seq,parent_id,born_seq,raw_text,payload,diags,ingest_ts) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, ar.Project.ID, r.Kind, r.Label, r.Source, r.DocSeq, r.ParentID, r.BornSeq, r.RawText, r.Payload, r.Diags, r.IngestTS); err != nil {
			return err
		}
	}
	for _, r := range ar.Batches {
		if _, err := tx.Exec(`INSERT INTO batches(id,project_id,event_seq,partial,accepted,rejected,raw_text,ts) VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, ar.Project.ID, r.EventSeq, b2i(r.Partial), r.Accepted, r.Rejected, r.RawText, r.TS); err != nil {
			return err
		}
	}
	for _, r := range ar.Derived {
		stale := any(nil)
		reason := any(nil)
		if r.StaleSeq.Valid {
			stale = r.StaleSeq.Int64
			if r.StaleReason.Valid {
				reason = r.StaleReason.String
			}
		}
		if _, err := tx.Exec(`INSERT INTO derived(id,project_id,kind,json,born_seq,stale_seq,stale_reason) VALUES(?,?,?,?,?,?,?)`,
			r.ID, ar.Project.ID, r.Kind, r.JSON, r.BornSeq, stale, reason); err != nil {
			return err
		}
	}
	for _, r := range ar.Decisions {
		var adj any
		if r.Adjustment != nil {
			adj = *r.Adjustment
		}
		if _, err := tx.Exec(`INSERT INTO decisions(id,project_id,event_seq,subject_type,subject_id,verdict,reason,revoked,conflict_with,resolved_by,winner,adjustment,ts) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, ar.Project.ID, r.EventSeq, r.SubjectType, r.SubjectID, r.Verdict, r.Reason, r.Revoked, r.ConflictWith, r.ResolvedBy, r.Winner, adj, r.TS); err != nil {
			return err
		}
	}
	for _, r := range ar.KV {
		if _, err := tx.Exec(`INSERT INTO kv(project_id,key,value) VALUES(?,?,?)`, ar.Project.ID, r.Key, r.Value); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := s.DB.Exec(`DELETE FROM sqlite_sequence WHERE name='events'`); err != nil {
		// table absent in non-autoincrement edge cases; safe to ignore once
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

type ArchiveError struct{ Msg string }

func (e *ArchiveError) Error() string { return e.Msg }

func MarshalArchive(ar *Archive) ([]byte, error)  { return json.MarshalIndent(ar, "", "  ") }
func UnmarshalArchive(b []byte) (*Archive, error) {
	var ar Archive
	if err := json.Unmarshal(b, &ar); err != nil {
		return nil, err
	}
	return &ar, nil
}
