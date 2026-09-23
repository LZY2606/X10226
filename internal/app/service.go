package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"tga-review/internal/calc"
	"tga-review/internal/domain"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

// Service is the review state machine over the append-only SQLite store.
type Service struct {
	st    *store.Store
	clock Clock
}

func New(st *store.Store, clock Clock) *Service {
	if clock == nil {
		clock = SystemClock{}
	}
	return &Service{st: st, clock: clock}
}

func (s *Service) now() string { return s.clock.Now().UTC().Format(time.RFC3339Nano) }
func (s *Service) nowT() time.Time { return s.clock.Now() }

// ---- project ----

func (s *Service) CreateProject(ctx context.Context, name, description string) (*domain.Project, error) {
	created := s.now()
	p := domain.Project{
		ID:          domain.ContentID(domain.KindProject, map[string]any{"name": name, "description": description, "created_at": created}),
		Name:        name,
		Description: description,
		CreatedAt:   created,
	}
	if err := s.st.CreateProject(ctx, p); err != nil {
		return nil, err
	}
	def := domain.DefaultParams()
	if err := s.st.InsertParams(ctx, p.ID, 1, def.Fingerprint(), "", def, created); err != nil {
		return nil, err
	}
	if err := s.st.AppendEvent(ctx, domain.Event{
		ProjectID: p.ID, Seq: 1, ID: domain.ContentID(domain.KindEvent, map[string]any{"p": p.ID, "seq": 1, "type": domain.EvtProjectCreated}),
		Type: domain.EvtProjectCreated, Payload: map[string]any{"name": name, "description": description}, CreatedAt: created,
	}); err != nil {
		return nil, err
	}
	return &p, nil
}

// ---- import ----

// ImportFragment is one submitted layer text fragment.
type ImportFragment struct {
	Layer  domain.SourceLayer `json:"layer"`
	Format string             `json:"format"`
	Raw    string             `json:"raw"`
}

// ImportResult reports per-fragment disposition and the resulting session.
type ImportResult struct {
	SessionID string             `json:"session_id"`
	Status    string             `json:"status"`
	Fragments []domain.Fragment  `json:"fragments"`
	Derived   []string           `json:"derived_node_ids"`
}

// Import commits valid fragments as immutable source revisions. Rejected
// fragments (fatal format errors) are still retained on the session so a
// half-failed import leaves an input trail (R7).
func (s *Service) Import(ctx context.Context, projectID string, frags []ImportFragment) (*ImportResult, error) {
	if len(frags) == 0 {
		return nil, fmt.Errorf("no fragments")
	}
	sesSeq, err := s.st.NextCounter(ctx, projectID, "session")
	if err != nil {
		return nil, err
	}
	now := s.now()
	sesID := domain.SeqID(domain.KindSession, projectID, sesSeq)
	dfrags := make([]domain.Fragment, 0, len(frags))
	acceptedAny, rejectedAny := false, false
	var newLayers []domain.SourceLayer

	for _, f := range frags {
		if !f.Layer.Valid() {
			dfrags = append(dfrags, domain.Fragment{Layer: f.Layer, Format: f.Format, Raw: f.Raw, Accepted: false,
				Diag: []domain.Diag{{Code: "E_LAYER", Severity: "error", Message: "unknown layer"}}})
			rejectedAny = true
			continue
		}
		res := ingest.Parse(f.Layer, f.Format, f.Raw)
		if res.Fatal {
			dfrags = append(dfrags, domain.Fragment{Layer: f.Layer, Format: f.Format, Raw: f.Raw, Accepted: false, Diag: res.Diag})
			rejectedAny = true
			continue
		}
		// commit as new revision
		prev, _ := s.st.CurrentSource(ctx, projectID, f.Layer)
		version := 1
		parent := ""
		if prev != nil {
			version = prev.Version + 1
			parent = prev.ID
		}
		src := domain.Source{
			ProjectID: projectID, Layer: f.Layer, Version: version, ParentID: parent,
			Format: normalizeFormat(f.Format), Raw: f.Raw, Payload: res.Payload, Diag: res.Diag,
			Accepted: true, CreatedAt: now,
		}
		src.ID = domain.ContentID(domain.KindSource, map[string]any{
			"project": projectID, "layer": string(f.Layer), "version": version,
			"parent": parent, "raw": f.Raw, "payload_fingerprint": domain.Fingerprint(res.Payload),
		})
		if err := s.st.InsertSource(ctx, src); err != nil {
			return nil, err
		}
		dfrags = append(dfrags, domain.Fragment{Layer: f.Layer, Format: f.Format, Raw: f.Raw, Accepted: true, SourceID: src.ID, Diag: res.Diag})
		acceptedAny = true
		newLayers = append(newLayers, f.Layer)
	}

	status := "committed"
	if acceptedAny && rejectedAny {
		status = "partial"
	} else if !acceptedAny {
		status = "failed"
	}
	ses := domain.Session{ID: sesID, ProjectID: projectID, Status: status, Fragments: dfrags, Submitted: now}
	if err := s.st.InsertSession(ctx, ses); err != nil {
		return nil, err
	}
	seq, err := s.nextEventSeq(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if err := s.st.AppendEvent(ctx, domain.Event{
		ProjectID: projectID, Seq: seq,
		ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": seq, "type": domain.EvtImport, "ses": sesID}),
		Type: domain.EvtImport, CreatedAt: now,
		Payload: map[string]any{"session_id": sesID, "status": status, "fragments": dfrags, "new_layers": newLayers},
	}); err != nil {
		return nil, err
	}

	out := &ImportResult{SessionID: sesID, Status: status, Fragments: dfrags}
	if acceptedAny {
		derived, _, err := s.recompute(ctx, projectID, "import:"+sesID)
		if err != nil {
			return nil, err
		}
		for _, n := range derived {
			out.Derived = append(out.Derived, n.ID)
		}
	}
	return out, nil
}

func normalizeFormat(f string) string {
	if f == "" {
		return "csv"
	}
	return f
}

func (s *Service) nextEventSeq(ctx context.Context, projectID string) (int, error) {
	n, err := s.st.EventCount(ctx, projectID)
	if err != nil {
		return 0, err
	}
	return n + 1, nil
}

// ---- recompute ----

func (s *Service) loadInputs(ctx context.Context, projectID string, params domain.Params) (calc.Inputs, map[domain.SourceLayer]*domain.Source, error) {
	in := calc.Inputs{Params: params}
	cur := map[domain.SourceLayer]*domain.Source{}
	get := func(l domain.SourceLayer) (*domain.Source, error) {
		return s.st.CurrentSource(ctx, projectID, l)
	}
	var err error
	if in.Samples, err = get(domain.LayerSamples); err != nil {
		return in, nil, err
	}
	if in.Drift, err = get(domain.LayerDrift); err != nil {
		return in, nil, err
	}
	if in.Furnace, err = get(domain.LayerFurnace); err != nil {
		return in, nil, err
	}
	if in.Valves, err = get(domain.LayerValves); err != nil {
		return in, nil, err
	}
	if in.Blank, err = get(domain.LayerBlank); err != nil {
		return in, nil, err
	}
	if in.InitMass, err = get(domain.LayerInitMass); err != nil {
		return in, nil, err
	}
	cur[domain.LayerSamples] = in.Samples
	cur[domain.LayerDrift] = in.Drift
	cur[domain.LayerFurnace] = in.Furnace
	cur[domain.LayerValves] = in.Valves
	cur[domain.LayerBlank] = in.Blank
	cur[domain.LayerInitMass] = in.InitMass
	return in, cur, nil
}

// recompute runs the pipeline against current inputs, expires only the nodes
// that changed (content ids compared), and appends a derived event.
func (s *Service) recompute(ctx context.Context, projectID, reason string) ([]domain.Node, []domain.RuleEvent, error) {
	params, _, err := s.st.CurrentParams(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	in, _, err := s.loadInputs(ctx, projectID, params)
	if err != nil {
		return nil, nil, err
	}
	nowT := s.nowT()
	res := calc.Run(in, projectID, nowT)

	// determine accepted stage keys from LIVE decisions at this moment
	accepted := map[string]bool{}
	decs, err := s.st.AllDecisions(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	confirmed := false
	for _, d := range decs {
		if !d.Live {
			continue
		}
		switch d.Kind {
		case domain.DecisionAcceptStage:
			accepted[d.Target] = true
		case domain.DecisionRejectStage:
			delete(accepted, d.Target)
		case domain.DecisionConfirmResidue:
			confirmed = true
		}
	}
	var corr calc.Corrected
	var stages calc.Stages
	for i := range res.Nodes {
		switch res.Nodes[i].Kind {
		case domain.NodeCorrectedSeries:
			unmarshalNode(res.Nodes[i].Payload["corrected"], &corr)
		case domain.NodeStages:
			unmarshalNode(res.Nodes[i].Payload["stages"], &stages)
		}
	}
	var stageNodeID string
	for i := range res.Nodes {
		if res.Nodes[i].Kind == domain.NodeStages {
			stageNodeID = res.Nodes[i].ID
		}
	}
	if stageNodeID != "" {
		rn := calc.ResidueStep(in, projectID, stageNodeID, stages, corr, accepted, confirmed, nowT.Add(time.Millisecond))
		res.Nodes = append(res.Nodes, rn)
	}

	seq, err := s.nextEventSeq(ctx, projectID)
	if err != nil {
		return nil, nil, err
	}
	now := s.now()
	kindToNew := map[domain.NodeKind]domain.Node{}
	for _, n := range res.Nodes {
		kindToNew[n.Kind] = n
	}
	var inserted []domain.Node
	for kind, n := range kindToNew {
		existing, _ := s.st.CurrentNode(ctx, projectID, kind)
		if existing != nil && existing.ID == n.ID {
			continue
		}
		// expire the previous current node first; stale nodes stay readable
		if existing != nil {
			if err := s.st.SupersedeNode(ctx, existing.ID, n.ID); err != nil {
				return nil, nil, err
			}
		}
		n.CreatedEventSeq = seq
		if err := s.st.InsertNode(ctx, n, seq); err != nil {
			return nil, nil, err
		}
		inserted = append(inserted, n)
	}
	sort.Slice(inserted, func(i, j int) bool { return string(inserted[i].Kind) < string(inserted[j].Kind) })

	ids := make([]string, len(inserted))
	for i, n := range inserted {
		ids[i] = n.ID
	}
	if len(ids) > 0 {
		if err := s.st.AppendEvent(ctx, domain.Event{
			ProjectID: projectID, Seq: seq,
			ID: domain.ContentID(domain.KindEvent, map[string]any{"p": projectID, "seq": seq, "type": domain.EvtDerived, "ids": ids}),
			Type: domain.EvtDerived, CreatedAt: now,
			Payload: map[string]any{"reason": reason, "node_ids": ids, "rules": res.Rules, "param_fp": params.Fingerprint()},
		}); err != nil {
			return nil, nil, err
		}
	}
	return inserted, res.Rules, nil
}

func unmarshalNode(v any, out any) {
	b, _ := json.Marshal(v)
	_ = json.Unmarshal(b, out)
}
