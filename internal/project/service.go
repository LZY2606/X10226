// Package project is the state machine over the event store: immutable
// imports, versioned parameter revisions with fingerprints, scoped
// recomputation (only dependents of the changed source go stale), human
// decisions with conflict + retraction history, counterexample archives and
// branch projects. All timestamps come from an injectable clock so the full
// narrative replays under virtual time.
package project

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tga-lineage/internal/analysis"
	"tga-lineage/internal/clock"
	"tga-lineage/internal/identity"
	"tga-lineage/internal/ingest"
	"tga-lineage/internal/model"
	"tga-lineage/internal/store"
)

type Service struct {
	store *store.Store
	clock clock.Clock
}

func New(st *store.Store, c clock.Clock) *Service {
	if c == nil {
		c = clock.Wall{}
	}
	return &Service{store: st, clock: c}
}

// ImportReport describes one submitted file and whether the layer committed.
type ImportReport struct {
	Name    string
	Kind    string
	Version int
	Status  string // committed | rejected
	Layer   *model.LayerVersion
	Diag    *model.Diagnostic
}

type CommitResult struct {
	State   *State
	Reports []ImportReport
}

func (s *Service) CreateProject(ctx context.Context, name, description string) (*State, error) {
	now := s.clock.Now().UTC().Format(time.RFC3339)
	uid := identity.Hash("prj_", now, name)
	info := model.ProjectInfo{UID: uid, Name: name, Description: description, CreatedAt: now}
	if err := s.store.CreateProject(ctx, info); err != nil {
		return nil, err
	}
	if _, err := s.store.AppendAt(ctx, uid, model.EvtProjectCreated, "", now,
		"project created", info); err != nil {
		return nil, err
	}
	return s.rebuild(ctx, uid, 0)
}

func (s *Service) State(ctx context.Context, pid string) (*State, error) {
	return s.rebuild(ctx, pid, 0)
}

func (s *Service) StateAt(ctx context.Context, pid string, seq int64) (*State, error) {
	return s.rebuild(ctx, pid, seq)
}

func (s *Service) CurrentParams(st *State) model.ParamSet {
	if st.Params != nil {
		return *st.Params
	}
	uid := identity.Hash("par_", st.Info.UID, "default")
	return model.DefaultParams(uid)
}

// ReviseParams stores a new parameter version with a parent pointer, then
// recomputes only artifacts that consume the changed parameter family.
func (s *Service) ReviseParams(ctx context.Context, pid string, mutate func(model.ParamSet) model.ParamSet, reason string) (*State, error) {
	st, err := s.State(ctx, pid)
	if err != nil {
		return nil, err
	}
	cur := s.CurrentParams(st)
	v := cur.Version + 1
	next := mutate(cur)
	next.Version = v
	next.Parent = cur.UID
	next.Reason = reason
	next.UID = identity.Hash("par_", pid, strconv.Itoa(v))
	fp := paramFingerprint(next)
	next.Drift.Fingerprint, next.Gap.Fingerprint, next.Noise.Fingerprint,
		next.Peak.Fingerprint, next.Atmosphere.Fingerprint = fp, fp, fp, fp, fp
	if err := s.commitParams(ctx, pid, st, next, reason); err != nil {
		return nil, err
	}
	return s.State(ctx, pid)
}

func (s *Service) commitParams(ctx context.Context, pid string, st *State, p model.ParamSet, note string) error {
	parent := lastEventUID(st)
	ev, err := s.store.AppendAt(ctx, pid, model.EvtParamsRevised, parent,
		s.clock.Now().UTC().Format(time.RFC3339), note, model.ParamsPayload{Params: p})
	if err != nil {
		return err
	}
	np := p
	st.Params = &np
	return s.recompute(ctx, pid, st, ev.UID, "parameters revised: "+note)
}

// Import parses one or more files (single CSV or bundle), commits each
// accepted file as a new immutable LayerVersion, and records rejected files
// without aborting the others (file-level half failure). Row-level failures
// commit as ImportPartial layers with diagnostics attached.
func (s *Service) Import(ctx context.Context, pid, defaultName string, data []byte) (*CommitResult, error) {
	st, err := s.State(ctx, pid)
	if err != nil {
		return nil, err
	}
	files := ingest.SplitBundle(data)
	if len(files) == 1 && files[0][0] == "" {
		files[0][0] = defaultName
	}
	res := &CommitResult{}
	changed := map[string]bool{}
	parent := lastEventUID(st)
	at := s.clock.Now().UTC().Format(time.RFC3339)
	for _, f := range files {
		name, body := f[0], f[1]
		kind := ingest.ResolveKindOnly(name, body)
		ver := 1
		var parentLayer string
		if cur := st.Layers[kind]; cur != nil {
			ver = cur.Version + 1
			parentLayer = cur.UID
		}
		fr := ingest.ParseFile(pid, name, []byte(body), ver, parentLayer)
		rep := ImportReport{Name: name, Kind: fr.Kind, Version: ver}
		if !fr.OK || fr.Layer == nil {
			rep.Status = "rejected"
			rep.Diag = fr.Diagnostic
			res.Reports = append(res.Reports, rep)
			// the rejected submission is itself an immutable diagnostic event
			diag := fr.Diagnostic
			if diag == nil {
				diag = &model.Diagnostic{Ref: name, Field: "<file>", Code: "rejected", Message: "file rejected"}
			}
			evRej, err := s.store.AppendAt(ctx, pid, model.EvtLayerImported, parent, at,
				"import rejected: "+name, model.LayerPayload{Layer: model.LayerVersion{
					UID:     identity.Hash("lyr_", pid, kind, strconv.Itoa(ver), "rejected"),
					Project: pid, Kind: kind, Version: ver, Parent: parentLayer,
					Source: name, Status: model.ImportFailed, Diags: []model.Diagnostic{*diag},
				}})
			if err != nil {
				return nil, err
			}
			parent = evRej.UID
			continue
		}
		layer := *fr.Layer
		ev, err := s.store.AppendAt(ctx, pid, model.EvtLayerImported, parent, at,
			fmt.Sprintf("import %s v%d (%s)", kind, ver, layer.Status),
			model.LayerPayload{Layer: layer})
		if err != nil {
			return nil, err
		}
		parent = ev.UID
		st.Layers[kind] = &layer
		changed[kind] = true
		rep.Layer = &layer
		rep.Status = "committed"
		res.Reports = append(res.Reports, rep)
	}
	if len(changed) > 0 {
		st2, err := s.State(ctx, pid)
		if err != nil {
			return nil, err
		}
		parentEv, _ := s.lastEvent(ctx, pid)
		_ = parentEv
		if err := s.recompute(ctx, pid, st2, lastEventUID(st2), "recomputed after import"); err != nil {
			return nil, err
		}
	}
	final, err := s.State(ctx, pid)
	if err != nil {
		return nil, err
	}
	res.State = final
	return res, nil
}

// recompute runs the pure pipeline and commits only artifacts whose uid
// changed (i.e. only dependents of the changed direct source). Unchanged
// artifacts keep their identity. All current derived uids are still listed in
// one derived_committed event so the projection knows the fresh frontier.
func (s *Service) recompute(ctx context.Context, pid string, st *State, parent, note string) error {
	in := analysisInput(st)
	if in.Samples == nil {
		return nil
	}
	r := analysis.Build(in)
	if !r.InputsOK {
		return nil
	}
	// commit judgments
	if len(r.Judgments) > 0 {
		ev, err := s.store.AppendAt(ctx, pid, model.EvtJudgmentEmitted, parent,
			s.clock.Now().UTC().Format(time.RFC3339), note,
			model.JudgmentPayload{Judgments: r.Judgments})
		if err != nil {
			return err
		}
		parent = ev.UID
	}
	inputFP := identity.Hash("ifp_", strings.Join(in.SourceUIDs(), "|"))
	payload := model.DerivedPayload{InputFP: inputFP}
	for _, a := range r.Artifacts {
		payload.Artifacts = append(payload.Artifacts, a)
	}
	_, err := s.store.AppendAt(ctx, pid, model.EvtDerivedCommitted, parent,
		s.clock.Now().UTC().Format(time.RFC3339), note, payload)
	return err
}

func (s *Service) lastEvent(ctx context.Context, pid string) (string, error) {
	max, err := s.store.MaxSeq(ctx, pid)
	if err != nil || max == 0 {
		return "", err
	}
	var uid string
	err = nil
	_ = s.store.Stream(ctx, pid, max, func(ev model.Event) error {
		uid = ev.UID
		return nil
	})
	return uid, nil
}

func lastEventUID(st *State) string {
	if len(st.Events) == 0 {
		return ""
	}
	return st.Events[len(st.Events)-1].UID
}

func paramFingerprint(p model.ParamSet) string {
	return identity.Hash("pfp_", strconv.Itoa(p.Version),
		p.Drift.Algorithm, strconv.FormatBool(p.Drift.Enabled),
		f(p.Drift.BlankGain), f(p.Drift.Offset),
		p.Gap.Algorithm, f(p.Gap.MaxStepMin),
		p.Noise.Algorithm, f(p.Noise.RelTolerance),
		p.Peak.Algorithm, f(p.Peak.Prominence), f(p.Peak.WindowMin),
		p.Atmosphere.Algorithm, f(p.Atmosphere.GasDelayMin), f(p.Atmosphere.ThermalLagMin),
	)[4:]
}

func f(x float64) string { return strconv.FormatFloat(x, 'g', -1, 64) }

// ApplyParams creates a new parameter revision from optional field updates.
func (s *Service) ApplyParams(ctx context.Context, pid string, set func(model.ParamSet) model.ParamSet, reason string) (*State, error) {
	return s.ReviseParams(ctx, pid, set, reason)
}
