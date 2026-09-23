package store

import (
	"encoding/json"
	"fmt"

	"tga-review/internal/model"
)

// ExportVersion is the wire format version for project export.
const ExportVersion = "tga-export.v1"

// Export is the full portable snapshot of one project. Identities,
// ordering (seq) and provenance relations are preserved verbatim so a
// delete + re-import round-trip restores them exactly.
type Export struct {
	Format    string                `json:"format"`
	Project   model.Project         `json:"project"`
	Events    []model.Event         `json:"events"`
	Batches   []model.Batch         `json:"batches"`
	Imports   []model.ImportVersion `json:"imports"`
	Derived   []model.Derived       `json:"derived"`
	Decisions []model.Decision      `json:"decisions"`
	Params    []model.ParamsVersion `json:"params"`
}

// ExportProject serializes one project.
func (s *Store) ExportProject(projectID string) ([]byte, error) {
	p, err := s.Project(projectID)
	if err != nil {
		return nil, err
	}
	ex := Export{Format: ExportVersion, Project: p}
	if ex.Events, err = s.Events(projectID, 0); err != nil {
		return nil, err
	}
	if ex.Batches, err = s.Batches(projectID); err != nil {
		return nil, err
	}
	if ex.Imports, err = s.Imports(projectID); err != nil {
		return nil, err
	}
	if ex.Derived, err = s.DerivedAll(projectID); err != nil {
		return nil, err
	}
	if ex.Decisions, err = s.Decisions(projectID); err != nil {
		return nil, err
	}
	if ex.Params, err = s.ParamsAll(projectID); err != nil {
		return nil, err
	}
	return json.MarshalIndent(ex, "", "  ")
}

// ImportProject restores an exported project, preserving all IDs and
// sequence numbers. It fails if the project ID already exists.
func (s *Store) ImportProject(data []byte) (string, error) {
	var ex Export
	if err := json.Unmarshal(data, &ex); err != nil {
		return "", fmt.Errorf("invalid export JSON: %w", err)
	}
	if ex.Format != ExportVersion {
		return "", fmt.Errorf("unsupported export format %q", ex.Format)
	}
	if _, err := s.Project(ex.Project.ID); err == nil {
		return "", fmt.Errorf("project %s already exists", ex.Project.ID)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO projects(id,name,created_seq) VALUES(?,?,?)`,
		ex.Project.ID, ex.Project.Name, ex.Project.CreatedSeq); err != nil {
		return "", err
	}
	for _, e := range ex.Events {
		if _, err := tx.Exec(`INSERT INTO events(seq,id,project_id,vtime,kind,actor,payload) VALUES(?,?,?,?,?,?,?)`,
			e.Seq, e.ID, e.ProjectID, e.VTime, e.Kind, e.Actor, e.Payload); err != nil {
			return "", err
		}
	}
	for _, b := range ex.Batches {
		if _, err := tx.Exec(`INSERT INTO batches(id,project_id,status,seq) VALUES(?,?,?,?)`,
			b.ID, b.ProjectID, b.Status, b.Seq); err != nil {
			return "", err
		}
	}
	for _, v := range ex.Imports {
		if _, err := tx.Exec(`INSERT INTO imports(id,project_id,layer,version,parent_id,batch_id,raw,format,status,diagnostics,seq)
		  VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			v.ID, v.ProjectID, v.Layer, v.Version, v.ParentID, v.BatchID, v.Raw, v.Format, v.Status, v.Diagnostics, v.Seq); err != nil {
			return "", err
		}
	}
	for _, d := range ex.Derived {
		if _, err := tx.Exec(`INSERT INTO derived(id,project_id,kind,algo_version,param_fp,source_ids,status,payload,created_seq,stale_seq)
		  VALUES(?,?,?,?,?,?,?,?,?,?)`,
			d.ID, d.ProjectID, d.Kind, d.AlgoVersion, d.ParamFP, d.SourceIDs, d.Status, d.Payload, d.CreatedSeq, d.StaleSeq); err != nil {
			return "", err
		}
	}
	for _, d := range ex.Decisions {
		if _, err := tx.Exec(`INSERT INTO decisions(id,project_id,target_id,verdict,note,actor,seq,undone_seq,inactive_seq,conflict)
		  VALUES(?,?,?,?,?,?,?,?,?,?)`,
			d.ID, d.ProjectID, d.TargetID, d.Verdict, d.Note, d.Actor, d.Seq, d.UndoneSeq, d.InactiveSeq, d.Conflict); err != nil {
			return "", err
		}
	}
	for _, p := range ex.Params {
		if _, err := tx.Exec(`INSERT INTO params(id,project_id,params,seq) VALUES(?,?,?,?)`,
			p.ID, p.ProjectID, p.Params, p.Seq); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return ex.Project.ID, nil
}
