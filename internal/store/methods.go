package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"tga-review/internal/tga"
)

var ErrNotFound = errors.New("not found")

func (s *Store) CreateProject(id, name string, params tga.Params, now time.Time) (Project, error) {
	if id == "" {
		id = "p_tga_001"
	}
	if params == (tga.Params{}) {
		params = tga.DefaultParams()
	}
	paramsJSON, _ := json.Marshal(params)
	project := Project{ID: id, Name: name, Params: params, CreatedAt: now, UpdatedAt: now}
	_, err := s.db.Exec(`INSERT INTO projects(id,name,params_json,created_at,updated_at) VALUES(?,?,?,?,?)`, id, name, string(paramsJSON), now.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
	return project, err
}

func (s *Store) ListProjects() ([]Project, error) {
	rows, err := s.db.Query(`SELECT id,name,params_json,created_at,updated_at FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var project Project
		var paramsJSON, created, updated string
		if err := rows.Scan(&project.ID, &project.Name, &paramsJSON, &created, &updated); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(paramsJSON), &project.Params)
		project.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		project.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, project)
	}
	return out, rows.Err()
}

func (s *Store) ImportSources(projectID string, inputs []SourceInput, now time.Time) (State, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	if err := requireProject(tx, projectID); err != nil {
		return State{}, err
	}
	for _, input := range inputs {
		if err := insertSource(tx, projectID, input, now); err != nil {
			return State{}, err
		}
	}
	state, err := recompute(tx, projectID, now)
	if err != nil {
		return State{}, err
	}
	payload, _ := json.Marshal(inputs)
	if err := appendEvent(tx, projectID, "import", "导入分层原始输入；原始层不可覆盖", "", payload, state, now); err != nil {
		return State{}, err
	}
	if err := tx.Commit(); err != nil {
		return State{}, err
	}
	return state, nil
}

func insertSource(tx *sql.Tx, projectID string, input SourceInput, now time.Time) error {
	if input.Status == "" {
		input.Status = "accepted"
	}
	var version int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM sources WHERE project_id=? AND kind=?`, projectID, input.Kind).Scan(&version); err != nil {
		return err
	}
	var parentID string
	_ = tx.QueryRow(`SELECT id FROM sources WHERE project_id=? AND kind=? ORDER BY version DESC LIMIT 1`, projectID, input.Kind).Scan(&parentID)
	var seq int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM sources WHERE project_id=?`, projectID).Scan(&seq); err != nil {
		return err
	}
	payloadJSON, _ := json.Marshal(input.Payload)
	id := fmt.Sprintf("src:%s-v%d", input.Kind, version)
	_, err := tx.Exec(`INSERT INTO sources(id,project_id,kind,version,parent_id,label,raw_text,payload_json,status,error_text,seq,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, id, projectID, input.Kind, version, parentID, input.Label, input.Raw, string(payloadJSON), input.Status, input.Error, seq, now.UTC().Format(time.RFC3339Nano))
	return err
}

func recompute(tx *sql.Tx, projectID string, now time.Time) (State, error) {
	sources, err := loadSourcesTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	project, err := loadProjectTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	current := map[string]tga.Source{}
	failed := map[string]tga.Source{}
	for _, source := range sources {
		if source.Status == "failed" {
			if _, exists := current[source.Kind]; !exists {
				failed[source.Kind] = source
			}
		} else {
			current[source.Kind] = source
			delete(failed, source.Kind)
		}
	}
	result := tga.Compute(tga.InputBundle{Sources: sources, CurrentByKind: current, FailedByKind: failed, Params: project.Params})
	if _, err := tx.Exec(`DELETE FROM nodes WHERE project_id=?`, projectID); err != nil {
		return State{}, err
	}
	for _, node := range result.Nodes {
		payload, _ := json.Marshal(node)
		_, err := tx.Exec(`INSERT INTO nodes(project_id,node_key,version,display_id,status,payload_json,created_at) VALUES(?,?,?,?,?,?,?)`, projectID, node.Key, node.Version, node.DisplayID, node.Status, string(payload), now.UTC().Format(time.RFC3339Nano))
		if err != nil {
			return State{}, err
		}
	}
	if err := markDecisionStaleness(tx, projectID, result.Nodes); err != nil {
		return State{}, err
	}
	return loadStateTx(tx, projectID)
}

func markDecisionStaleness(tx *sql.Tx, projectID string, nodes []tga.Node) error {
	valid := map[string]bool{}
	for _, node := range nodes {
		for _, candidate := range node.Candidates {
			valid["candidate:"+candidate.ID] = true
		}
	}
	rows, err := tx.Query(`SELECT candidate_key FROM decisions WHERE project_id=? AND superseded_by=''`, projectID)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, key)
	}
	rows.Close()
	for _, key := range keys {
		status := "stale"
		if valid[key] {
			status = "current"
		}
		if _, err := tx.Exec(`UPDATE decisions SET superseded_by=CASE WHEN ?='stale' THEN 'source-revision' ELSE '' END WHERE project_id=? AND candidate_key=?`, status, projectID, key); err != nil {
			return err
		}
	}
	return nil
}
