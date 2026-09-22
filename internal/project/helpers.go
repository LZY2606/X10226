package project

import (
	"context"
	"encoding/json"
	"time"

	"tga-lineage/internal/model"
	"tga-lineage/internal/store"
)

type Row struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	Seq       int64  `json:"seq"`
}

func (s *Service) ListRows(ctx context.Context) ([]Row, error) {
	rows, err := s.store.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, Row{UID: r.UID, Name: r.Name, CreatedAt: r.CreatedAt, Seq: r.Seq})
	}
	return out, nil
}

// ParamsPublic returns the current parameter set in UI-friendly JSON form.
func ParamsPublic(p model.ParamSet) any {
	return map[string]any{
		"uid":        p.UID,
		"version":    p.Version,
		"parent":     p.Parent,
		"reason":     p.Reason,
		"drift":      p.Drift,
		"gap":        p.Gap,
		"noise":      p.Noise,
		"peak":       p.Peak,
		"atmosphere": p.Atmosphere,
	}
}

// StaleArtifacts returns artifact identities that are no longer current but
// remain viewable. The review UI marks them "过期" and never presents them as
// the present conclusion.
func (st *State) StaleArtifacts() []*model.Artifact {
	var out []*model.Artifact
	for _, uid := range st.ArtOrder {
		if !st.CurrentUIDs[uid] {
			out = append(out, st.Arts[uid])
		}
	}
	return out
}

func (s *Service) Note(ctx context.Context, pid, text string) (*State, error) {
	st, err := s.State(ctx, pid)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.AppendAt(ctx, pid, model.EvtNote, lastEventUID(st),
		s.clock.Now().UTC().Format(time.RFC3339), text,
		model.NotePayload{Text: text}); err != nil {
		return nil, err
	}
	return s.State(ctx, pid)
}

func (s *Service) Export(ctx context.Context) (store.Export, error) {
	return s.store.ExportAll(ctx)
}

func (s *Service) ImportArchive(ctx context.Context, ex store.Export) error {
	return s.store.ImportAll(ctx, ex)
}

// DecodeExport parses an export JSON payload.
func DecodeExport(raw []byte) (store.Export, error) {
	var ex store.Export
	err := json.Unmarshal(raw, &ex)
	return ex, err
}
