package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tga-review/internal/tga"
)

func (s *Store) State(projectID string) (State, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return State{}, err
	}
	defer tx.Rollback()
	return loadStateTx(tx, projectID)
}

func (s *Store) EventState(projectID string, eventID int64) (State, error) {
	var snapshot string
	err := s.db.QueryRow(`SELECT snapshot_json FROM events WHERE project_id=? AND id=?`, projectID, eventID).Scan(&snapshot)
	if err == sql.ErrNoRows {
		return State{}, ErrNotFound
	}
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal([]byte(snapshot), &state); err != nil {
		return State{}, err
	}
	return state, nil
}

func loadProjectTx(tx *sql.Tx, id string) (Project, error) {
	var project Project
	var paramsJSON, created, updated string
	err := tx.QueryRow(`SELECT id,name,params_json,created_at,updated_at FROM projects WHERE id=?`, id).Scan(&project.ID, &project.Name, &paramsJSON, &created, &updated)
	if err == sql.ErrNoRows {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, err
	}
	json.Unmarshal([]byte(paramsJSON), &project.Params)
	project.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	project.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return project, nil
}

func requireProject(tx *sql.Tx, id string) error {
	var exists int
	err := tx.QueryRow(`SELECT 1 FROM projects WHERE id=?`, id).Scan(&exists)
	if err == sql.ErrNoRows {
		return ErrNotFound
	}
	return err
}

func loadSourcesTx(tx *sql.Tx, projectID string) ([]tga.Source, error) {
	rows, err := tx.Query(`SELECT id,kind,version,COALESCE(parent_id,''),label,raw_text,payload_json,seq,created_at,status,COALESCE(error_text,'') FROM sources WHERE project_id=? ORDER BY seq,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tga.Source{}
	for rows.Next() {
		var source tga.Source
		var payloadJSON, created string
		if err := rows.Scan(&source.ID, &source.Kind, &source.Version, &source.ParentID, &source.Label, &source.Raw, &payloadJSON, &source.Order, &created, &source.Status, &source.Error); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(payloadJSON), &source.Payload)
		source.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, source)
	}
	return out, rows.Err()
}

func loadStateTx(tx *sql.Tx, projectID string) (State, error) {
	project, err := loadProjectTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	sources, err := loadSourcesTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	nodes, err := loadNodesTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	decisions, err := loadDecisionsTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	events, err := loadEventsTx(tx, projectID)
	if err != nil {
		return State{}, err
	}
	judgments := []tga.Judgment{}
	for _, node := range nodes {
		judgments = append(judgments, node.Diagnoses...)
	}
	for _, source := range sources {
		if source.Status == "failed" {
			judgments = append(judgments, tga.Judgment{Code: "import_half_failure", Severity: "error", Message: source.Error, ObjectKeys: []string{"raw:" + source.ID}, SourceIDs: []string{source.ID}, RuleVersion: tga.AlgorithmCandidates})
		}
	}
	return State{Project: project, Sources: sources, Nodes: nodes, Decisions: decisions, Events: events, Judgments: judgments}, nil
}

func loadNodesTx(tx *sql.Tx, projectID string) ([]tga.Node, error) {
	rows, err := tx.Query(`SELECT payload_json FROM nodes WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tga.Node{}
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var node tga.Node
		if err := json.Unmarshal([]byte(payload), &node); err != nil {
			return nil, err
		}
		out = append(out, node)
	}
	return out, rows.Err()
}

func loadDecisionsTx(tx *sql.Tx, projectID string) ([]tga.Decision, error) {
	rows, err := tx.Query(`SELECT id,event_id,candidate_key,stage,alternative,action,reason,parameters_json,conflict_json,COALESCE(superseded_by,''),created_at,status FROM decisions WHERE project_id=? ORDER BY event_id,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []tga.Decision{}
	for rows.Next() {
		var decision tga.Decision
		var paramsJSON, conflictJSON, created, status string
		if err := rows.Scan(&decision.ID, &decision.EventID, &decision.CandidateKey, &decision.Stage, &decision.Alternative, &decision.Action, &decision.Reason, &paramsJSON, &conflictJSON, &decision.SupersededBy, &created, &status); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(paramsJSON), &decision.Parameters)
		json.Unmarshal([]byte(conflictJSON), &decision.ConflictWith)
		decision.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		_ = status
		out = append(out, decision)
	}
	return out, rows.Err()
}

func loadEventsTx(tx *sql.Tx, projectID string) ([]Event, error) {
	rows, err := tx.Query(`SELECT id,project_id,type,summary,COALESCE(source_id,''),payload_json,snapshot_json,created_at FROM events WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
	var event Event
	var created string
		if err := rows.Scan(&event.ID, &event.ProjectID, &event.Type, &event.Summary, &event.SourceID, &event.Payload, &event.Snapshot, &created); err != nil {
			return nil, err
		}
		event.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, event)
	}
	return out, rows.Err()
}

func appendEvent(tx *sql.Tx, projectID, typ, summary, sourceID string, payload []byte, state State, now time.Time) error {
	if payload == nil {
		payload = []byte(`{}`)
	}
	state.Events = nil
	snapshot, err := json.Marshal(state)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO events(project_id,type,summary,source_id,payload_json,snapshot_json,created_at) VALUES(?,?,?,?,?,?,?)`, projectID, typ, summary, sourceID, string(payload), string(snapshot), now.UTC().Format(time.RFC3339Nano))
	return err
}

func debugInvalid(message string, args ...interface{}) error {
	return fmt.Errorf(message, args...)
}
