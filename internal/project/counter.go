package project

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"tga-lineage/internal/identity"
	"tga-lineage/internal/model"
)

// Mutation is the single direct source changed in a counterexample run.
// Exactly one field is non-empty per run, which is what makes the blast
// radius check meaningful.
type Mutation struct {
	Scenario string
	Import   []byte // import payload (a single layer file or a bundle)
	// Parameter family bumps, mutually exclusive with Import.
	GasDelayMin    *float64
	ThermalLagMin  *float64
	DriftOffsetMg  *float64
	DriftEnabled   *bool
	NoiseTolerance *float64
	GapMaxStepMin  *float64
	PeakProminence *float64
	ExpectedScope  []string // artifact kinds expected to change
	Note           string
}

// RunCounterExample forks the current project (a cheap historical copy of
// its event stream), applies exactly one mutation, records affected vs.
// unaffected artifact identities, and stores a CounterExample verdict object
// in BOTH projects. Adopted and rejected runs are retained identically;
// rejection only marks the archive entry.
func (s *Service) RunCounterExample(ctx context.Context, basePID, name string, m Mutation) (string, *State, error) {
	base, err := s.State(ctx, basePID)
	if err != nil {
		return "", nil, err
	}
	now := s.clock.Now().UTC().Format(time.RFC3339)
	branchUID := identity.Hash("prj_", now, name, m.Scenario)
	info := model.ProjectInfo{
		UID: branchUID, Name: fmt.Sprintf("counter:%s:%s", m.Scenario, name),
		Description: "counterexample branch from " + basePID + " — " + m.Note,
		CreatedAt:   now,
	}
	if err := s.store.CreateProject(ctx, info); err != nil {
		return "", nil, err
	}
	if _, err := s.store.AppendAt(ctx, branchUID, model.EvtProjectCreated, "", now,
		"counterexample branch", info); err != nil {
		return "", nil, err
	}
	// copy event rows verbatim (same event identities preserved in branch)
	if err := s.cloneEvents(ctx, basePID, branchUID); err != nil {
		return "", nil, err
	}
	branch, err := s.State(ctx, branchUID)
	if err != nil {
		return "", nil, err
	}
	before := snapshotUids(branch)

	if m.Import != nil {
		cr, err := s.Import(ctx, branchUID, "mutation.csv", m.Import)
		if err != nil {
			return "", nil, err
		}
		branch = cr.State
	} else {
		st2, err := s.ReviseParams(ctx, branchUID, func(p model.ParamSet) model.ParamSet {
			if m.GasDelayMin != nil {
				p.Atmosphere.GasDelayMin = *m.GasDelayMin
			}
			if m.ThermalLagMin != nil {
				p.Atmosphere.ThermalLagMin = *m.ThermalLagMin
			}
			if m.DriftOffsetMg != nil {
				p.Drift.Offset = *m.DriftOffsetMg
			}
			if m.DriftEnabled != nil {
				p.Drift.Enabled = *m.DriftEnabled
			}
			if m.NoiseTolerance != nil {
				p.Noise.RelTolerance = *m.NoiseTolerance
			}
			if m.GapMaxStepMin != nil {
				p.Gap.MaxStepMin = *m.GapMaxStepMin
			}
			if m.PeakProminence != nil {
				p.Peak.Prominence = *m.PeakProminence
			}
			return p
		}, "counterexample "+m.Scenario+": "+m.Note)
		if err != nil {
			return "", nil, err
		}
		branch = st2
	}
	after := snapshotUids(branch)
	affected, unaffected, changedKinds := diffUids(before, after)
	blastOK := scopeConfined(changedKinds, m.ExpectedScope)

	c := model.CounterExample{
		UID:           identity.Hash("cex_", branchUID, m.Scenario),
		Scenario:      m.Scenario,
		Project:       branchUID,
		DirectSource:  directSourceOf(m),
		Affected:      affected,
		Unaffected:    unaffected,
		ExpectedScope: m.ExpectedScope,
		BlastOK:       blastOK,
		Verdict:       "open",
		Judgments:     collectJudgments(branch, affected),
	}
	if _, err := s.store.AppendAt(ctx, branchUID, model.EvtCounterRecorded, lastEventUID(branch),
		s.clock.Now().UTC().Format(time.RFC3339),
		"counterexample run: "+m.Scenario,
		model.CounterPayload{Counter: c}); err != nil {
		return "", nil, err
	}
	// mirror a compact record into the base project archive
	if _, err := s.store.AppendAt(ctx, basePID, model.EvtCounterRecorded, lastEventUID(base),
		s.clock.Now().UTC().Format(time.RFC3339),
		"counterexample launched: "+m.Scenario+" -> "+branchUID,
		model.CounterPayload{Counter: c}); err != nil {
		return "", nil, err
	}
	final, err := s.State(ctx, branchUID)
	return branchUID, final, err
}

// VerdictCounter records adoption or rejection; both outcomes stay stored.
func (s *Service) VerdictCounter(ctx context.Context, basePID, counterUID, verdict, note string, adopt bool) (*State, error) {
	st, err := s.State(ctx, basePID)
	if err != nil {
		return nil, err
	}
	for i := range st.Counters {
		c := st.Counters[i]
		if c.UID != counterUID {
			continue
		}
		c.Verdict = verdict
		c.Adopted = adopt
		c.RejectionNote = note
		if _, err := s.store.AppendAt(ctx, basePID, model.EvtCounterVerdict, lastEventUID(st),
			s.clock.Now().UTC().Format(time.RFC3339),
			"counterexample verdict: "+verdict,
			model.CounterPayload{Counter: c}); err != nil {
			return nil, err
		}
		if _, err := s.store.AppendAt(ctx, c.Project, model.EvtCounterVerdict, lastEventUID(st),
			s.clock.Now().UTC().Format(time.RFC3339),
			"counterexample verdict from base: "+verdict,
			model.CounterPayload{Counter: c}); err != nil {
			return nil, err
		}
		return s.State(ctx, basePID)
	}
	return nil, fmt.Errorf("counterexample %s not found", counterUID)
}

type snap struct {
	All         map[string]bool
	KindOf      map[string]string
	KindChanged []string
}

func snapshotUids(st *State) snap {
	out := snap{All: map[string]bool{}, KindOf: map[string]string{}}
	for _, uid := range st.ArtOrder {
		a := st.Arts[uid]
		if !st.CurrentUIDs[uid] {
			continue
		}
		out.All[uid] = true
		out.KindOf[uid] = a.Kind
	}
	return out
}

func diffUids(before, after snap) (affected, unaffected, changedKinds []string) {
	for uid := range after.All {
		if !before.All[uid] {
			affected = append(affected, uid)
			changedKinds = append(changedKinds, after.KindOf[uid])
		}
	}
	for uid := range before.All {
		if after.All[uid] {
			unaffected = append(unaffected, uid)
		}
	}
	return dedup(affected), dedup(unaffected), dedup(changedKinds)
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range in {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

// scopeConfined verifies every changed artifact kind was expected.
func scopeConfined(changed, expected []string) bool {
	ex := map[string]bool{}
	for _, k := range expected {
		ex[k] = true
	}
	for _, k := range dedup(changed) {
		if !ex[k] && k != "" {
			return false
		}
	}
	return true
}

func directSourceOf(m Mutation) string {
	switch {
	case m.Import != nil:
		return "import:" + m.Scenario
	case m.GasDelayMin != nil || m.ThermalLagMin != nil:
		return "params.atmosphere"
	case m.DriftOffsetMg != nil || m.DriftEnabled != nil:
		return "params.drift"
	case m.NoiseTolerance != nil:
		return "params.noise"
	case m.GapMaxStepMin != nil:
		return "params.gap"
	case m.PeakProminence != nil:
		return "params.peak"
	}
	return "unknown"
}

func collectJudgments(st *State, affected []string) []model.Judgment {
	want := map[string]bool{}
	for _, a := range affected {
		want[a] = true
	}
	var out []model.Judgment
	for _, j := range st.Judgments {
		if want[j.Subject] || j.Subject == "" {
			out = append(out, j)
		}
	}
	return out
}

func (s *Service) cloneEvents(ctx context.Context, src, dst string) error {
	return s.store.CloneEvents(ctx, src, dst)
}

var _ = strconv.Itoa
var _ = time.RFC3339
