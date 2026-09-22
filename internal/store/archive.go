package store

import (
	"bytes"
	"encoding/json"
	"fmt"

	"tga-archive/internal/domain"
)

// ArchiveFormat is the offline exchange format. Events alone reconstruct
// every derived identity, ordering and provenance relation.
const ArchiveFormat = "tga-archive/v1"

type Archive struct {
	Format  string        `json:"format"`
	Project ProjectInfo   `json:"project"`
	Events  []EventRecord `json:"events"`
}

type EventRecord struct {
	Type    string          `json:"type"`
	At      string          `json:"at"`
	Parent  string          `json:"parent,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

// Export serializes one project.
func (s *Store) Export(projectID string) (Archive, error) {
	info, err := s.Project(projectID)
	if err != nil {
		return Archive{}, err
	}
	evs, err := s.Events(projectID)
	if err != nil {
		return Archive{}, err
	}
	a := Archive{Format: ArchiveFormat, Project: info}
	for _, ev := range evs {
		a.Events = append(a.Events, EventRecord{Type: ev.Type, At: ev.At, Parent: ev.Parent, Payload: ev.Payload})
	}
	return a, nil
}

// ImportVerification reports identity checks performed on import.
type ImportVerification struct {
	ProjectID       string   `json:"projectId"`
	Events          int      `json:"events"`
	RecomputedIDs   []string `json:"recomputedIds"`
	EventIDMismatch []string `json:"eventIdMismatch,omitempty"`
	Imported        bool     `json:"imported"`
}

// Import loads an archive. Event content IDs are recomputed and checked;
// seq is reassigned by AUTOINCREMENT. The first event must be project_created.
func (s *Store) Import(a Archive) (ImportVerification, error) {
	v := ImportVerification{}
	if a.Format != ArchiveFormat {
		return v, fmt.Errorf("unsupported archive format %q", a.Format)
	}
	if len(a.Events) == 0 || a.Events[0].Type != domain.EvProjectCreated {
		return v, fmt.Errorf("archive must start with a project_created event")
	}
	pid := a.Project.ID
	if pid == "" {
		pid = domain.HashID("prj", a.Project.Name, a.Events[0].At)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return v, err
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT OR IGNORE INTO projects(id,name,case_id,seed,created_at,note) VALUES(?,?,?,?,?,?)`,
		pid, a.Project.Name, a.Project.CaseID, a.Project.Seed, a.Project.CreatedAt, a.Project.Note)
	if err != nil {
		return v, err
	}

	var inserted int
	for _, rec := range a.Events {
		var payload any
		if err := json.Unmarshal(rec.Payload, &payload); err != nil {
			return v, fmt.Errorf("invalid payload for %s event: %w", rec.Type, err)
		}
		want := domain.EventID(pid, rec.Type, rec.At, rec.Parent, payload)
		var exists int
		err := tx.QueryRow(`SELECT COUNT(*) FROM events WHERE project=? AND id=?`, pid, want).Scan(&exists)
		if err != nil {
			return v, err
		}
		v.RecomputedIDs = append(v.RecomputedIDs, want)
		if exists == 0 {
			_, err = tx.Exec(`INSERT INTO events(id,project,type,at,parent,payload) VALUES(?,?,?,?,?,?)`,
				want, pid, rec.Type, rec.At, rec.Parent, string(rec.Payload))
			if err != nil {
				return v, err
			}
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return v, err
	}
	v.ProjectID = pid
	v.Events = inserted
	v.Imported = true
	return v, nil
}

// Marshal renders the archive as indented JSON.
func (a Archive) Marshal() ([]byte, error) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(a); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ParseArchive decodes without trusting any field.
func ParseArchive(data []byte) (Archive, error) {
	var a Archive
	if err := json.Unmarshal(data, &a); err != nil {
		return a, err
	}
	return a, nil
}
