package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"tga/internal/domain"
	"tga/internal/pipeline"
	"tga/internal/store"
)

var (
	ErrNotFound = errors.New("project not found")
	ErrNoSource = errors.New("no committed source data")
)

// Clock is injected so tests use virtual time.
type Clock func() string

type Service struct {
	Store *store.Store
	Now   Clock
	Actor string
}

func New(st *store.Store, now Clock) *Service {
	if now == nil {
		now = func() string { return "" }
	}
	return &Service{Store: st, Now: now, Actor: "reviewer"}
}

// CreateProject records project.created. The id is supplied (or derived) so it
// survives export/import unchanged.
func (s *Service) CreateProject(ctx context.Context, id, name string) error {
	if id == "" {
		id = store.SHAID("proj", name, s.Now())
	}
	if name == "" {
		name = "TGA project"
	}
	payload := map[string]any{"name": name}
	if _, _, _, err := s.Store.AppendEvent(ctx, id, domain.EvtProjectCreated, s.Actor, payload, s.Now()); err != nil {
		return err
	}
	return s.Store.CreateProject(ctx, id, name, s.Now())
}

// ImportResult describes what a commit attempt produced.
type ImportResult struct {
	Version   int    `json:"version"`
	Status    string `json:"status"`
	Partial   bool   `json:"partial"`
	Reason    string `json:"reason,omitempty"`
	DiagCount int    `json:"diag_count"`
	EventID   string `json:"event_id"`
	Seq       int64  `json:"seq"`
}

// CommitSource parses raw text and appends a new immutable source version.
//
//   - hard parse failure (no sample rows) -> a rejected event is still written,
//     nothing is derived: this is the recorded "import half-failure".
//   - row/unit problems -> partial commit: good layers are kept, diagnostics are
//     part of the version.
//   - clean -> committed.
//
// Original text is never overwritten; each version links to its parent.
func (s *Service) CommitSource(ctx context.Context, projectID, rawText string) (*ImportResult, error) {
	st, err := s.load(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	parent := 0
	if len(st.Sources) > 0 {
		parent = st.Sources[len(st.Sources)-1].Version
	}
	version := parent + 1
	layers, perr := pipeline.ImportRaw(rawText)
	sc := SourceCommit{Version: version, Parent: parent, RefIDs: map[string]string{}}
	if perr != nil {
		sc.Status = "rejected"
		sc.Partial = false
		sc.Reason = perr.Error()
	} else {
		sc.Layers = layers
		sc.Partial = len(layers.Diags) > 0 || hasInvalidRows(layers)
		if sc.Partial {
			sc.Status = "partial"
		} else {
			sc.Status = "committed"
		}
	}
	sc.RefIDs["raw"] = store.SHAID("rawsrc", projectID, version, rawText)
	id, seq, err := s.append(ctx, projectID, domain.EvtSourceCommitted, sc)
	if err != nil {
		return nil, err
	}
	res := &ImportResult{Version: version, Status: sc.Status, Partial: sc.Partial,
		Reason: sc.Reason, EventID: id, Seq: seq}
	if layers != nil {
		res.DiagCount = len(layers.Diags)
	}
	return res, nil
}

func hasInvalidRows(l *domain.ImportedLayers) bool {
	for _, sp := range l.Samples {
		if !sp.Kept {
			return true
		}
	}
	for _, g := range l.Gas {
		if !g.Time.Valid {
			return true
		}
	}
	return false
}

// ReviseParams appends a new parameter version (revision, not overwrite).
func (s *Service) ReviseParams(ctx context.Context, projectID string, p domain.AnalysisParams, reason string) (int, string, int64, error) {
	st, err := s.load(ctx, projectID, 0)
	if err != nil {
		return 0, "", 0, err
	}
	parent := 0
	if len(st.Params) > 0 {
		parent = st.Params[len(st.Params)-1].Version
	}
	pc := ParamsCommit{Version: parent + 1, Parent: parent, Params: p, Reason: reason}
	id, seq, err := s.append(ctx, projectID, domain.EvtParamsRevised, pc)
	return pc.Version, id, seq, err
}

// RunResult is returned by RunAnalysis.
type RunResult struct {
	AnalysisID string  `json:"analysis_id"`
	Stale      bool    `json:"stale"`
	Sources    []int   `json:"newer_source_versions,omitempty"`
	Params     []int   `json:"newer_param_versions,omitempty"`
	Seq        int64   `json:"seq"`
	Analysis   *pipeline.Analysis
}

// RunAnalysis computes a content-addressed analysis for the current source and
// parameter version and records analysis.run. Re-running identical inputs
// returns the same analysis id.
func (s *Service) RunAnalysis(ctx context.Context, projectID string) (*RunResult, error) {
	st, err := s.load(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	src := currentSource(st)
	if src == nil || src.Layers == nil {
		return nil, ErrNoSource
	}
	params := pipeline.DefaultParams()
	pv := 0
	if pc := currentParams(st); pc != nil {
		params = pc.Params
		pv = pc.Version
	}
	an := pipeline.Run(src.Layers, params, src.Version, pv)
	// persist artifact + refs (idempotent by content id)
	refs := map[string]string{
		"source": store.SHAID("rawsrc", projectID, src.Version),
		"params": store.SHAID("pfx", projectID, pv, an.ParamFingerprint),
	}
	if err := s.Store.PutArtifact(ctx, projectID, an.AnalysisID, "analysis", st.Seq+1, an, refs); err != nil {
		return nil, err
	}
	ref := AnalysisRef{AnalysisID: an.AnalysisID, SourceV: src.Version, ParamsV: pv}
	_, seq, err := s.append(ctx, projectID, domain.EvtAnalysisRun, ref)
	if err != nil {
		return nil, err
	}
	return &RunResult{AnalysisID: an.AnalysisID, Seq: seq, Analysis: an}, nil
}

// Decide records accept/reject/adjust. Accepting a candidate in a multi-
// candidate group while another accept is active creates a recorded conflict;
// the earlier decision is superseded (still visible) unless keepBoth is set.
type DecideRequest struct {
	AnalysisID  string      `json:"analysis_id"`
	CandidateID string      `json:"candidate_id"`
	Action      string      `json:"action"`
	Note        string      `json:"note"`
	Adjustment  *Adjustment `json:"adjustment,omitempty"`
	KeepBoth    bool        `json:"keep_both"`
}

type DecideResult struct {
	DecisionID string `json:"decision_id"`
	Conflict   *Conflict `json:"conflict,omitempty"`
	NewAnalysisID string `json:"new_analysis_id,omitempty"`
	Seq        int64  `json:"seq"`
}

func (s *Service) Decide(ctx context.Context, projectID string, req DecideRequest) (*DecideResult, error) {
	if req.Action != "accept" && req.Action != "reject" && req.Action != "adjust" {
		return nil, fmt.Errorf("unknown action %q", req.Action)
	}
	st, err := s.load(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	an, err := s.analysisOf(ctx, projectID, req.AnalysisID)
	if err != nil {
		return nil, err
	}
	if req.CandidateID != "" && !candidateExists(an, req.CandidateID) {
		return nil, fmt.Errorf("candidate %s not found", req.CandidateID)
	}
	res := &DecideResult{}
	d := Decision{
		ID: store.SHAID("dec", projectID, req.AnalysisID, req.CandidateID, req.Action, len(st.Decisions), req.Note),
		Candidate: req.CandidateID, AnalysisID: req.AnalysisID,
		Action: req.Action, Note: req.Note, Active: true,
	}
	if req.Adjustment != nil {
		body, _ := json.Marshal(req.Adjustment)
		d.ID = store.SHAID("dec", projectID, req.AnalysisID, string(body), len(st.Decisions))
		if err := s.Store.PutArtifact(ctx, projectID, "adj_"+d.ID, "adjustment", st.Seq+1, req.Adjustment,
			map[string]string{"analysis": req.AnalysisID, "candidate": req.CandidateID}); err != nil {
			return nil, err
		}
	}
	// conflict detection on accept of a different competing candidate
	if req.Action == "accept" && req.CandidateID != "" {
		accepts := st.ActiveAccepts(req.AnalysisID)
		for _, old := range accepts {
			if old.Candidate != req.CandidateID {
				if !req.KeepBoth {
					d.Supersedes = old.ID
				}
				c := Conflict{
					Earlier: old.ID, Later: d.ID, Candidate: req.AnalysisID,
					Resolution: "superseded",
				}
				if req.KeepBoth {
					c.Resolution = "flagged"
				}
				if _, cseq, err := s.append(ctx, projectID, domain.EvtConflictResolved, c); err != nil {
					return nil, err
				} else {
					c.Seq = cseq
				}
				res.Conflict = &c
			}
		}
	}
	_, seq, err := s.append(ctx, projectID, domain.EvtDecisionMade, d)
	if err != nil {
		return nil, err
	}
	res.DecisionID = d.ID
	res.Seq = seq
	return res, nil
}

// UndoDecision writes decision.undone; the superseded decision reactivates.
func (s *Service) UndoDecision(ctx context.Context, projectID, decisionID, note string) (string, int64, error) {
	st, err := s.load(ctx, projectID, 0)
	if err != nil {
		return "", 0, err
	}
	var target *Decision
	for i := range st.Decisions {
		if st.Decisions[i].ID == decisionID {
			target = &st.Decisions[i]
		}
	}
	if target == nil {
		return "", 0, fmt.Errorf("decision %s not found", decisionID)
	}
	undo := Decision{
		ID: store.SHAID("undo", projectID, decisionID, len(st.Decisions), note),
		Candidate: target.Candidate, AnalysisID: target.AnalysisID,
		Action: "undo:" + target.Action, Note: note, UndoOf: target.ID, Active: false,
	}
	id, seq, err := s.append(ctx, projectID, domain.EvtDecisionUndone, undo)
	return id, seq, err
}

func candidateExists(an *pipeline.Analysis, id string) bool {
	for _, c := range an.Candidates {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (s *Service) analysisOf(ctx context.Context, projectID, analysisID string) (*pipeline.Analysis, error) {
	var an pipeline.Analysis
	ok, err := s.Store.Artifact(ctx, projectID, analysisID, &an)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("analysis %s not found", analysisID)
	}
	return &an, nil
}

// DeleteProject records project.deleted (the journal remains for audit) and hides it.
func (s *Service) DeleteProject(ctx context.Context, projectID string) (string, int64, error) {
	id, seq, err := s.append(ctx, projectID, domain.EvtProjectDeleted, map[string]string{})
	if err == nil {
		_ = s.Store.DeleteProject(ctx, projectID)
	}
	return id, seq, err
}

func (s *Service) append(ctx context.Context, projectID, kind string, payload any) (string, int64, error) {
	id, seq, _, err := s.Store.AppendEvent(ctx, projectID, kind, s.Actor, payload, s.Now())
	return id, seq, err
}

func (s *Service) load(ctx context.Context, projectID string, upTo int64) (*State, error) {
	exists, err := s.Store.ProjectExists(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, ErrNotFound
	}
	evs, err := s.Store.Events(ctx, projectID, upTo)
	if err != nil {
		return nil, err
	}
	return Materialise(evs, upTo)
}
