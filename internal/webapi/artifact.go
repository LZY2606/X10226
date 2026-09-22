package webapi

import (
	"net/http"
	"strconv"

	"tgareview/internal/engine"
)

// CurrentArtifact loads the current-head artifact for SVG rendering.
func (a *API) CurrentArtifact(r *http.Request) (*engine.Artifact, error) {
	st, err := a.svc.CurrentState(r.Context(), r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	id := st.CurrentArtifactID()
	if id == "" {
		return nil, http.ErrMissingFile
	}
	return st.Artifacts[id], nil
}

// ArtifactAt loads the current artifact visible at a historical seq.
func (a *API) ArtifactAt(r *http.Request) (*engine.Artifact, error) {
	seq, err := strconv.ParseInt(r.PathValue("seq"), 10, 64)
	if err != nil {
		return nil, err
	}
	st, err := a.svc.StateAt(r.Context(), r.PathValue("id"), seq)
	if err != nil {
		return nil, err
	}
	id := st.CurrentArtifactID()
	if id == "" {
		return nil, http.ErrMissingFile
	}
	return st.Artifacts[id], nil
}
