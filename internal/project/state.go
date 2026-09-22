package project

import (
	"sort"

	"tga-lineage/internal/model"
)

// State is the projection of a project event stream at one sequence number.
type State struct {
	Info   model.ProjectInfo
	Seq    int64
	Layers map[string]*model.LayerVersion // kind -> current version
	Params *model.ParamSet
	// Artifacts keyed by uid. Artifacts never change uid when their inputs
	// are unchanged, so the map doubles as the dependency cache.
	Arts map[string]*model.Artifact
	// Art order is the order artifact uids first appeared, for stable UI.
	ArtOrder  []string
	Decisions []model.Decision
	Judgments []model.Judgment
	Counters  []model.CounterExample
	Events    []model.Event
	// CurrentUIDs is the set of artifacts produced by the latest
	// recomputation; older uids remain in Arts for history but are stale.
	CurrentUIDs map[string]bool
	InputFP     string
}

func newState(info model.ProjectInfo) *State {
	return &State{
		Info: info, Layers: map[string]*model.LayerVersion{},
		Arts: map[string]*model.Artifact{}, CurrentUIDs: map[string]bool{},
	}
}

func (st *State) CurrentArtifacts() []*model.Artifact {
	out := make([]*model.Artifact, 0, len(st.ArtOrder))
	for _, uid := range st.ArtOrder {
		if st.CurrentUIDs[uid] {
			out = append(out, st.Arts[uid])
		}
	}
	return out
}

// ArtifactFresh reports whether an artifact is produced from the current
// layer versions and parameter set. Historical viewers use the same data at
// older seq instead of mutating this flag.
func (st *State) ArtifactFresh(a *model.Artifact) bool { return st.CurrentUIDs[a.UID] }

// DecisionsFor returns decisions targeting an artifact or any of its
// contained stable ids (stages, residuals, points), newest last.
func (st *State) DecisionsFor(uid string) []model.Decision {
	var out []model.Decision
	for _, d := range st.Decisions {
		if d.TargetUID == uid || targetsContained(st, d.TargetUID, uid) {
			out = append(out, d)
		}
	}
	return out
}

func targetsContained(st *State, target, wanted string) bool {
	for _, a := range st.Arts {
		if a.UID != target {
			continue
		}
		for _, s := range a.Stages {
			if s.UID == wanted {
				return true
			}
		}
		for _, r := range a.Residuals {
			if r.UID == wanted {
				return true
			}
		}
	}
	return false
}

// EffectiveDecision returns the latest non-retracted decision for a target.
func (st *State) EffectiveDecision(uid string) *model.Decision {
	var best *model.Decision
	for i := range st.Decisions {
		d := &st.Decisions[i]
		if d.TargetUID != uid || d.Verdict == model.DecisionRetracted {
			continue
		}
		best = d
	}
	return best
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// AllLayers returns every committed layer version across all kinds, ordered
// by kind then version, so the UI can show parent chains and revisions.
func (st *State) AllLayers() []model.LayerVersion {
	var out []model.LayerVersion
	for _, k := range model.LayerKinds {
		l := st.Layers[k]
		if l == nil {
			continue
		}
		// current projection retains only the newest version; older versions
		// remain available via history events when needed
		out = append(out, *l)
	}
	return out
}

// LayerHistory walks stored events to return every version of a kind.
func (st *State) LayerHistory(kind string) []model.LayerVersion {
	var out []model.LayerVersion
	for _, ev := range st.Events {
		if ev.Kind != model.EvtLayerImported {
			continue
		}
		lp, ok := ev.Payload.(model.LayerPayload)
		if !ok || lp.Layer.Kind != kind {
			continue
		}
		out = append(out, lp.Layer)
	}
	return out
}

func (st *State) CurrentParamSet() model.ParamSet {
	if st.Params != nil {
		return *st.Params
	}
	return model.DefaultParams("")
}
