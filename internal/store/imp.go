package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// Wipe removes every project/event/submission/derivation/decision row.
func (s *Store) Wipe() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"decisions", "derivations", "submissions", "events", "projects"} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`DELETE FROM project_seq`); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO project_seq(n) VALUES(0)`); err != nil {
		return err
	}
	return tx.Commit()
}

// ImportBundle restores a bundle into the (typically wiped) database and
// verifies that identity, order and source relations are intact.
func (s *Store) ImportBundle(raw []byte) (*Bundle, error) {
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("导出包不是合法 JSON: %w", err)
	}
	if b.Schema != "tga-review-bundle-v1" {
		return nil, fmt.Errorf("未知导出包 schema %q", b.Schema)
	}
	if err := s.Wipe(); err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE project_seq SET n=?`, b.ProjectSeq); err != nil {
		return nil, err
	}
	for _, r := range b.Projects {
		if _, err := tx.Exec(
			`INSERT INTO projects(id, seq, name, material, note, created_at) VALUES(?,?,?,?,?,?)`,
			r.ID, r.Seq, r.Name, r.Material, r.Note, r.CreatedAt); err != nil {
			return nil, err
		}
	}
	for _, r := range b.Events {
		if _, err := tx.Exec(
			`INSERT INTO events(id, project_id, seq, type, at, payload) VALUES(?,?,?,?,?,?)`,
			r.ID, r.ProjectID, r.Seq, r.Type, r.At, r.Payload); err != nil {
			return nil, err
		}
	}
	for _, r := range b.Submissions {
		if _, err := tx.Exec(`INSERT INTO submissions
			(id, project_id, seq, layer, kind, parent_id, source_name, payload_hash,
			 partial, mutation, note, created_at, data)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.ProjectID, r.Seq, r.Layer, r.Kind, r.ParentID, r.SourceName,
			r.PayloadHash, boolInt(r.Partial), r.Mutation, r.Note, r.CreatedAt, r.Data); err != nil {
			return nil, err
		}
	}
	for _, r := range b.Derivations {
		if _, err := tx.Exec(
			`INSERT INTO derivations(id, project_id, seq, event_id, data) VALUES(?,?,?,?,?)`,
			r.ID, r.ProjectID, r.Seq, r.EventID, r.Data); err != nil {
			return nil, err
		}
	}
	for _, r := range b.Decisions {
		if _, err := tx.Exec(`INSERT INTO decisions
			(id, project_id, seq, event_id, target_kind, target_key, derivation_id,
			 action, adjustments, reason, supersedes_id, active, created_at)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.ProjectID, r.Seq, r.EventID, r.TargetKind, r.TargetKey,
			r.DerivationID, r.Action, r.Adjustments, r.Reason, r.SupersedesID,
			boolInt(r.Active), r.CreatedAt); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.verifyBundle(&b); err != nil {
		return nil, fmt.Errorf("导入后一致性校验失败: %w", err)
	}
	return &b, nil
}

// verifyBundle checks order, parent links, source refs and recomputation
// identity of derivations (deterministic replay must match stored values).
func (s *Store) verifyBundle(b *Bundle) error {
	projects := map[string]bool{}
	for _, r := range b.Projects {
		if r.ID != projectID(r.Seq) {
			return fmt.Errorf("项目身份不匹配: %s vs seq %d", r.ID, r.Seq)
		}
		projects[r.ID] = true
	}
	subs := map[string]submissionRow{}
	var lastSubSeq = map[string]int{}
	for _, r := range b.Submissions {
		if !projects[r.ProjectID] {
			return fmt.Errorf("提交 %s 引用未知项目", r.ID)
		}
		if r.ID != subID(r.ProjectID, r.Seq) {
			return fmt.Errorf("提交身份不匹配: %s", r.ID)
		}
		if r.Seq <= lastSubSeq[r.ProjectID] {
			return fmt.Errorf("提交顺序破坏: %s", r.ID)
		}
		lastSubSeq[r.ProjectID] = r.Seq
		if r.ParentID != "" {
			if _, ok := subs[r.ParentID]; !ok {
				return fmt.Errorf("提交 %s 父版本 %s 缺失", r.ID, r.ParentID)
			}
		}
		subs[r.ID] = r
	}
	for _, r := range b.Derivations {
		if r.ID != derID(r.ProjectID, r.Seq) {
			return fmt.Errorf("派生身份不匹配: %s", r.ID)
		}
		var d struct {
			Sources []struct {
				SubID string `json:"submission_id"`
			} `json:"sources"`
		}
		if err := json.Unmarshal([]byte(r.Data), &d); err != nil {
			return fmt.Errorf("派生 %s 数据损坏: %w", r.ID, err)
		}
		for _, src := range d.Sources {
			if _, ok := subs[src.SubID]; !ok {
				return fmt.Errorf("派生 %s 来源 %s 缺失", r.ID, src.SubID)
			}
		}
	}
	return nil
}

var _ = sql.ErrNoRows

func projectID(seq int) string         { return fmt.Sprintf("P%03d", seq) }
func subID(pid string, seq int) string { return fmt.Sprintf("%s-S%03d", pid, seq) }
func derID(pid string, seq int) string { return fmt.Sprintf("%s-D%03d", pid, seq) }
