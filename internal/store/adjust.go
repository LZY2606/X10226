package store

import (
	"encoding/json"
	"fmt"

	"tga-review/internal/domain"
)

// AdjustParams records a parameter revision (gas delay / temp lag /
// thresholds) as a new immutable params version with a parent link, then
// derives. This is the "参数调整" path from the review page.
func (s *Store) AdjustParams(pid, reason string, p domain.Params, mutation string) (*domain.Submission, *domain.Derivation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	if _, err := s.getProjectLocked(tx, pid); err != nil {
		return nil, nil, err
	}
	seq, err := nextSeqLocked(tx, "submissions", pid)
	if err != nil {
		return nil, nil, err
	}
	parent, _ := latestSubmissionLocked(tx, pid, domain.LayerParams)
	p.Algorithm = "tga-v1"
	if p.BlankDriftModel == "" {
		p.BlankDriftModel = "blank_linear"
	}
	pb, _ := json.Marshal(p)
	sub := domain.Submission{
		ProjectID: pid, Seq: seq, Layer: domain.LayerParams, Kind: "revision",
		SourceName: "params.json", PayloadHash: domain.Fingerprint(string(pb)),
		Params: &p, Mutation: mutation, Note: reason, CreatedAt: s.now(),
	}
	if parent != nil {
		sub.ParentID = parent.ID
	}
	sub.ID = domain.SubmissionStableID(pid, seq)
	data, _ := json.Marshal(sub)
	if _, err := tx.Exec(`INSERT INTO submissions
		(id, project_id, seq, layer, kind, parent_id, source_name, payload_hash,
		 partial, mutation, note, created_at, data)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		sub.ID, pid, seq, string(sub.Layer), sub.Kind, sub.ParentID, sub.SourceName,
		sub.PayloadHash, 0, sub.Mutation, sub.Note,
		sub.CreatedAt.Format("2006-01-02T15:04:05.999999999Z07:00"), string(data)); err != nil {
		return nil, nil, err
	}
	eseq, err := nextEventSeqLocked(tx, pid)
	if err != nil {
		return nil, nil, err
	}
	eid, err := appendEvent(tx, pid, eseq, "params_revised", s.now(), map[string]any{
		"submission_id": sub.ID, "parent_id": sub.ParentID,
		"param_fingerprint": domain.ParamsFingerprint(p), "reason": reason,
		"mutation": mutation,
	})
	if err != nil {
		return nil, nil, err
	}
	d, err := deriveLocked(tx, pid, eid, s.now())
	if err != nil {
		return nil, nil, fmt.Errorf("adjust: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return &sub, d, nil
}

// AddRawRevision appends a revised non-params stratum (the "只影响局部依赖的
// 源数据改动" path) with the mutation tag, then derives.
func (s *Store) AddRawRevision(pid string, sub domain.Submission) (*domain.Submission, *domain.Derivation, error) {
	return s.addSubmissionInternal(pid, sub)
}
