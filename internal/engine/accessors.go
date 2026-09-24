package engine

import "tgaline/internal/domain"

func (p *Project) Lock()   { p.mu.Lock() }
func (p *Project) Unlock() { p.mu.Unlock() }

func (p *Project) ID_() string             { return p.ID }
func (p *Project) Events() []*domain.Event { return p.events }
func (p *Project) EventBySeq(seq int64) *domain.Event {
	for _, e := range p.events {
		if e.Seq == seq {
			return e
		}
	}
	return nil
}
func (p *Project) Layers() map[string]*domain.Layer       { return p.layers }
func (p *Project) Current() domain.CurrentLayers          { return p.current }
func (p *Project) Layer(id string) *domain.Layer          { return p.layers[id] }
func (p *Project) Node(id string) *domain.Node            { return p.nodes[id] }
func (p *Project) Nodes() map[string]*domain.Node         { return p.nodes }
func (p *Project) Builds() []*domain.Event                { return p.builds }
func (p *Project) CurrentBuild() *BuildNodes              { return p.currentBuild }
func (p *Project) Decisions() []*domain.Decision          { return p.decisions }
func (p *Project) Effective() map[string]*domain.Decision { return p.effective }
func (p *Project) Params(name string) *ParamsVersion {
	if name == "" {
		name = p.CurrentParamsName()
	}
	return p.params[name]
}
func (p *Project) ParamsOrder() []string { return p.paramsOrder }
func (p *Project) CurrentParamsName() string {
	// most recently revised set
	var best string
	var bestSeq int64
	for name, pv := range p.params {
		if pv.EventSeq > bestSeq {
			bestSeq, best = pv.EventSeq, name
		}
	}
	return best
}
