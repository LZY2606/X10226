package tga

import (
	"context"
	"encoding/json"
	"fmt"
)

// Clock abstracts time so tests run on virtual fixed time.
type Clock interface{ NowMS() int64 }

// Store is the persistence boundary.
type Store interface {
	CreateProject(ctx context.Context, p Project) error
	GetProject(ctx context.Context, id string) (Project, error)
	ListProjects(ctx context.Context) ([]Project, error)
	AppendEvent(ctx context.Context, e Event) error
	Events(ctx context.Context, projectID string) ([]Event, error)
	MaxSeq(ctx context.Context, projectID string) (int64, error)
	UpsertNode(ctx context.Context, n NodeSnapshot) error
	NodeAt(ctx context.Context, projectID, nodeID string, atSeq int64) (NodeSnapshot, bool, error)
	CurrentNode(ctx context.Context, projectID string) (NodeSnapshot, bool, error)
	AllNodeIDs(ctx context.Context, projectID string) ([]string, error)
	BulkNodes(ctx context.Context, projectID string, ids []string) (map[string]NodeSnapshot, error)
}

// Project is the top-level review case.
type Project struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Note      string `json:"note,omitempty"`
	CreatedAt int64  `json:"created_at_ms"`
	HeadSeq   int64  `json:"head_seq"`
}

// NodeSnapshot stores one derived report version together with the log seq up
// to which it is current.
type NodeSnapshot struct {
	ProjectID  string `json:"project_id"`
	NodeID     string `json:"node_id"`
	Current    bool   `json:"current"`
	BuiltAtSeq int64  `json:"built_at_seq"`
	ReportJSON []byte `json:"report_json"`
}

// Engine executes commands against a store and rebuilds projections.
type Engine struct {
	store Store
	clock Clock
}

func NewEngine(store Store, clock Clock) *Engine {
	return &Engine{store: store, clock: clock}
}

// ProjectView is the replayed state used by the UI and API.
type ProjectView struct {
	Project    Project             `json:"project"`
	Batches    []*Batch            `json:"batches"`
	Params     []ParamVersion      `json:"param_versions"`
	Decisions  []Decision          `json:"decisions"`
	Conflicts  []Conflict          `json:"conflicts"`
	Rejections []RejectedImport    `json:"rejections"`
	Current    *Report             `json:"current,omitempty"`
	AtSeq      int64               `json:"at_seq"`
}

// ImpactedObject pairs a rejected/changed input fragment with the derived
// objects it affects.
type ImpactedObject struct {
	NodeID   string   `json:"node_id"`
	ObjectID string   `json:"object_id"`
	Kind     string   `json:"kind"`
	Reasons  []string `json:"reasons,omitempty"`
}

func (e *Engine) append(ctx context.Context, projectID, kind string, payload any) (Event, error) {
	seq, err := e.store.MaxSeq(ctx, projectID)
	if err != nil {
		return Event{}, err
	}
	seq++
	body := CanonicalJSON(payload)
	ev := Event{
		ID:        HashID("ev", map[string]any{"p": projectID, "seq": seq, "kind": kind, "body": string(body)}),
		ProjectID: projectID, Seq: seq, Kind: kind,
		AtMS: e.clock.NowMS(), Payload: body,
	}
	if err := e.store.AppendEvent(ctx, ev); err != nil {
		return Event{}, err
	}
	return ev, nil
}

// CreateProject opens a new review case.
func (e *Engine) CreateProject(ctx context.Context, name, note string) (Project, error) {
	seq := int64(0)
	id := HashID("proj", map[string]any{"name": name, "at": e.clock.NowMS(), "nonce": seq})
	p := Project{ID: id, Name: name, Note: note, CreatedAt: e.clock.NowMS()}
	if err := e.store.CreateProject(ctx, p); err != nil {
		return Project{}, err
	}
	if _, err := e.append(ctx, id, EvProjectCreated, map[string]any{"id": id, "name": name, "note": note}); err != nil {
		return Project{}, err
	}
	return p, nil
}

// CommitBatch submits one stratified source layer. Raw input is immutable:
// revisions must point at the superseded batch.
type CommitBatchInput struct {
	Layer      string              `json:"layer"`
	Parent     string              `json:"parent,omitempty"`
	Supersedes string              `json:"supersedes,omitempty"`
	Origin     string              `json:"origin"`
	Note       string              `json:"note,omitempty"`
	Items      []CommitItem        `json:"items"`
}

// CommitItem is one raw record with preserved raw text per field.
type CommitItem struct {
	Kind string            `json:"kind"`
	Raw  map[string]string `json:"raw"`
}

// CommitResult reports the committed batch plus per-item diagnostics.
type CommitResult struct {
	Batch    *Batch           `json:"batch"`
	Partial  bool             `json:"partial"`
	Diags    []Diagnostic     `json:"diags"`
	Impacted []ImpactedObject `json:"impacted,omitempty"`
}

func (e *Engine) CommitBatch(ctx context.Context, projectID string, in CommitBatchInput) (CommitResult, error) {
	if !validLayer(in.Layer) {
		return CommitResult{}, fmt.Errorf("未知分层 %q", in.Layer)
	}
	seq, err := e.store.MaxSeq(ctx, projectID)
	if err != nil {
		return CommitResult{}, err
	}
	batch := &Batch{
		ID:         HashID("batch", map[string]any{"p": projectID, "seq": seq + 1, "layer": in.Layer}),
		ProjectID:  projectID,
		Seq:        seq + 1,
		Layer:      in.Layer,
		Parent:     in.Parent,
		Supersedes: in.Supersedes,
		Origin:     defaultStr(in.Origin, "text"),
		Note:       in.Note,
		CommittedAtMS: e.clock.NowMS(),
	}
	allInvalid := in.Layer != LayerGas && in.Layer != LayerProgram
	allInvalid = false
	var diags []Diagnostic
	for i, ci := range in.Items {
		item := RawItem{
			ID:      HashID("item", map[string]any{"batch": batch.ID, "idx": i, "raw": ci.Raw}),
			BatchID: batch.ID, Idx: i, Kind: ci.Kind, Raw: ci.Raw, Valid: true,
		}
		item.Valid, item.Diags = validateItem(in.Layer, ci)
		if !item.Valid {
			diags = append(diags, item.Diags...)
		}
		batch.Items = append(batch.Items, item)
	}
	partial := false
	if len(diags) > 0 {
		partial = true
		batch.Kind = "partial"
	} else {
		batch.Kind = "layer"
	}
	payload := map[string]any{"batch": batch}
	if _, err := e.append(ctx, projectID, EvBatchCommitted, payload); err != nil {
		return CommitResult{}, err
	}
	res := CommitResult{Batch: batch, Partial: partial, Diags: diags}
	if partial {
		// record the rejected/partial attempt as its own event too
		rej := RejectedImport{
			ID: HashID("rej", map[string]any{"batch": batch.ID}),
			Seq: batch.Seq, AtMS: batch.CommittedAtMS,
			Origin: batch.Origin, Status: "partial",
			RequestedLayer: in.Layer, Diags: diags,
			AcceptedBatchID: batch.ID,
			Fragment: summarizeItems(in.Items),
		}
		if _, err := e.append(ctx, projectID, EvImportRejected, rej); err != nil {
			return CommitResult{}, err
		}
	}
	impacted, err := e.rebuild(ctx, projectID)
	if err != nil {
		return CommitResult{}, err
	}
	res.Impacted = impacted
	return res, nil
}

// RejectImport records an outright failed import (nothing committed) while
// retaining the input fragment and diagnostics.
func (e *Engine) RejectImport(ctx context.Context, projectID, origin, layer, fragment string, diags []Diagnostic) (RejectedImport, error) {
	seq, err := e.store.MaxSeq(ctx, projectID)
	if err != nil {
		return RejectedImport{}, err
	}
	rej := RejectedImport{
		ID: HashID("rej", map[string]any{"p": projectID, "seq": seq + 1, "fragment": fragment}),
		Seq: seq + 1, AtMS: e.clock.NowMS(),
		Origin: origin, Status: "rejected", RequestedLayer: layer,
		Diags: diags, Fragment: fragment,
	}
	if _, err := e.append(ctx, projectID, EvImportRejected, rej); err != nil {
		return RejectedImport{}, err
	}
	return rej, nil
}

// ReviseParams commits a new parameter version linked to its parent.
func (e *Engine) ReviseParams(ctx context.Context, projectID, note string, params Params) (ParamVersion, []ImpactedObject, error) {
	view, err := e.replay(ctx, projectID, 0)
	if err != nil {
		return ParamVersion{}, nil, err
	}
	seq := view.Project.HeadSeq + 1
	parent := ""
	if len(view.Params) > 0 {
		parent = view.Params[len(view.Params)-1].ID
	}
	if params.AlgorithmVersion == "" {
		params = DefaultParams()
	}
	pv := ParamVersion{
		ID: HashID("params", map[string]any{"p": projectID, "seq": seq, "p": string(CanonicalJSON(params))}),
		Seq: seq, Parent: parent, Note: note,
		CommittedAtMS: e.clock.NowMS(), Params: params,
	}
	if _, err := e.append(ctx, projectID, EvParamsCommitted, pv); err != nil {
		return ParamVersion{}, nil, err
	}
	impacted, err := e.rebuild(ctx, projectID)
	return pv, impacted, err
}

// RecordDecision applies a human accept/reject/adjust decision.
type DecisionInput struct {
	SubjectKind  string  `json:"subject_kind"`
	SubjectID    string  `json:"subject_id"`
	Verdict      string  `json:"verdict"`
	Comment      string  `json:"comment,omitempty"`
	StartS       float64 `json:"start_s,omitempty"`
	EndS         float64 `json:"end_s,omitempty"`
	AttributedS  string  `json:"attributed_switch_id,omitempty"`
}

func (e *Engine) RecordDecision(ctx context.Context, projectID string, in DecisionInput) (Decision, *Conflict, error) {
	view, err := e.replay(ctx, projectID, 0)
	if err != nil {
		return Decision{}, nil, err
	}
	seq := view.Project.HeadSeq + 1
	d := Decision{
		ID: HashID("dec", map[string]any{"p": projectID, "seq": seq, "subj": in.SubjectID, "v": in.Verdict}),
		Seq: seq, SubjectKind: in.SubjectKind, SubjectID: in.SubjectID,
		Verdict: in.Verdict, Comment: in.Comment,
		StartS: in.StartS, EndS: in.EndS, AttributedS: in.AttributedS,
		State: DecisionStateActive, CommittedAtMS: e.clock.NowMS(),
	}
	var conflict *Conflict
	for i := range view.Decisions {
		prev := &view.Decisions[i]
		if prev.State == DecisionStateActive && prev.SubjectKind == in.SubjectKind && prev.SubjectID == in.SubjectID {
			if prev.Verdict != in.Verdict || prev.StartS != in.StartS || prev.EndS != in.EndS {
				conflict = &Conflict{
					ID: HashID("conflict", map[string]any{"p": projectID, "a": prev.ID, "b": d.ID}),
					Seq: seq, SubjectKind: in.SubjectKind, SubjectID: in.SubjectID,
					FirstID: prev.ID, SecondID: d.ID, Open: true, AtMS: e.clock.NowMS(),
					Reason: fmt.Sprintf("对同一对象先后给出互相冲突的裁定：%s vs %s", prev.Verdict, in.Verdict),
				}
			}
			break
		}
	}
	if _, err := e.append(ctx, projectID, EvDecisionRecorded, d); err != nil {
		return Decision{}, nil, err
	}
	if conflict != nil {
		if _, err := e.append(ctx, projectID, EvConflictRecorded, conflict); err != nil {
			return Decision{}, nil, err
		}
	}
	if _, err := e.rebuild(ctx, projectID); err != nil {
		return Decision{}, nil, err
	}
	return d, conflict, nil
}

// ResolveConflict closes a conflict, keeping exactly one decision active.
func (e *Engine) ResolveConflict(ctx context.Context, projectID, conflictID, keep string) (Conflict, error) {
	view, err := e.replay(ctx, projectID, 0)
	if err != nil {
		return Conflict{}, err
	}
	var con *Conflict
	for i := range view.Conflicts {
		if view.Conflicts[i].ID == conflictID && view.Conflicts[i].Open {
			con = &view.Conflicts[i]
		}
	}
	if con == nil {
		return Conflict{}, fmt.Errorf("未找到开放冲突 %s", conflictID)
	}
	keepID, otherID := con.FirstID, con.SecondID
	resolution := "keep_first"
	if keep == VerdictAccepted || keep == con.SecondID {
		keepID, otherID = con.SecondID, con.FirstID
		if keep == con.SecondID || keep == VerdictAccepted {
			resolution = "keep_second"
		}
	}
	con.Open = false
	con.ResolvedBy = keepID
	con.Resolution = resolution
	if _, err := e.append(ctx, projectID, EvConflictResolved, con); err != nil {
		return Conflict{}, err
	}
	if _, err := e.append(ctx, projectID, EvDecisionRecorded, map[string]any{
		"id": otherID, "state": DecisionStateResolved, "superseded_by": keepID, "conflict": conflictID,
	}) {
		return Conflict{}, err
	}
	if _, err := e.rebuild(ctx, projectID); err != nil {
		return Conflict{}, err
	}
	return *con, nil
}

// UndoDecision marks an active decision undone (recorded, not deleted).
func (e *Engine) UndoDecision(ctx context.Context, projectID, decisionID, reason string) error {
	if _, err := e.append(ctx, projectID, EvDecisionUndone, map[string]any{
		"id": decisionID, "reason": reason,
	}); err != nil {
		return err
	}
	_, err := e.rebuild(ctx, projectID)
	return err
}

func validLayer(l string) bool {
	for _, x := range Layers {
		if x == l {
			return true
		}
	}
	return false
}

func defaultStr(a, b string) string {
	if a == "" {
		return b
	}
	return a
}

func summarizeItems(items []CommitItem) string {
	bs, _ := json.Marshal(items)
	s := string(bs)
	if len(s) > 2000 {
		return s[:2000] + "…(截断)"
	}
	return s
}
