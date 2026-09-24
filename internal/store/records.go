package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"tgaline/internal/domain"
)

// AppendEvent inserts an event. Payload is stored with a type tag so replay
// decodes the concrete payload struct.
func (s *Store) AppendEvent(ev *domain.Event) error {
	tagged, err := json.Marshal(map[string]any{"@type": string(ev.Type), "data": ev.Payload})
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO events(seq,project_id,id,type,at,actor,parent_id,payload)
		 VALUES(?,?,?,?,?,?,?,?)`,
		ev.Seq, ev.ProjectID, ev.ID, string(ev.Type), ev.At, ev.Actor, ev.ParentID, string(tagged))
	return err
}

func (s *Store) LoadEvents(projectID string) ([]*domain.Event, error) {
	rows, err := s.db.Query(
		`SELECT seq,id,type,at,actor,parent_id,payload FROM events
		 WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Event
	for rows.Next() {
		var ev domain.Event
		var typ, payload string
		if err := rows.Scan(&ev.Seq, &ev.ID, &typ, &ev.At, &ev.Actor, &ev.ParentID, &payload); err != nil {
			return nil, err
		}
		ev.Type = domain.EventType(typ)
		ev.ProjectID = projectID
		var tagged struct {
			Type string          `json:"@type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(payload), &tagged); err != nil {
			return nil, err
		}
		ev.Payload, err = decodePayload(domain.EventType(tagged.Type), tagged.Data)
		if err != nil {
			return nil, err
		}
		out = append(out, &ev)
	}
	return out, rows.Err()
}

func decodePayload(t domain.EventType, data json.RawMessage) (any, error) {
	switch t {
	case domain.EvProjectCreated:
		var v map[string]any
		return v, json.Unmarshal(data, &v)
	case domain.EvLayerCommitted:
		var v domain.LayerCommitPayload
		return v, json.Unmarshal(data, &v)
	case domain.EvParamsRevised:
		var v domain.ParamsPayload
		return v, json.Unmarshal(data, &v)
	case domain.EvBuildSucceeded:
		var v domain.BuildPayload
		return v, json.Unmarshal(data, &v)
	case domain.EvDecisionMade:
		var v domain.DecisionPayload
		return v, json.Unmarshal(data, &v)
	case domain.EvDecisionUndone, domain.EvConflictResolved:
		var v map[string]any
		return v, json.Unmarshal(data, &v)
	case domain.EvArchiveImported:
		var v domain.ArchivePayload
		return v, json.Unmarshal(data, &v)
	default:
		var v map[string]any
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("unknown payload type %q", t)
		}
		return v, nil
	}
}

func (s *Store) SaveLayer(l *domain.Layer) error {
	body, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT OR REPLACE INTO layers(id,project_id,kind,version,parent_id,origin,sha,body,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		l.ID, l.ProjectID, string(l.Kind), l.Version, l.ParentID, l.Origin, l.SHA, string(body), l.CreatedAt)
	return err
}

func (s *Store) LoadLayer(id string) (*domain.Layer, error) {
	var body string
	row := s.db.QueryRow(`SELECT body FROM layers WHERE id=?`, id)
	if err := row.Scan(&body); err != nil {
		return nil, err
	}
	var l domain.Layer
	if err := json.Unmarshal([]byte(body), &l); err != nil {
		return nil, err
	}
	return &l, nil
}

func (s *Store) SaveNode(n *domain.Node) error {
	src, err := json.Marshal(n.DirectSources)
	if err != nil {
		return err
	}
	notes, _ := json.Marshal(n.Notes)
	_, err = s.db.Exec(
		`INSERT OR IGNORE INTO nodes(id,kind,algo_version,params_fp,sources,body,status,notes,project_hint)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		n.ID, string(n.Kind), n.AlgoVersion, n.ParamsFP, string(src), n.Body, n.Status, string(notes), "")
	return err
}

func (s *Store) LoadNode(id string) (*domain.Node, error) {
	var src, body, status, notes string
	var kind, algo, fp string
	row := s.db.QueryRow(
		`SELECT kind,algo_version,params_fp,sources,body,status,notes FROM nodes WHERE id=?`, id)
	if err := row.Scan(&kind, &algo, &fp, &src, &body, &status, &notes); err != nil {
		return nil, err
	}
	n := &domain.Node{ID: id, Kind: domain.NodeKind(kind), AlgoVersion: algo, ParamsFP: fp,
		Body: body, Status: status}
	if err := json.Unmarshal([]byte(src), &n.DirectSources); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(notes), &n.Notes)
	return n, nil
}

func (s *Store) ListNodeIDs(projectHint string) ([]string, error) {
	rows, err := s.db.Query(`SELECT id FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// AllNodes returns every node (used by archive export); nodes are globally
// content-addressed so the project hint is not needed.
func (s *Store) AllNodes() ([]*domain.Node, error) {
	rows, err := s.db.Query(`SELECT id FROM nodes ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*domain.Node, 0, len(ids))
	for _, id := range ids {
		n, err := s.LoadNode(id)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// AllLayers returns every stored layer.
func (s *Store) AllLayers() ([]*domain.Layer, error) {
	rows, err := s.db.Query(`SELECT id FROM layers ORDER BY project_id, kind, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*domain.Layer, 0, len(ids))
	for _, id := range ids {
		l, err := s.LoadLayer(id)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, nil
}

var _ sql.NullString
var _ = fmt.Sprintf
