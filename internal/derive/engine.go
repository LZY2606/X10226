// Package derive turns the current raw layer versions + one Params revision
// into content-addressed, immutable derived nodes. Every derived object
// carries algorithm version, parameter fingerprint and direct sources.
package derive

import (
	"time"

	"tga-review/internal/domain"
)

// Engine carries head versions of all raw layers and a parameter set.
type Engine struct {
	params    domain.Params
	head      map[domain.RawKind]*domain.RawSource
	now       time.Time
	projectID string
}

// NewEngine builds an engine from the current head of each raw layer.
func NewEngine(p domain.Params, head map[domain.RawKind]*domain.RawSource, now time.Time) *Engine {
	if head == nil {
		head = map[domain.RawKind]*domain.RawSource{}
	}
	return &Engine{params: p, head: head, now: now}
}

func (e *Engine) current(k domain.RawKind) *domain.RawSource {
	if r := e.head[k]; r != nil {
		return r
	}
	return &domain.RawSource{Kind: k, Version: 0}
}

func (e *Engine) optional(k domain.RawKind) *domain.RawSource { return e.head[k] }

// m0Mg resolves initial mass with an explicit precedence: initmass layer wins;
// otherwise the first valid sample mass is used and flagged.
func (e *Engine) m0Mg() *float64 {
	if im := e.optional(domain.KindInitMass); im != nil && im.Payload.InitMass != nil && im.Payload.InitMass.Mass.Canon != nil {
		v := *im.Payload.InitMass.Mass.Canon
		return &v
	}
	if sm := e.optional(domain.KindSamples); sm != nil {
		for _, s := range sm.Payload.Samples {
			if s.Mass.Canon != nil {
				v := *s.Mass.Canon
				return &v
			}
		}
	}
	return nil
}

// Result bundles computed nodes with their content-addressed identities.
type Result struct {
	ParamsID string
	Correct  *domain.Derived
	DTG      *domain.Derived
	Stages   *domain.Derived
	Residue  *domain.Derived
}

// rawRef builds a direct-source ref for one layer.
func (e *Engine) rawRef(k domain.RawKind) (domain.Ref, bool) {
	r, ok := e.head[k]
	if !ok {
		return domain.Ref{}, false
	}
	return domain.Ref{Kind: "raw", ID: r.ID, Ver: r.Version, Label: k.Label()}, true
}
