package engine

import "tgaline/internal/domain"

// ReplayAsOf reconstructs the read-only state exactly as it existed after
// event seq. It does not write anything; older node ids remain resolvable
// because derived results are immutable and content-addressed.
func ReplayAsOf(projectID string, seq int64, clock Clock, st Store) (*Project, error) {
	all, err := st.LoadEvents(projectID)
	if err != nil {
		return nil, err
	}
	p := &Project{
		ID: projectID, nodes: map[string]*domain.Node{}, layers: map[string]*domain.Layer{},
		current: domain.CurrentLayers{}, params: map[string]*ParamsVersion{},
		effective: map[string]*domain.Decision{}, store: st, clock: clock,
	}
	for _, ev := range all {
		if ev.Seq > seq {
			break
		}
		if err := p.apply(ev); err != nil {
			return nil, err
		}
	}
	return p, nil
}

// AtSeq reports whether the project currently has exactly seq events.
func (p *Project) AtSeq() int64 { return int64(len(p.events)) }

// CurrentBuildEventSeq returns the seq of the latest build, and whether
// decisions after it exist (they do not invalidate computed nodes but
// change adjudications).
func (p *Project) LatestBuildEvent() *domain.Event {
	if len(p.builds) == 0 {
		return nil
	}
	return p.builds[len(p.builds)-1]
}
