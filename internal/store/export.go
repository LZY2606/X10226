package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// ArchiveVersion is the export format version.
const ArchiveVersion = "tga-review-archive/v1"

// Archive is the portable, identity-preserving project representation.
// The event log is the source of truth; raw_docs are included only as an
// integrity cross-check and are rebuilt on import.
type Archive struct {
	Format   string                 `json:"format"`
	Project  ArchiveProject         `json:"project"`
	Events   []*StoredEvent         `json:"events"`
	RawDocs  []ArchiveDoc           `json:"raw_docs"`
	FP       string                 `json:"archive_fp"`
}

type ArchiveProject struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	SourceFP string `json:"source_fp"`
}

type ArchiveDoc struct {
	ID        string                 `json:"id"`
	Kind      string                 `json:"kind"`
	Layer     string                 `json:"layer"`
	Version   int                    `json:"version"`
	ParentDoc string                 `json:"parent_doc"`
	Origin    string                 `json:"origin"`
	Committed int64                  `json:"committed_seq"`
	RawText   string                 `json:"raw_text,omitempty"`
	Content   map[string]interface{} `json:"content"`
}

// ExportProject serializes a project. All stable IDs and the event order are
// preserved byte-for-byte.
func (s *Store) ExportProject(project string) (*Archive, error) {
	events, err := s.Events(project)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, ErrNotFound
	}
	var name, sourceFP string
	row := s.db.QueryRow(`SELECT name,source_fp FROM projects WHERE id=?`, project)
	if err := row.Scan(&name, &sourceFP); err != nil {
		return nil, err
	}
	docs, err := s.exportDocs(project)
	if err != nil {
		return nil, err
	}
	a := &Archive{
		Format: ArchiveVersion,
		Project: ArchiveProject{ID: project, Name: name, SourceFP: sourceFP},
		Events: events,
		RawDocs: docs,
	}
	a.FP = archiveFingerprint(a)
	return a, nil
}

func (s *Store) exportDocs(project string) ([]ArchiveDoc, error) {
	rows, err := s.db.Query(`SELECT id,kind,layer,version,parent_doc,origin,committed,raw_text,content
		FROM raw_docs WHERE project=? ORDER BY committed,id`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArchiveDoc
	for rows.Next() {
		var d ArchiveDoc
		var content string
		if err := rows.Scan(&d.ID, &d.Kind, &d.Layer, &d.Version, &d.ParentDoc,
			&d.Origin, &d.Committed, &d.RawText, &content); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(content), &d.Content); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ImportResult reports what an import did.
type ImportResult struct {
	ProjectID string `json:"project_id"`
	Status    string `json:"status"` // imported | idempotent
	Events    int    `json:"events"`
}

// ImportArchive loads an archive. With preserveIDs=true (the normal export ->
// delete DB -> import round trip) every id, order and source relation is
// kept; hash chain verification runs before any write.
func (s *Store) ImportArchive(a *Archive, preserveIDs bool) (*ImportResult, error) {
	if err := verifyArchive(a); err != nil {
		return nil, err
	}
	pid := a.Project.ID
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, pid).Scan(&existing); err != nil {
		return nil, err
	}
	if existing > 0 {
		var count int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE project=?`, pid).Scan(&count); err != nil {
			return nil, err
		}
		if count == len(a.Events) {
			return &ImportResult{ProjectID: pid, Status: "idempotent", Events: 0}, nil
		}
		return nil, fmt.Errorf("project %s already exists with different history", pid)
	}
	if _, err := tx.Exec(`INSERT INTO projects(id,name,created_at,last_seq,last_hash,source_fp)
		VALUES(?,?,?,?,?,?)`, pid, a.Project.Name, a.Events[0].OccurredAt, 0, "", a.Project.SourceFP); err != nil {
		return nil, err
	}
	n := 0
	for _, ev := range a.Events {
		pj, err := json.Marshal(ev.Payload)
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`INSERT INTO events(seq,project,kind,occurred_at,client_id,payload,prev_hash,hash)
			VALUES(?,?,?,?,?,?,?,?)`, ev.Seq, pid, ev.Kind, ev.OccurredAt, ev.ClientID,
			string(pj), ev.PrevHash, ev.Hash); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(`UPDATE projects SET last_seq=?,last_hash=?,created_seq=CASE WHEN kind='project_created' THEN seq ELSE created_seq END WHERE id=?`,
			ev.Seq, ev.Hash, pid); err != nil {
			return nil, err
		}
		n++
		projEv := &StoredEvent{Seq: ev.Seq, Project: pid, Kind: ev.Kind, Payload: ev.Payload}
		if err := projectEvent(tx, projEv); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	_ = sql.ErrNoRows
	return &ImportResult{ProjectID: pid, Status: "imported", Events: n}, nil
}

func archiveFingerprint(a *Archive) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%s|%d", a.Format, a.Project.ID, a.Project.SourceFP, len(a.Events))
	for _, ev := range a.Events {
		fmt.Fprintf(h, "|%d:%s:%s", ev.Seq, ev.Kind, ev.Hash)
	}
	sum := h.Sum(nil)
	return hex.EncodeToString(sum[:])
}

func verifyArchive(a *Archive) error {
	if a.Format != ArchiveVersion {
		return fmt.Errorf("unsupported archive format %q", a.Format)
	}
	if len(a.Events) == 0 || a.Events[0].Kind != "project_created" {
		return fmt.Errorf("archive must start with project_created")
	}
	if a.FP != archiveFingerprint(a) {
		return fmt.Errorf("archive fingerprint mismatch")
	}
	prev := ""
	for i, ev := range a.Events {
		if ev.Seq != int64(i+1) {
			return fmt.Errorf("event seq gap at index %d", i)
		}
		if ev.PrevHash != prev {
			return fmt.Errorf("hash chain broken at seq %d", ev.Seq)
		}
		pj, err := canonicalJSON(ev.Payload)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s\n%s\n%d\n%s\n%s\n%s",
			ev.Seq, ev.Project, ev.Kind, ev.OccurredAt, ev.ClientID, string(pj), prev)))
		if hex.EncodeToString(sum[:]) != ev.Hash {
			return fmt.Errorf("event hash mismatch at seq %d", ev.Seq)
		}
		prev = ev.Hash
	}
	return nil
}

// DecodeArchive parses an archive payload.
func DecodeArchive(data []byte) (*Archive, error) {
	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("invalid archive json: %w", err)
	}
	return &a, nil
}
