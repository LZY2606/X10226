package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tga-review/internal/derive"
	"tga-review/internal/domain"
	"tga-review/internal/ingest"
)

// ImportReport describes what an import batch changed.
type ImportReport struct {
	BatchID      string                       `json:"batch_id"`
	Accepted     []ingest.FileResult          `json:"accepted"`
	Rejected     []ingest.FileResult          `json:"rejected"`
	HalfFailure  bool                         `json:"half_failure"`
	StaleNodes   []string                     `json:"stale_nodes"`
	NewNodes     []string                     `json:"new_nodes"`
	ChangedKinds []string                     `json:"changed_kinds"`
}

// Import parses a six-stratum batch (or a revision subset), persists raw
// sources immutably, and recomputes only downstream nodes. Rejected files in
// a partial batch are preserved as a half-failure and do not poison accepted
// strata.
func (e *Engine) Import(ctx context.Context, projectID string, files []ingest.FileInput) (*ImportReport, error) {
	st, err := e.loadState(ctx, projectID)
	if err != nil {
		return nil, err
	}
	batchID := ingest.ShortID("batch", projectID, len(st.Events), len(files), filesHash(files))
	versions := map[string]int{}
	parents := map[string]string{}
	for kind, cur := range st.Current {
		versions[kind] = cur.Version
		parents[kind] = cur.ID
	}
	batch := ingest.ParseBatch(projectID, batchID, files, versions, parents)

	rep := &ImportReport{BatchID: batchID}
	var changed []string
	for i := range batch.Files {
		fr := &batch.Files[i]
		if fr.Source == nil {
			continue
		}
		if fr.Accepted {
			// new version replaces prior version of the same kind
			if cur := st.Current[fr.Kind]; cur != nil {
				fr.Source.Version = cur.Version + 1
				fr.Source.ParentID = cur.ID
				fr.Source.ID = ingest.ShortID("src", projectID, fr.Kind,
					fr.Source.Version, contentOf(fr.Source), batchID)
			}
			if err := e.st.PutSource(ctx, *fr.Source, true); err != nil {
				return nil, err
			}
			rep.Accepted = append(rep.Accepted, *fr)
			changed = append(changed, fr.Kind)
		} else {
			if err := e.st.PutSource(ctx, *fr.Source, false); err != nil {
				return nil, err
			}
			rep.Rejected = append(rep.Rejected, *fr)
		}
	}
	rep.HalfFailure = len(rep.Rejected) > 0 && len(rep.Accepted) > 0
	rep.ChangedKinds = changed

	// event: raw sources committed (immutable), even rejected ones recorded
	if err := e.recordImport(ctx, st, rep); err != nil {
		return nil, err
	}
	if len(changed) > 0 {
		if err := e.recompute(ctx, st, changed, map[string]any{
			"reason": "source_import", "batch_id": batchID,
			"changed_kinds": changed, "half_failure": rep.HalfFailure}); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func (e *Engine) recordImport(ctx context.Context, st *State, rep *ImportReport) error {
	sources, _ := e.st.ListSources(ctx, st.Project.ID)
	snap := e.snapshotOf(st, sources)
	payload := map[string]any{
		"project_id":    st.Project.ID,
		"batch_id":      rep.BatchID,
		"accepted":      kindsOf(rep.Accepted),
		"rejected":      rejectedSummary(rep.Rejected),
		"half_failure":  rep.HalfFailure,
		"changed_kinds": rep.ChangedKinds,
	}
	return e.append(ctx, domain.EvSourceImported, payload, snap)
}

// ReviseParameters creates a new parameter version and invalidates only the
// downstream nodes whose fingerprint changes.
func (e *Engine) ReviseParameters(ctx context.Context, projectID string, next domain.Parameters, reason string) ([]string, error) {
	st, err := e.loadState(ctx, projectID)
	if err != nil {
		return nil, err
	}
	next.Version = st.Params.Version + 1
	next.Fingerprint = ingest.Fingerprint(derive.AlgorithmVersion, fingerprintView(next))
	oldFp := st.Params.Fingerprint
	if next.Fingerprint == ingest.Fingerprint(derive.AlgorithmVersion, fingerprintView(st.Params)) {
		// identical content: still record revision but nothing becomes stale
	}
	st.Params = next
	snap := e.snapshotOf(st, st.Sources)
	if err := e.append(ctx, domain.EvParamsRevised, map[string]any{
		"project_id": projectID, "old_fp": oldFp, "new_fp": next.Fingerprint,
		"reason": reason,
	}, snap); err != nil {
		return nil, err
	}
	affected := []string{domain.KindSamples} // timing params affect windows/stages/residue paths
	if err := e.recompute(ctx, st, affected, map[string]any{
		"reason": "params_revised", "old_fp": oldFp, "new_fp": next.Fingerprint}); err != nil {
		return nil, err
	}
	return []string{oldFp, next.Fingerprint}, nil
}

// recompute runs the pipeline on current sources/params and then supersedes
// only nodes whose identity changed and that are reachable from changedKinds.
func (e *Engine) recompute(ctx context.Context, st *State, changedKinds []string, meta map[string]any) error {
	// refresh latest sources from store
	sources, err := e.st.ListSources(ctx, st.Project.ID)
	if err != nil {
		return err
	}
	current := map[string]*domain.Source{}
	for i := range sources {
		s := &sources[i]
		if cur := current[s.Kind]; cur == nil || s.Version > cur.Version {
			current[s.Kind] = s
		}
	}
	st.Current = current
	st.Sources = sources

	oldByKind := map[string]domain.Node{}
	oldAll := map[string]domain.Node{}
	for _, n := range st.Nodes {
		oldAll[n.ID] = n
	}
	_ = oldAll
	// previous nodes indexed by (kind, base identity without fp)
	res := derive.Run(st.Project.ID, current, st.Params)
	var newIDs, staleIDs []string
	for _, n := range res.Nodes {
		if err := e.st.PutNode(ctx, n, true); err != nil {
			return err
		}
		newIDs = append(newIDs, n.ID)
	}
	// supersede previous current nodes of affected kinds whose id changed
	affectedNodeKinds := affectedKinds(changedKinds)
	oldNodes, _ := e.st.ListNodes(ctx, st.Project.ID, true)
	newSet := map[string]bool{}
	for _, id := range newIDs {
		newSet[id] = true
	}
	seq := int64(len(st.Events) + 1)
	for _, on := range oldNodes {
		if !affectedNodeKinds[on.Kind] {
			continue
		}
		if newSet[on.ID] {
			continue
		}
		// is there a replacement with same logical key (kind + sources)?
		repl := replacementFor(on, res.Nodes)
		if repl != "" {
			if err := e.st.Supersede(ctx, on.ID, repl, fmt.Sprintf(
				"downstream of changed %s", strings.Join(changedKinds, ",")), seq); err != nil {
				return err
			}
			staleIDs = append(staleIDs, on.ID)
		}
	}
	// refresh current node set so the snapshot embeds the exact visible nodes
	currentNodes, _ := e.st.ListNodes(ctx, st.Project.ID, true)
	st.Nodes = currentNodes
	decodeNodes(st, currentNodes)
	// persist a derived event with snapshot
	allSources, _ := e.st.ListSources(ctx, st.Project.ID)
	st.Params = currentParams(st.Params)
	st.StageOrder = stageOrder(res.Stages, st.StageOrder)
	snap := e.snapshotOf(st, allSources)
	snap.CurrentNodeIDs = newIDs
	payload := mergeMeta(meta, map[string]any{
		"project_id":   st.Project.ID,
		"new_nodes":    newIDs,
		"stale_nodes":  staleIDs,
		"param_fp":     st.Params.Fingerprint,
		"stage_order":  snap.StageOrder,
	})
	if err := e.append(ctx, domain.EvDerived, payload, snap); err != nil {
		return err
	}
	if len(staleIDs) > 0 {
		staleSnap := e.snapshotOf(st, allSources)
		staleSnap.CurrentNodeIDs = newIDs
		_ = e.append(ctx, domain.EvNodesStale, map[string]any{
			"project_id": st.Project.ID, "stale_nodes": staleIDs,
			"reason": strings.Join(changedKinds, ","),
		}, staleSnap)
	}
	return nil
}

// affectedKinds maps changed source strata to the node kinds they can make
// stale. Local changes stay local: blank/drift never invalidate gas windows.
func affectedKinds(changed []string) map[string]bool {
	out := map[string]bool{}
	for _, c := range changed {
		switch c {
		case domain.KindSamples:
			for _, k := range []string{domain.NodeNormSample, domain.NodeCorrected,
				domain.NodeDTG, domain.NodeStages, domain.NodeResidue} {
				out[k] = true
			}
		case domain.KindDrift, domain.KindBlank:
			for _, k := range []string{domain.NodeNormDrift, domain.NodeNormBlank,
				domain.NodeCorrected, domain.NodeDTG, domain.NodeStages, domain.NodeResidue} {
				out[k] = true
			}
		case domain.KindInitWeight:
			for _, k := range []string{domain.NodeNormInit, domain.NodeStages, domain.NodeResidue} {
				out[k] = true
			}
		case domain.KindGasEvents:
			out[domain.NodeNormGas] = true
			out[domain.NodeReactWindow] = true
			out[domain.NodeStages] = true
		case domain.KindProgram:
			out[domain.NodeNormProgram] = true
		}
	}
	return out
}

// replacementFor matches an old node to its new counterpart by kind and the
// non-fingerprint part of its identity (same direct sources/nodes).
func replacementFor(old domain.Node, fresh []domain.Node) string {
	oldBase := baseKey(old)
	for _, n := range fresh {
		if n.Kind == old.Kind && baseKey(n) == oldBase {
			return n.ID
		}
	}
	// kind-level fallback when source id itself changed (new version)
	for _, n := range fresh {
		if n.Kind == old.Kind {
			return n.ID
		}
	}
	return ""
}

func baseKey(n domain.Node) string {
	return n.Kind + "|" + strings.Join(n.DirectSources, ",") + "|" + strings.Join(n.DirectNodes, ",")
}

func stageOrder(stages []domain.Stage, prev []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range prev {
		seen[id] = true
		out = append(out, id)
	}
	for _, s := range stages {
		if !seen[s.ID] {
			out = append(out, s.ID)
		}
	}
	return out
}

func currentParams(p domain.Parameters) domain.Parameters { return p }

func mergeMeta(a, b map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func kindsOf(frs []ingest.FileResult) []string {
	var out []string
	for _, f := range frs {
		out = append(out, f.Kind)
	}
	return out
}

func rejectedSummary(frs []ingest.FileResult) []map[string]any {
	var out []map[string]any
	for _, f := range frs {
		out = append(out, map[string]any{
			"kind": f.Kind, "filename": f.Filename, "error": f.HardError,
			"source_id": safeSrcID(f.Source)})
	}
	return out
}

func safeSrcID(s *domain.Source) string {
	if s == nil {
		return ""
	}
	return s.ID
}

func filesHash(files []ingest.FileInput) string {
	parts := make([]string, len(files))
	for i, f := range files {
		parts[i] = f.Filename + ":" + f.Content
	}
	b, _ := json.Marshal(parts)
	return ingest.StableID(b)
}

func contentOf(s *domain.Source) string {
	var b strings.Builder
	for _, r := range s.Rows {
		for _, h := range s.Header {
			b.WriteString(r.Cells[h])
		}
	}
	return b.String()
}

// fingerprintView excludes the version counter and fingerprint itself.
func fingerprintView(p domain.Parameters) domain.Parameters {
	p.Version = 0
	p.Fingerprint = ""
	return p
}
