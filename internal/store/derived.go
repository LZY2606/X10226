package store

import (
	"database/sql"

	"gotga/internal/domain"
)

// SaveDerivedSnapshot replaces the "current" surface of a recomputation:
// previous rows of the recomputed kinds are marked stale, then every object in
// the new bundle is upserted back to non-stale. Content-addressed objects that
// did not change therefore keep their identity AND their original born_seq
// (UPSERT only refreshes json), while objects that disappeared stay readable as
// stale with the given reason.
func (s *Store) SaveDerivedSnapshot(projectID string, b *domain.ResultBundle, kinds []string, reason string) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, k := range kinds {
		if _, err := tx.Exec(`UPDATE derived SET stale_seq=?, stale_reason=? WHERE project_id=? AND kind=? AND stale_seq IS NULL`,
			b.EventSeq, reason, projectID, k); err != nil {
			return err
		}
	}
	upsert := func(id, kind, j string) error {
		_, err := tx.Exec(`INSERT INTO derived(id,project_id,kind,json,born_seq) VALUES(?,?,?,?,?)
			ON CONFLICT(project_id,id) DO UPDATE SET kind=excluded.kind, json=excluded.json, stale_seq=NULL, stale_reason=NULL, born_seq=derived.born_seq`,
			id, projectID, kind, j, b.EventSeq)
		return err
	}
	if b.Series != nil {
		if err := upsert(b.Series.ID, "series", mustJSON(b.Series)); err != nil {
			return err
		}
	}
	for i := range b.Peaks {
		if err := upsert(b.Peaks[i].ID, "peak", mustJSON(b.Peaks[i])); err != nil {
			return err
		}
	}
	for i := range b.Stages {
		if err := upsert(b.Stages[i].ID, "stage", mustJSON(b.Stages[i])); err != nil {
			return err
		}
	}
	for i := range b.Residues {
		if err := upsert(b.Residues[i].ID, "residue", mustJSON(b.Residues[i])); err != nil {
			return err
		}
	}
	for i := range b.Judgments {
		if err := upsert(b.Judgments[i].ID, "judgment", mustJSON(b.Judgments[i])); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func staleReason(m map[string]string, id, fallback string) string {
	if m != nil {
		if r, ok := m[id]; ok {
			return r
		}
	}
	return fallback
}

// DerivedRows are raw derived cache rows for replay/export.
type DerivedRows struct {
	ID, Kind, JSON string
	BornSeq        int64
	StaleSeq       sql.NullInt64
	StaleReason    sql.NullString
}

func (s *Store) DerivedList(projectID string) ([]DerivedRows, error) {
	rows, err := s.DB.Query(`SELECT id,kind,json,born_seq,stale_seq,stale_reason FROM derived WHERE project_id=? ORDER BY born_seq,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DerivedRows
	for rows.Next() {
		var r DerivedRows
		if err := rows.Scan(&r.ID, &r.Kind, &r.JSON, &r.BornSeq, &r.StaleSeq, &r.StaleReason); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CurrentBundle reconstructs the bundle current at head, attaching stale flags.
func (s *Store) CurrentBundle(projectID string, headSeq int64) (*domain.ResultBundle, error) {
	rows, err := s.DerivedList(projectID)
	if err != nil {
		return nil, err
	}
	b := &domain.ResultBundle{EventSeq: headSeq}
	latestBorn := map[string]int64{}
	for _, r := range rows {
		if r.Kind == "series" {
			if n, ok := latestBorn["series"]; !ok || r.BornSeq > n {
				latestBorn["series"] = r.BornSeq
			}
		}
	}
	for _, r := range rows {
		switch r.Kind {
		case "series":
			if r.BornSeq == latestBorn["series"] {
				var sr domain.Series
				if err := unmarshal(r.JSON, &sr); err != nil {
					return nil, err
				}
				b.Series = &sr
			}
		case "peak":
			var o domain.Peak
			_ = unmarshal(r.JSON, &o)
			b.Peaks = append(b.Peaks, o)
		case "stage":
			var o domain.StageCandidate
			_ = unmarshal(r.JSON, &o)
			b.Stages = append(b.Stages, o)
		case "residue":
			var o domain.Residue
			_ = unmarshal(r.JSON, &o)
			b.Residues = append(b.Residues, o)
		case "judgment":
			var o domain.Judgment
			_ = unmarshal(r.JSON, &o)
			b.Judgments = append(b.Judgments, o)
		}
	}
	return b, nil
}
