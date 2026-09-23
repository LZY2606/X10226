package service

import (
	"sort"

	"tga-lineage/internal/domain"
)

// FragmentView is one versioned layer shown in the review UI.
type FragmentView struct {
	Kind      string                   `json:"kind"`
	Version   int                      `json:"version"`
	Filename  string                   `json:"filename"`
	Format    string                   `json:"format"`
	Imported  bool                     `json:"imported"`
	Accepted  bool                     `json:"accepted"`
	Rows      []domain.Row             `json:"rows"`
	DiagCount int                      `json:"diagnostic_count"`
}

// NodeView wraps a node with current/stale freshness and a display identity.
type NodeView struct {
	domain.Node
	Fresh      bool `json:"fresh"`
	StaleRun   int  `json:"stale_run,omitempty"`
	BecameRun  int  `json:"became_run,omitempty"`
	DisplayID  string `json:"display_id"`
	HasDecision *domain.Decision `json:"decision,omitempty"`
}

// RunView summarizes one derivation run for history/staleness.
type RunView struct {
	Number      int      `json:"number"`
	EventID     int      `json:"event_id"`
	Algorithm   string   `json:"algorithm"`
	ParamFP     string   `json:"param_fp"`
	NodeCount   int      `json:"node_count"`
	StaleIDs    []string `json:"stale_ids"`
}

// ViewModel is the single payload rendered by the review page.
type ViewModel struct {
	ProjectID   string                    `json:"project_id"`
	Name        string                    `json:"name"`
	HeadEvent   int                       `json:"head_event"`
	Historical  bool                      `json:"historical"`
	Fragments   []FragmentView            `json:"fragments"`
	Params      map[string]domain.Param   `json:"params"`
	ParamVersion int                      `json:"param_version"`
	ParamFP     string                    `json:"param_fp"`
	Nodes       []NodeView                `json:"nodes"`
	StaleNodes  []NodeView                `json:"stale_nodes"`
	ManualNodes []NodeView                `json:"manual_nodes"`
	Runs        []RunView                 `json:"runs"`
	Decisions   []*domain.Decision        `json:"decisions"`
	Conflicts   []*domain.Conflict        `json:"conflicts"`
	Events      []EventView               `json:"events"`
	Algorithm   string                    `json:"algorithm"`
}

// EventView summarizes an event for the history timeline.
type EventView struct {
	ID      int    `json:"id"`
	Kind    string `json:"kind"`
	At      string `json:"at"`
	Summary string `json:"summary"`
}

func (p *Project) View(events []domain.Event) *ViewModel {
	vm := &ViewModel{
		ProjectID: p.ID, Name: p.Name, HeadEvent: p.headEvent,
		Params: cloneParams(p.params), ParamVersion: p.paramVer, ParamFP: p.paramFP,
		Algorithm: engineAlgorithm(),
	}
	for _, k := range domain.AllFragmentKinds() {
		for _, f := range p.fragHistory[k] {
			fv := FragmentView{Kind: string(f.Kind), Version: f.Version, Filename: f.Filename,
				Format: f.Format, Imported: f.Imported, Accepted: f.Accepted, Rows: f.Rows}
			for _, r := range f.Rows {
				fv.DiagCount += len(r.Diagnostics)
			}
			vm.Fragments = append(vm.Fragments, fv)
		}
	}
	decByNode := map[string]*domain.Decision{}
	for _, d := range p.decisions {
		if !d.Retracted {
			decByNode[d.NodeID] = d
		}
	}
	for id, n := range p.current {
		nv := NodeView{Node: n, Fresh: true, DisplayID: id}
		if d, ok := decByNode[id]; ok {
			dd := *d
			nv.HasDecision = &dd
		}
		vm.Nodes = append(vm.Nodes, nv)
	}
	// stale nodes: keep them viewable, never presented as current
	staleSeen := map[string]bool{}
	for runNum := len(p.runs); runNum >= 1; runNum-- {
		for _, n := range p.runNodes[runNum] {
			if _, cur := p.current[n.ID]; cur {
				continue
			}
			if staleSeen[n.ID] {
				continue
			}
			staleSeen[n.ID] = true
			nv := NodeView{Node: n, Fresh: false, StaleRun: p.staleAt[n.ID], BecameRun: runNum, DisplayID: n.ID}
			vm.StaleNodes = append(vm.StaleNodes, nv)
		}
	}
	for _, m := range p.manualList() {
		vm.ManualNodes = append(vm.ManualNodes, NodeView{Node: m, Fresh: true, DisplayID: m.ID})
	}
	sortNodeViews(vm.Nodes)
	sortNodeViews(vm.StaleNodes)
	for i, r := range p.runs {
		vm.Runs = append(vm.Runs, RunView{Number: r.Number, EventID: r.EventID,
			Algorithm: r.Algorithm, ParamFP: r.ParamFP, NodeCount: len(r.Nodes), StaleIDs: r.StaleIDs})
		_ = i
	}
	vm.Decisions = p.decisions
	vm.Conflicts = p.conflicts
	for _, e := range events {
		if e.ProjectID != p.ID {
			continue
		}
		vm.Events = append(vm.Events, EventView{ID: e.ID, Kind: string(e.Kind), At: e.At, Summary: summarize(e)})
	}
	return vm
}

func sortNodeViews(ns []NodeView) {
	sort.SliceStable(ns, func(i, j int) bool {
		if ns[i].Kind != ns[j].Kind {
			return kindOrder(ns[i].Kind) < kindOrder(ns[j].Kind)
		}
		return ns[i].ID < ns[j].ID
	})
}

func kindOrder(k string) int {
	order := []string{
		"source/raw_sample", "source/sample_temp", "source/raw_blank",
		"source/furnace_program", "source/gas_timeline", "source/initial_mass",
		"derived/blank_corrected", "derived/drift_curve", "derived/drift_corrected",
		"derived/smoothed", "derived/dtg", "derived/peaks",
		"stage/candidate", "stage/manual", "derived/residue",
		"derived/sampling_gaps", "derived/nonmonotonic", "derived/import_quality",
	}
	for i, o := range order {
		if o == k {
			return i
		}
	}
		return len(order)
}

func engineAlgorithm() string { return "tga-stage/1.3.0" }
