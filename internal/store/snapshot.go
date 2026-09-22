package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"

	"tgaaudit/internal/domain"
	"tgaaudit/internal/pipeline"
)

// DerivationSnapshot is the read model served to the review UI: current raw
// batches, current parameters, all derived nodes (current and superseded),
// decisions, conflicts, rule events, attempts and history markers.
type DerivationSnapshot struct {
	ProjectID        string                          `json:"project_id"`
	ProjectName      string                          `json:"project_name"`
	Seed             int64                           `json:"seed"`
	AsOfSeq          int                             `json:"as_of_seq"`
	CurrentSetID     string                          `json:"current_set_id"`
	CurrentParams    domain.Params                   `json:"current_params"`
	ParamFingerprint string                          `json:"param_fingerprint"`
	Batches          []domain.Batch                  `json:"batches"`
	CurrentBatchIDs  map[string]string               `json:"current_batch_ids"`
	Nodes            []NodeView                      `json:"nodes"`
	RuleEvents       []domain.RuleEvent              `json:"rule_events"`
	Decisions        []domain.Decision               `json:"decisions"`
	Conflicts        []domain.Conflict               `json:"conflicts"`
	Undos            []domain.UndoRecord             `json:"undos"`
	Attempts         []domain.ImportAttempt          `json:"attempts"`
	Events           []domain.Event                  `json:"events"`
	Corrected        *pipeline.CorrectedData         `json:"corrected"`
	Dtg              *pipeline.DtgData               `json:"dtg"`
	Gaps             []pipeline.Gap                  `json:"gaps"`
	PeakSets         []pipeline.PeakSet              `json:"peak_sets"`
	Stages           map[string][]pipeline.Stage     `json:"stages"`
	Reactions        []pipeline.ReactionWindow       `json:"reactions"`
	Residues         map[string]pipeline.ResidueData `json:"residues"`
	Diagnostics      []string                        `json:"diagnostics,omitempty"`
}

// NodeView wraps a node with its current/superseded state and attached
// decision state.
type NodeView struct {
	domain.Node
	State    string           `json:"state"` // current | superseded
	Decision *domain.Decision `json:"decision,omitempty"`
}

func (s *Store) recompute(ctx context.Context, projectID string) (*DerivationSnapshot, error) {
	cur, err := s.currentBatches(ctx, projectID)
	if err != nil {
		return nil, err
	}
	params, err := s.currentParams(ctx, projectID)
	if err != nil {
		return nil, err
	}
	der := pipeline.Derive(cur, params)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Determine the previous derivation set. Only nodes whose direct
	// sources are absent from the new derivation are superseded; unchanged
	// dependencies keep their node identity and stay current.
	var prevNodes []domain.Node
	pr, err := tx.QueryContext(ctx,
		`SELECT payload FROM nodes WHERE project_id=? AND superseded_event_seq IS NULL`,
		projectID)
	if err != nil {
		return nil, err
	}
	for pr.Next() {
		var pj string
		if err := pr.Scan(&pj); err != nil {
			pr.Close()
			return nil, err
		}
		var n domain.Node
		if err := json.Unmarshal([]byte(pj), &n); err != nil {
			pr.Close()
			return nil, err
		}
		prevNodes = append(prevNodes, n)
	}
	pr.Close()

	newIDs := map[string]bool{}
	for _, n := range der.Nodes {
		newIDs[n.ID] = true
	}
	if err := pr.Err(); err != nil {
		return nil, err
	}
	for _, old := range prevNodes {
		if newIDs[old.ID] {
			continue
		}
		dependent := false
		for _, src := range old.Sources {
			if src.Kind == "node" && !newIDs[src.ID] {
				dependent = true
				break
			}
			if src.Kind == "params" && src.ID != pipeline.ParamFingerprint(params) {
				dependent = true
				break
			}
			if src.Kind == "batch" {
				if curID, ok := der.SourceBatchIDs[src.Role]; ok && curID != src.ID {
					dependent = true
					break
				}
			}
		}
		if dependent || old.Kind == domain.NodeCorrected || old.Kind == domain.NodeDtg {
			if _, err := tx.ExecContext(ctx,
				`UPDATE nodes SET superseded_event_seq=? WHERE project_id=? AND id=?`,
				0, projectID, old.ID); err != nil {
				return nil, err
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Record the derivation as a single derived event (set-level provenance);
	// individual nodes are projections of the embedded node payloads.
	derivedPayload := map[string]any{
		"set_id":            der.SetID,
		"param_fingerprint": pipeline.ParamFingerprint(params),
		"node_ids":          nodeIDs(der.Nodes),
		"source_batches":    der.SourceBatchIDs,
		"rule_events":       der.RuleEvents,
	}
	ev, err := s.appendEvent(ctx, projectID, domain.EvDerived, derivedPayload)
	if err != nil {
		return nil, err
	}
	for _, n := range der.Nodes {
		n.EventSeq = ev.Seq
		pj, _ := json.Marshal(n)
		if _, err := s.db.ExecContext(ctx,
			`INSERT INTO nodes(project_id,id,event_seq,set_id,kind,algo,param_fp,payload)
			 VALUES(?,?,?,?,?,?,?,?)
			 ON CONFLICT(project_id,id) DO NOTHING`,
			projectID, n.ID, ev.Seq, der.SetID, n.Kind, n.AlgorithmVersion,
			n.ParamFingerprint, string(pj)); err != nil {
			return nil, err
		}
	}
	for i, re := range der.RuleEvents {
		refs, _ := json.Marshal(re.Refs)
		if _, err := s.db.ExecContext(ctx,
			`INSERT OR REPLACE INTO rule_events(project_id,node_id,seq_in_set,
			  code,subject,severity,message,refs,set_id)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			projectID, der.SetID, i, re.Code, re.Subject, re.Severity,
			re.Message, string(refs), der.SetID); err != nil {
			return nil, err
		}
	}
	return s.Snapshot(ctx, projectID, 0)
}

func nodeIDs(ns []domain.Node) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		out = append(out, n.ID)
	}
	sort.Strings(out)
	return out
}

func (s *Store) currentBatches(ctx context.Context, projectID string) (pipeline.CurrentBatches, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT payload FROM batches WHERE project_id=?
		 AND version=(SELECT MAX(version) FROM batches b2
		   WHERE b2.project_id=batches.project_id AND b2.kind=batches.kind)
		 ORDER BY kind`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := pipeline.CurrentBatches{}
	for rows.Next() {
		var pj string
		if err := rows.Scan(&pj); err != nil {
			return nil, err
		}
		var bat domain.Batch
		if err := json.Unmarshal([]byte(pj), &bat); err != nil {
			return nil, err
		}
		out[bat.Kind] = &bat
	}
	return out, rows.Err()
}

func (s *Store) currentParams(ctx context.Context, projectID string) (domain.Params, error) {
	var pj string
	err := s.db.QueryRowContext(ctx,
		`SELECT payload FROM params_versions WHERE project_id=?
		 ORDER BY version_no DESC LIMIT 1`, projectID).Scan(&pj)
	if err == sql.ErrNoRows {
		return domain.DefaultParams(), nil
	}
	if err != nil {
		return domain.Params{}, err
	}
	var p domain.Params
	if err := json.Unmarshal([]byte(pj), &p); err != nil {
		return domain.Params{}, err
	}
	if p.AlgorithmVersion == "" {
		p.AlgorithmVersion = domain.CurrentParamsVersion
	}
	return p, nil
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

var _ = fmt.Sprintf
