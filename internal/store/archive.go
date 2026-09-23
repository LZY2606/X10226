package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
)

// Archive is the portable export format. Identity (ids), order (seq) and
// provenance (parent links, source refs) are preserved verbatim so a
// delete-and-reimport cycle reproduces the exact same review state.
type Archive struct {
	Format      string           `json:"format"`
	Version     int              `json:"version"`
	ExportedSeq int64            `json:"exported_seq"`
	Sources     []archiveSource  `json:"sources"`
	Derived     []archiveDerived `json:"derived"`
	Events      []archiveEvent   `json:"events"`
	Counters    map[string]int64 `json:"counters"`
}

type archiveSource struct {
	ID          string          `json:"id"`
	Kind        string          `json:"kind"`
	Version     int             `json:"version"`
	ParentID    string          `json:"parent_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
	Diagnostics json.RawMessage `json:"diagnostics"`
	CreatedSeq  int64           `json:"created_seq"`
}

type archiveDerived struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	AlgoVersion      string          `json:"algo_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	SourceIDs        json.RawMessage `json:"source_ids"`
	Payload          json.RawMessage `json:"payload"`
	CreatedSeq       int64           `json:"created_seq"`
	SupersededSeq    *int64          `json:"superseded_seq,omitempty"`
}

type archiveEvent struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	At      string          `json:"at"`
}

const ArchiveFormat = "tgareview-archive"

// Export writes the complete archive.
func (s *Store) Export(w io.Writer) error {
	a := Archive{Format: ArchiveFormat, Version: 1, Counters: map[string]int64{}}
	a.ExportedSeq = s.MaxSeq()

	rows, err := s.db.Query(`SELECT id,kind,version,COALESCE(parent_id,''),payload,diagnostics,created_seq FROM sources ORDER BY created_seq, id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var as archiveSource
		var parent string
		if err := rows.Scan(&as.ID, &as.Kind, &as.Version, &parent, &as.Payload, &as.Diagnostics, &as.CreatedSeq); err != nil {
			rows.Close()
			return err
		}
		as.ParentID = parent
		a.Sources = append(a.Sources, as)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT id,kind,algo_version,param_fingerprint,source_ids,payload,created_seq,superseded_seq FROM derived ORDER BY created_seq, id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ad archiveDerived
		var sup sql.NullInt64
		if err := rows.Scan(&ad.ID, &ad.Kind, &ad.AlgoVersion, &ad.ParamFingerprint, &ad.SourceIDs, &ad.Payload, &ad.CreatedSeq, &sup); err != nil {
			rows.Close()
			return err
		}
		if sup.Valid {
			v := sup.Int64
			ad.SupersededSeq = &v
		}
		a.Derived = append(a.Derived, ad)
	}
	rows.Close()

	rows, err = s.db.Query(`SELECT seq,id,type,payload,at FROM events ORDER BY seq`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var ae archiveEvent
		if err := rows.Scan(&ae.Seq, &ae.ID, &ae.Type, &ae.Payload, &ae.At); err != nil {
			rows.Close()
			return err
		}
		a.Events = append(a.Events, ae)
	}
	rows.Close()

	crows, err := s.db.Query(`SELECT name,value FROM counters`)
	if err != nil {
		return err
	}
	for crows.Next() {
		var name string
		var v int64
		if err := crows.Scan(&name, &v); err != nil {
			crows.Close()
			return err
		}
		a.Counters[name] = v
	}
	crows.Close()

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(a)
}

// Import loads an archive, preserving ids, sequences and provenance.
// Existing rows with the same id are skipped so re-import is idempotent.
func (s *Store) Import(r io.Reader) (*Archive, error) {
	var a Archive
	if err := json.NewDecoder(r).Decode(&a); err != nil {
		return nil, fmt.Errorf("archive decode: %w", err)
	}
	if a.Format != ArchiveFormat {
		return nil, fmt.Errorf("unsupported archive format %q", a.Format)
	}
	err := s.withTx(func(tx *sql.Tx) error {
		for _, src := range a.Sources {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO sources(id,kind,version,parent_id,payload,diagnostics,created_seq) VALUES(?,?,?,?,?,?,?)`,
				src.ID, src.Kind, src.Version, nullStr(src.ParentID), string(src.Payload), string(src.Diagnostics), src.CreatedSeq); err != nil {
				return err
			}
		}
		for _, d := range a.Derived {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO derived(id,kind,algo_version,param_fingerprint,source_ids,payload,created_seq,superseded_seq) VALUES(?,?,?,?,?,?,?,?)`,
				d.ID, d.Kind, d.AlgoVersion, d.ParamFingerprint, string(d.SourceIDs), string(d.Payload), d.CreatedSeq, d.SupersededSeq); err != nil {
				return err
			}
		}
		for _, e := range a.Events {
			if _, err := tx.Exec(`INSERT OR IGNORE INTO events(seq,id,type,payload,at) VALUES(?,?,?,?,?)`,
				e.Seq, e.ID, e.Type, string(e.Payload), e.At); err != nil {
				return err
			}
		}
		for name, v := range a.Counters {
			if _, err := tx.Exec(`INSERT INTO counters(name,value) VALUES(?,?)
				ON CONFLICT(name) DO UPDATE SET value=MAX(value, excluded.value)`, name, v); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.AppendEvent("archive.imported", map[string]any{
		"exported_seq": a.ExportedSeq, "sources": len(a.Sources),
		"derived": len(a.Derived), "events": len(a.Events),
	}); err != nil {
		return nil, err
	}
	return &a, nil
}
