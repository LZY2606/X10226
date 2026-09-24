package engine

import (
	"context"
	"encoding/json"

	"tga-review/internal/derive"
	"tga-review/internal/domain"
)

func anyOf(v any) any {
	b, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(b, &out)
	return out
}

func (e *Engine) installLayerRevision(ctx context.Context, projectID string, doc RawDoc,
	par parsedStrata, version int, parentID, reason string) error {
	layer := domain.Layer(doc.Layer)
	sid := layerStableID(string(layer), doc.LogicalID)
	payload := map[string]any{
		"layer":     string(layer),
		"stable_id": sid,
		"version":   version,
		"parent_id": parentID,
		"reason":    reason,
	}
	switch layer {
	case domain.LayerRawPoints:
		payload["points"] = anyOf(par.rawPoints)
	case domain.LayerBlank:
		payload["points"] = anyOf(par.blankPoints)
	case domain.LayerFurnace:
		payload["segments"] = anyOf(par.furnace)
	case domain.LayerGas:
		payload["valves"] = anyOf(par.valves)
	case domain.LayerInitialMass:
		payload["raw"] = anyOf(*par.initialMass)
	}
	return e.emitLayer(ctx, projectID, payload)
}

// ReviseInitialMass registers a new version of the sample initial mass with a
// parent link. The old value is never overwritten.
func (e *Engine) ReviseInitialMass(ctx context.Context, projectID, text, unit, reason string) error {
	events, err := e.Store.Events(ctx, projectID, 0)
	if err != nil {
		return err
	}
	st, err := derive.Replay(events)
	if err != nil {
		return err
	}
	current := 0
	parent := ""
	if st.InitialMass != nil {
		current = st.InitialMass.Version
		parent = st.InitialMass.StableID
	}
	rv := domain.ParseRawValue(text, unit, domain.UnitMg)
	sid := layerStableID(string(domain.LayerInitialMass), "sample-initial-mass")
	payload := map[string]any{
		"layer":     string(domain.LayerInitialMass),
		"stable_id": sid,
		"version":   current + 1,
		"parent_id": parent,
		"reason":    reason,
		"raw":       anyOf(rv),
	}
	if err := e.emitLayer(ctx, projectID, payload); err != nil {
		return err
	}
	_, err = e.Recompute(ctx, projectID)
	return err
}

func (e *Engine) installParameters(ctx context.Context, projectID string, p derive.Parameters, reason string) error {
	current := len(p.VersionID) // unused; version tracked in id
	_ = current
	payload := map[string]any{
		"layer":      string(domain.LayerParameters),
		"stable_id":  p.VersionID,
		"version":    1,
		"parent_id":  p.ParentVersionID,
		"reason":     reason,
		"parameters": anyOf(p),
	}
	return e.emitLayer(ctx, projectID, payload)
}

// ReviseParameters installs a new parameter version (fingerprint changes ->
// recomputed derived values carry the new fingerprint).
func (e *Engine) ReviseParameters(ctx context.Context, projectID string, p derive.Parameters, reason string) error {
	events, err := e.Store.Events(ctx, projectID, 0)
	if err != nil {
		return err
	}
	st, err := derive.Replay(events)
	if err != nil {
		return err
	}
	parent := ""
	version := 1
	if len(st.ParamsHistory) > 0 {
		parent = st.Params.VersionID
		version = len(st.ParamsHistory) + 1
	}
	p.VersionID = layerStableID("params", reason+"#"+itoa(version))
	p.ParentVersionID = parent
	payload := map[string]any{
		"layer":      string(domain.LayerParameters),
		"stable_id":  p.VersionID,
		"version":    version,
		"parent_id":  parent,
		"reason":     reason,
		"parameters": anyOf(p),
	}
	if err := e.emitLayer(ctx, projectID, payload); err != nil {
		return err
	}
	_, err = e.Recompute(ctx, projectID)
	return err
}

func (e *Engine) installDrift(ctx context.Context, projectID, model, reason string) error {
	sid := layerStableID(string(domain.LayerDrift), "blank-zeroed")
	payload := map[string]any{
		"layer":     string(domain.LayerDrift),
		"stable_id": sid,
		"version":   1,
		"reason":    reason,
		"model":     model,
		"note":      "天平基线漂移：空白曲线零对齐后逐点扣除",
	}
	return e.emitLayer(ctx, projectID, payload)
}

// SetDriftModel changes the drift model locally; only corrected series and
// its downstream candidates/residue become stale.
func (e *Engine) SetDriftModel(ctx context.Context, projectID, model, reason string) error {
	events, err := e.Store.Events(ctx, projectID, 0)
	if err != nil {
		return err
	}
	st, err := derive.Replay(events)
	if err != nil {
		return err
	}
	version := 1
	parent := ""
	if st.Drift != nil {
		version = st.Drift.Version + 1
		parent = st.Drift.StableID
	}
	sid := layerStableID(string(domain.LayerDrift), model)
	payload := map[string]any{
		"layer":     string(domain.LayerDrift),
		"stable_id": sid,
		"version":   version,
		"parent_id": parent,
		"reason":    reason,
		"model":     model,
	}
	if err := e.emitLayer(ctx, projectID, payload); err != nil {
		return err
	}
	_, err = e.Recompute(ctx, projectID)
	return err
}

func (e *Engine) emitLayer(ctx context.Context, projectID string, payload map[string]any) error {
	seq, err := e.Store.NextSeq(ctx, projectID)
	if err != nil {
		return err
	}
	layer, _ := payload["layer"].(string)
	return e.append(ctx, &domain.Event{
		ID:        eventID("layer-"+layer, seq),
		ProjectID: projectID,
		Type:      domain.EvLayerRevised,
		AtVirtual: e.Clock.Now(),
		Payload:   payload,
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
