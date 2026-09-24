// Package store 提供 SQLite 持久化、事件溯源视图与导出/导入。
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"

	"tga-review/internal/core"
)

// Clock 是虚拟时钟：测试用固定步进，服务器用真实时间。
type Clock interface{ Now() int64 }

type Store struct {
	db    *sql.DB
	clk   Clock
	idSeq int64
}

func Open(path string, clk Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, clk: clk}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE IF NOT EXISTS layers (
  id TEXT PRIMARY KEY, project_id TEXT, kind TEXT, version INTEGER,
  parent_id TEXT, format TEXT, raw TEXT, status TEXT,
  diagnostics TEXT, submitted_at INTEGER
);
CREATE TABLE IF NOT EXISTS results (
  id TEXT PRIMARY KEY, project_id TEXT, kind TEXT, algo_version TEXT,
  param_fp TEXT, inputs TEXT, payload TEXT, created_seq INTEGER
);
CREATE TABLE IF NOT EXISTS candidates (
  id TEXT PRIMARY KEY, result_id TEXT, project_id TEXT, payload TEXT,
  decided_seq INTEGER
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY, id TEXT, project_id TEXT, kind TEXT,
  payload TEXT, at INTEGER
);
CREATE TABLE IF NOT EXISTS heads (project_id TEXT PRIMARY KEY, head_seq INTEGER);
`)
	return err
}

func (s *Store) nextID(prefix string) string {
	s.idSeq++
	return fmt.Sprintf("%s_%06d", prefix, s.idSeq)
}

// ensureIDs 恢复 ID 计数器，避免导入后与既有 ID 冲突。
func (s *Store) ensureIDs() error {
	var maxSeq sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(CAST(substr(id, instr(id,'_')+1) AS INTEGER)) FROM (
	  SELECT id FROM layers UNION ALL SELECT id FROM results
	  UNION ALL SELECT id FROM candidates UNION ALL SELECT id FROM events)`).Scan(&maxSeq); err != nil {
		return err
	}
	if maxSeq.Valid && maxSeq.Int64 > s.idSeq {
		s.idSeq = maxSeq.Int64
	}
	return nil
}

// AppendEvent 追加事件并推进项目头。历史只可追加，从不覆盖。
func (s *Store) AppendEvent(projectID, kind string, payload any) (*core.Event, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&seq); err != nil {
		return nil, err
	}
	ev := &core.Event{Seq: seq, ID: s.nextID("ev"), ProjectID: projectID, Kind: kind, Payload: b, At: s.clk.Now()}
	if _, err := tx.Exec(`INSERT INTO events(seq,id,project_id,kind,payload,at) VALUES(?,?,?,?,?,?)`,
		ev.Seq, ev.ID, ev.ProjectID, ev.Kind, string(b), ev.At); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`INSERT INTO heads(project_id,head_seq) VALUES(?,?)
	  ON CONFLICT(project_id) DO UPDATE SET head_seq=excluded.head_seq`, projectID, seq); err != nil {
		return nil, err
	}
	return ev, tx.Commit()
}

func (s *Store) PutLayer(l *core.Layer) error {
	d, _ := json.Marshal(l.Diagnostics)
	_, err := s.db.Exec(`INSERT INTO layers(id,project_id,kind,version,parent_id,format,raw,status,diagnostics,submitted_at)
	  VALUES(?,?,?,?,?,?,?,?,?,?)`,
		l.ID, l.ProjectID, l.Kind, l.Version, l.ParentID, l.Format, l.Raw, l.Status, string(d), l.SubmittedAt)
	return err
}

func (s *Store) PutResult(r *core.Result) error {
	in, _ := json.Marshal(r.Inputs)
	_, err := s.db.Exec(`INSERT INTO results(id,project_id,kind,algo_version,param_fp,inputs,payload,created_seq)
	  VALUES(?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.Kind, r.AlgoVersion, r.ParamFP, string(in), string(r.Payload), r.CreatedSeq)
	return err
}

func (s *Store) PutCandidate(c *core.StageCandidate) error {
	b, _ := json.Marshal(c)
	_, err := s.db.Exec(`INSERT INTO candidates(id,result_id,project_id,payload,decided_seq) VALUES(?,?,?,?,?)`,
		c.ID, c.ResultID, c.ProjectID, string(b), c.DecidedSeq)
	return err
}

// UpdateCandidateVerdict 更新候选裁定（候选本身可变的是裁定状态；历史由事件承载）。
func (s *Store) UpdateCandidateVerdict(id, verdict, reason string, decidedSeq int64) error {
	c, err := s.GetCandidate(id)
	if err != nil {
		return err
	}
	c.Verdict, c.RejectReason, c.DecidedSeq = verdict, reason, decidedSeq
	b, _ := json.Marshal(c)
	_, err = s.db.Exec(`UPDATE candidates SET payload=?, decided_seq=? WHERE id=?`, string(b), decidedSeq, id)
	return err
}

func (s *Store) GetCandidate(id string) (*core.StageCandidate, error) {
	var b string
	if err := s.db.QueryRow(`SELECT payload FROM candidates WHERE id=?`, id).Scan(&b); err != nil {
		return nil, err
	}
	var c core.StageCandidate
	return &c, json.Unmarshal([]byte(b), &c)
}

// Head 返回项目当前头事件序号。
func (s *Store) Head(projectID string) (int64, error) {
	var h int64
	err := s.db.QueryRow(`SELECT head_seq FROM heads WHERE project_id=?`, projectID).Scan(&h)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return h, err
}

// SetHead 移动头指针（撤销/回到旧事件）；移动本身已作为事件追加。
func (s *Store) SetHead(projectID string, seq int64) error {
	_, err := s.db.Exec(`INSERT INTO heads(project_id,head_seq) VALUES(?,?)
	  ON CONFLICT(project_id) DO UPDATE SET head_seq=excluded.head_seq`, projectID, seq)
	return err
}

func (s *Store) Events(projectID string, upTo int64) ([]*core.Event, error) {
	q := `SELECT seq,id,project_id,kind,payload,at FROM events WHERE project_id=?`
	args := []any{projectID}
	if upTo > 0 {
		q += ` AND seq<=?`
		args = append(args, upTo)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Event
	for rows.Next() {
		var e core.Event
		var payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.ProjectID, &e.Kind, &payload, &e.At); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, &e)
	}
	return out, rows.Err()
}

func (s *Store) Layers(projectID string) ([]*core.Layer, error) {
	rows, err := s.db.Query(`SELECT id,project_id,kind,version,parent_id,format,raw,status,diagnostics,submitted_at
	  FROM layers WHERE project_id=? ORDER BY kind, version, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Layer
	for rows.Next() {
		var l core.Layer
		var diags string
		if err := rows.Scan(&l.ID, &l.ProjectID, &l.Kind, &l.Version, &l.ParentID, &l.Format, &l.Raw, &l.Status, &diags, &l.SubmittedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(diags), &l.Diagnostics)
		out = append(out, &l)
	}
	return out, rows.Err()
}

func (s *Store) Results(projectID string) ([]*core.Result, error) {
	rows, err := s.db.Query(`SELECT id,project_id,kind,algo_version,param_fp,inputs,payload,created_seq
	  FROM results WHERE project_id=? ORDER BY created_seq, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.Result
	for rows.Next() {
		var r core.Result
		var inputs, payload string
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.AlgoVersion, &r.ParamFP, &inputs, &payload, &r.CreatedSeq); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(inputs), &r.Inputs)
		r.Payload = json.RawMessage(payload)
		out = append(out, &r)
	}
	return out, rows.Err()
}

func (s *Store) Candidates(projectID string) ([]*core.StageCandidate, error) {
	rows, err := s.db.Query(`SELECT payload FROM candidates WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*core.StageCandidate
	for rows.Next() {
		var b string
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		var c core.StageCandidate
		if err := json.Unmarshal([]byte(b), &c); err != nil {
			return nil, err
		}
		out = append(out, &c)
	}
	return out, rows.Err()
}

// View 是某一事件序号处的可见状态：层、结果、候选与裁定。
type View struct {
	Seq        int64
	Layers     []*core.Layer
	Latest     map[string]*core.Layer // kind -> 最新版本
	Results    []*core.Result
	Stale      map[string]bool // 过期结果：仍可查看，不能冒充当前
	Candidates []*core.StageCandidate
	Events     []*core.Event
}

// ViewAt 重放到指定事件序号（0 = 当前头），恢复当时的依赖版本与可见结论。
func (s *Store) ViewAt(projectID string, seq int64) (*View, error) {
	head, err := s.Head(projectID)
	if err != nil {
		return nil, err
	}
	if seq <= 0 || seq > head {
		seq = head
	}
	events, err := s.Events(projectID, seq)
	if err != nil {
		return nil, err
	}
	v := &View{Seq: seq, Latest: map[string]*core.Layer{}, Stale: map[string]bool{}, Events: events}
	// 事件重放构建可见状态。
	layerIDs := map[string]bool{}
	resultIDs := map[string]bool{}
	candIDs := map[string]bool{}
	verdicts := map[string][3]string{} // cand -> verdict/reason
	decidedSeq := map[string]int64{}
	for _, e := range events {
		switch e.Kind {
		case core.EvLayerImported, core.EvLayerRejected:
			var p struct {
				LayerID string `json:"layer_id"`
			}
			json.Unmarshal(e.Payload, &p)
			layerIDs[p.LayerID] = true
		case core.EvDerivationRun:
			var p struct {
				ResultIDs []string `json:"result_ids"`
			}
			json.Unmarshal(e.Payload, &p)
			for _, id := range p.ResultIDs {
				resultIDs[id] = true
			}
		case core.EvCandidateGenerated:
			var p struct {
				CandidateID string `json:"candidate_id"`
			}
			json.Unmarshal(e.Payload, &p)
			candIDs[p.CandidateID] = true
		case core.EvAdjudication:
			var p struct {
				CandidateID string `json:"candidate_id"`
				Verdict     string `json:"verdict"`
				Reason      string `json:"reason"`
			}
			json.Unmarshal(e.Payload, &p)
			verdicts[p.CandidateID] = [3]string{p.Verdict, p.Reason, ""}
			decidedSeq[p.CandidateID] = e.Seq
		}
	}
	layers, err := s.Layers(projectID)
	if err != nil {
		return nil, err
	}
	byID := map[string]*core.Layer{}
	for _, l := range layers {
		byID[l.ID] = l
		if layerIDs[l.ID] {
			v.Layers = append(v.Layers, l)
			cur := v.Latest[l.Kind]
			if cur == nil || l.Version > cur.Version {
				v.Latest[l.Kind] = l
			}
		}
	}
	sort.Slice(v.Layers, func(i, j int) bool {
		if v.Layers[i].Kind != v.Layers[j].Kind {
			return kindOrder(v.Layers[i].Kind) < kindOrder(v.Layers[j].Kind)
		}
		return v.Layers[i].Version < v.Layers[j].Version
	})
	results, err := s.Results(projectID)
	if err != nil {
		return nil, err
	}
	for _, r := range results {
		if !resultIDs[r.ID] {
			continue
		}
		v.Results = append(v.Results, r)
		// 局部过期：任一直接来源出现更新版本 → 该结果过期。
		for _, in := range r.Inputs {
			latest := v.Latest[in.Kind]
			if latest != nil && latest.Version > in.Version && sameLineage(byID, latest, in.LayerID) {
				v.Stale[r.ID] = true
			}
		}
	}
	cands, err := s.Candidates(projectID)
	if err != nil {
		return nil, err
	}
	for _, c := range cands {
		if !candIDs[c.ID] {
			continue
		}
		cp := *c
		if vd, ok := verdicts[c.ID]; ok {
			cp.Verdict, cp.RejectReason, cp.DecidedSeq = vd[0], vd[1], decidedSeq[c.ID]
		} else if cp.DecidedSeq > seq {
			cp.Verdict, cp.RejectReason, cp.DecidedSeq = "pending", "", 0
		}
		v.Candidates = append(v.Candidates, &cp)
	}
	return v, nil
}

func kindOrder(k string) int {
	for i, kk := range core.LayerKinds {
		if kk == k {
			return i
		}
	}
	return 99
}

// sameLineage 判断 newer 是否源自 older（沿父版本链）。
func sameLineage(byID map[string]*core.Layer, newer *core.Layer, olderID string) bool {
	cur := newer
	for cur != nil {
		if cur.ID == olderID {
			return true
		}
		cur = byID[cur.ParentID]
	}
	return false
}
