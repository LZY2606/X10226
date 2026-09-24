package engine

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"tgaline/internal/domain"
)

// ParamsVersion is one immutable named parameter set.
type ParamsVersion struct {
	Name       string             `json:"name"`
	Params     map[string]float64 `json:"params"`
	ParentName string             `json:"parent_name,omitempty"`
	Reason     string             `json:"reason,omitempty"`
	FP         string             `json:"fp"`
	EventSeq   int64              `json:"event_seq"`
}

// Project is the replayable state of one analysis case.
type Project struct {
	mu sync.Mutex

	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`

	events       []*domain.Event
	nodes        map[string]*domain.Node // node id -> node (immutable memo)
	layers       map[string]*domain.Layer
	current      domain.CurrentLayers
	params       map[string]*ParamsVersion
	paramsOrder  []string
	builds       []*domain.Event // build_succeeded events, in order
	currentBuild *BuildNodes
	decisions    []*domain.Decision
	// subject id -> current effective decision (after undo/resolve)
	effective map[string]*domain.Decision
	store     Store
	clock     Clock
	actor     string
}

// Store is the persistence boundary satisfied by internal/store.
type Store interface {
	AppendEvent(ev *domain.Event) error
	SaveLayer(l *domain.Layer) error
	SaveNode(n *domain.Node) error
	LoadEvents(projectID string) ([]*domain.Event, error)
	LoadLayer(id string) (*domain.Layer, error)
	LoadNode(id string) (*domain.Node, error)
	ListNodeIDs(projectHint string) ([]string, error)
}

// NewProject starts a brand-new project and records project_created.
func NewProject(id, name string, clock Clock, actor string, st Store) (*Project, error) {
	p := &Project{
		ID: id, Name: name, CreatedAt: clock.Now().Format(time.RFC3339Nano),
		nodes: map[string]*domain.Node{}, layers: map[string]*domain.Layer{},
		current: domain.CurrentLayers{}, params: map[string]*ParamsVersion{},
		effective: map[string]*domain.Decision{}, store: st, clock: clock, actor: actor,
	}
	_, err := p.append(domain.EvProjectCreated, map[string]any{"name": name})
	if err != nil {
		return nil, err
	}
	// install default parameters
	if _, err := p.ReviseParams("baseline", DefaultParams(), "", "基线参数", false); err != nil {
		return nil, err
	}
	return p, nil
}

// RestoreProject replays persisted events into a fresh state.
func RestoreProject(projectID string, clock Clock, st Store) (*Project, error) {
	evs, err := st.LoadEvents(projectID)
	if err != nil {
		return nil, err
	}
	if len(evs) == 0 {
		return nil, fmt.Errorf("engine: no events for project %q", projectID)
	}
	p := &Project{
		ID: projectID, nodes: map[string]*domain.Node{}, layers: map[string]*domain.Layer{},
		current: domain.CurrentLayers{}, params: map[string]*ParamsVersion{},
		effective: map[string]*domain.Decision{}, store: st, clock: clock,
	}
	for _, ev := range evs {
		if err := p.apply(ev); err != nil {
			return nil, fmt.Errorf("replay seq %d: %w", ev.Seq, err)
		}
	}
	return p, nil
}

func (p *Project) append(typ domain.EventType, payload any) (*domain.Event, error) {
	var parent string
	if n := len(p.events); n > 0 {
		parent = p.events[n-1].ID
	}
	seq := int64(len(p.events) + 1)
	ev := &domain.Event{
		Seq: seq, ProjectID: p.ID, Type: typ,
		At:    p.clock.Now().Format(time.RFC3339Nano),
		Actor: p.actor, ParentID: parent, Payload: payload,
	}
	id, err := domain.Hash([]any{"event", seq, string(typ), ev.At, ev.Actor, parent, payload})
	if err != nil {
		return nil, err
	}
	ev.ID = "ev" + id[:24]
	if err := p.store.AppendEvent(ev); err != nil {
		return nil, err
	}
	if err := p.apply(ev); err != nil {
		return nil, err
	}
	return ev, nil
}

func (p *Project) apply(ev *domain.Event) error {
	p.events = append(p.events, ev)
	switch ev.Type {
	case domain.EvProjectCreated:
		var pl struct{ Name string }
		if err := recode(ev.Payload, &pl); err == nil && p.Name == "" {
			p.Name = pl.Name
		}
		if p.CreatedAt == "" {
			p.CreatedAt = ev.At
		}
	case domain.EvLayerCommitted:
		var pl domain.LayerCommitPayload
		if err := recode(ev.Payload, &pl); err != nil {
			return err
		}
		l, err := p.store.LoadLayer(pl.LayerID)
		if err != nil {
			return fmt.Errorf("load layer %s: %w", pl.LayerID, err)
		}
		p.layers[l.ID] = l
		p.current[l.Kind] = l
	case domain.EvParamsRevised:
		var pl domain.ParamsPayload
		if err := recode(ev.Payload, &pl); err != nil {
			return err
		}
		pv := &ParamsVersion{Name: pl.Name, Params: pl.Params, ParentName: pl.ParentName,
			Reason: pl.Reason, FP: fingerprintParams(pl.Params), EventSeq: ev.Seq}
		if _, exists := p.params[pl.Name]; !exists {
			p.paramsOrder = append(p.paramsOrder, pl.Name)
		}
		p.params[pl.Name] = pv
	case domain.EvBuildSucceeded:
		var pl domain.BuildPayload
		if err := recode(ev.Payload, &pl); err != nil {
			return err
		}
		p.builds = append(p.builds, ev)
		// rebuild the in-memory BuildNodes from persisted nodes
		bn := &BuildNodes{BuildID: pl.BuildID}
		load := func(id string) *domain.Node {
			n, err := p.store.LoadNode(id)
			if err != nil {
				return nil
			}
			p.nodes[id] = n
			return n
		}
		bn.Corrected = load(firstByKind(pl.NodeIDs, p, "n_"+string(domain.NodeCorrectedSeries)+"_"))
		_ = bn
		// full load by id list, mapping kinds
		for _, id := range pl.NodeIDs {
			n := load(id)
			if n == nil {
				continue
			}
			switch n.Kind {
			case domain.NodeCorrectedSeries:
				bn.Corrected = n
			case domain.NodeFurnaceTrace:
				bn.Furnace = n
			case domain.NodeGapMask:
				bn.Gap = n
			case domain.NodeNoiseReport:
				bn.Noise = n
			case domain.NodeDTG:
				bn.DTG = n
			case domain.NodeTGNormalized:
				bn.TG = n
			case domain.NodePeakSet:
				bn.Peaks = n
			case domain.NodeReactionWindows:
				bn.Reaction = n
			case domain.NodeStageCandidates:
				bn.Stages = n
			case domain.NodeResidues:
				bn.Residues = n
			}
		}
		// decode corrected series for UI convenience
		if bn.Corrected != nil {
			var s domain.Series
			if err := recodeJSON(bn.Corrected.Body, &s); err == nil {
				bn.Series = &s
			}
		}
		if bn.DTG != nil {
			var s domain.Series
			if err := recodeJSON(bn.DTG.Body, &s); err == nil {
				bn.DTGRate = s.RateMgMin
				bn.DTGOK = s.Mask
			}
		}
		p.currentBuild = bn
	case domain.EvDecisionMade:
		var pl domain.DecisionPayload
		if err := recode(ev.Payload, &pl); err != nil {
			return err
		}
		d := &domain.Decision{
			Seq: ev.Seq, EventID: ev.ID, SubjectID: pl.SubjectID, Kind: pl.Kind,
			Verdict: pl.Verdict, Reason: pl.Reason, Params: pl.Params, At: ev.At,
			SupersedesEventID: pl.SupersedesEventID, ConflictWithEvent: pl.ConflictWithEvent,
		}
		p.decisions = append(p.decisions, d)
		// conflicting verdict on a subject already adjudicated: mark both
		if prev := p.effective[pl.SubjectID]; prev != nil &&
			pl.SupersedesEventID == "" && isOpposing(prev.Kind, pl.Kind) {
			d.ConflictWithEvent = prev.EventID
		}
		p.effective[pl.SubjectID] = d
	case domain.EvDecisionUndone:
		var pl struct {
			EventID string `json:"event_id"`
			Reason  string `json:"reason"`
		}
		if err := recode(ev.Payload, &pl); err != nil {
			return err
		}
		for _, d := range p.decisions {
			if d.EventID == pl.EventID {
				d.Undone = true
				d.UndoneBy = ev.ID
				if p.effective[d.SubjectID] != nil && p.effective[d.SubjectID].EventID == d.EventID {
					delete(p.effective, d.SubjectID)
				}
			}
		}
	case domain.EvConflictResolved:
		var pl struct {
			SubjectID     string `json:"subject_id"`
			WinnerEventID string `json:"winner_event_id"`
			Reason        string `json:"reason"`
		}
		if err := recode(ev.Payload, &pl); err != nil {
			return err
		}
		for _, d := range p.decisions {
			if d.SubjectID == pl.SubjectID {
				if d.EventID == pl.WinnerEventID {
					p.effective[d.SubjectID] = d
				}
			}
		}
	case domain.EvArchiveImported:
		// provenance only; layers/nodes/events already present in store
	}
	return nil
}

func isOpposing(a, b string) bool {
	return (a == "accept" && b == "reject") || (a == "reject" && b == "accept")
}

func firstByKind(ids []string, p *Project, prefix string) string {
	for _, id := range ids {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
			return id
		}
	}
	return ""
}

func recode(in, out any) error {
	b, err := jsonMarshal(in)
	if err != nil {
		return err
	}
	return jsonUnmarshal(b, out)
}

// accessors used by webapp/archive; defined in accessors.go
var _ = sort.Strings
