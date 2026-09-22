package project

import (
	"context"
	"database/sql"
	"encoding/json"

	"tgareview/internal/domain"
	"tgareview/internal/engine"
	"tgareview/internal/ids"
)

// recompute builds the current state from all events, runs the engine and
// caches the artifact. Recomputation is deterministic, so the same basis
// always yields the same artifact id; importing an export reproduces it.
func (s *Service) recompute(ctx context.Context, tx *sql.Tx, projectID string, seq int64) (string, error) {
	state, err := s.replayAt(ctx, tx, projectID, seq)
	if err != nil {
		return "", err
	}
	art, err := s.analyzeState(ctx, tx, projectID, state)
	if err != nil {
		return "", err
	}
	content, _ := json.Marshal(art)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO artifacts(project_id,artifact_id,basis_seq,alg_version,params_fp,content,current)
		 VALUES(?,?,?,?,?,?,1)
		 ON CONFLICT(project_id,artifact_id) DO UPDATE SET basis_seq=excluded.basis_seq, content=excluded.content, current=1`,
		projectID, art.ID, seq, art.AlgorithmVersion, art.ParamsFingerprint, string(content)); err != nil {
		return "", err
	}
	// only the artifact matching the current head basis is current
	if _, err := tx.ExecContext(ctx,
		`UPDATE artifacts SET current = CASE WHEN artifact_id=? THEN 1 ELSE 0 END WHERE project_id=?`,
		art.ID, projectID); err != nil {
		return "", err
	}
	return art.ID, nil
}

func (s *Service) analyzeState(ctx context.Context, tx *sql.Tx, projectID string, state *State) (*engine.Artifact, error) {
	inputs := engine.Inputs{ProjectID: projectID, Params: state.Params,
		Layers: map[domain.LayerKind]*domain.Layer{}, Revisions: map[domain.LayerKind]string{}}
	for _, k := range domain.AllLayers() {
		rev := state.HeadRev[k]
		if rev == "" {
			continue
		}
		var raw string
		err := tx.QueryRowContext(ctx, `SELECT raw_canonical FROM layer_revisions WHERE project_id=? AND rev_id=?`,
			projectID, rev).Scan(&raw)
		if err != nil {
			return nil, err
		}
		l, err := domain.ParseLayer(k, "", json.RawMessage(raw))
		if err != nil {
			// accepted revisions always parse; be defensive
			return nil, err
		}
		inputs.Layers[k] = l
		inputs.Revisions[k] = rev
	}
	return engine.Analyze(inputs), nil
}

// StateAt reconstructs the projected state at a given event sequence (0 = after
// creation). Returning to an old event restores the dependencies and visible
// conclusions of that moment; later computation never overwrites history.
func (s *Service) StateAt(ctx context.Context, projectID string, seq int64) (*State, error) {
	return s.replayAt(ctx, s.st.DB, projectID, seq)
}

// CurrentState projects the complete event log.
func (s *Service) CurrentState(ctx context.Context, projectID string) (*State, error) {
	var maxSeq int64
	if err := s.st.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, projectID).Scan(&maxSeq); err != nil {
		return nil, err
	}
	return s.replayAt(ctx, s.st.DB, projectID, maxSeq)
}

func (s *Service) replayAt(ctx context.Context, q queryable, projectID string, atSeq int64) (*State, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT seq,type,payload FROM events WHERE project_id=? AND seq<=? ORDER BY seq ASC`, projectID, atSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var st *State
	// artifact contents are loaded lazily by id from the artifacts table.
	artifactSeen := map[string]bool{}
	for rows.Next() {
		var seq int64
		var typ, payload string
		if err := rows.Scan(&seq, &typ, &payload); err != nil {
			return nil, err
		}
		switch EventType(typ) {
		case EvProjectCreated:
			var p CreatedPayload
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return nil, err
			}
			st = newState(projectID, p.Name, p.Description)
		case EvLayerCommitted:
			var p LayerCommitInfo
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return nil, err
			}
			parent := st.HeadRev[p.Kind]
			st.RevParent[p.RevID] = parent
			st.RevKind[p.RevID] = p.Kind
			st.HeadRev[p.Kind] = p.RevID
		case EvImportSubmitted:
			// layer_committed children already applied; nothing else needed
		case EvParamsRevised:
			var p ParamsPayload
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return nil, err
			}
			st.Params = p.Params
			st.ParamsRevID = p.ParamsRevID
			st.ParamsParent = p.ParentRevID
		case EvDecisionRecorded:
			var p DecisionPayload
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return nil, err
			}
			d := &Decision{ID: p.DecisionID, Seq: seq, ArtifactID: p.ArtifactID, TargetID: p.TargetID,
				Kind: p.Kind, Value: p.Value, Rationale: p.Rationale, Supersedes: p.Supersedes, ConflictOf: p.ConflictOf}
			st.DecisionLog = append(st.DecisionLog, d)
			if p.Supersedes != "" {
				// keep the superseded decision out of the live map, but retain it in log
			}
			st.Decisions[p.TargetID] = d
		case EvDecisionUndone:
			var p UndoPayload
			if err := json.Unmarshal([]byte(payload), &p); err != nil {
				return nil, err
			}
			for _, d := range st.DecisionLog {
				if d.ID == p.DecisionID {
					d.Undone = true
					if cur := st.Decisions[d.TargetID]; cur != nil && cur.ID == d.ID {
						delete(st.Decisions, d.TargetID)
					}
				}
			}
		}
		st.Seq = seq

		// load any artifact cached at this seq into the projection
		idsRows, err := q.QueryContext(ctx,
			`SELECT artifact_id FROM artifacts WHERE project_id=? AND basis_seq=?`, projectID, seq)
		if err != nil {
			return nil, err
		}
		for idsRows.Next() {
			var aid string
			_ = idsRows.Scan(&aid)
			if !artifactSeen[aid] {
				artifactSeen[aid] = true
			}
		}
		idsRows.Close()
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if st == nil {
		st = newState(projectID, "", "")
	}
	// populate artifacts from table up to the cutoff
	arows, err := q.QueryContext(ctx,
		`SELECT artifact_id,content,basis_seq FROM artifacts WHERE project_id=? AND basis_seq<=? ORDER BY basis_seq ASC`,
		projectID, atSeq)
	if err != nil {
		return nil, err
	}
	defer arows.Close()
	for arows.Next() {
		var aid, content string
		var bseq int64
		if err := arows.Scan(&aid, &content, &bseq); err != nil {
			return nil, err
		}
		var art engine.Artifact
		if err := json.Unmarshal([]byte(content), &art); err != nil {
			return nil, err
		}
		st.Artifacts[aid] = &art
		st.ArtifactOrder = append(st.ArtifactOrder, aid)
		st.BasisSeq[aid] = bseq
	}
	return st, nil
}

type queryable interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// EnsureEngineIDUsed keeps ids imported (documentation helper).
var _ = ids.Of
