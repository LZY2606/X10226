package engine

import (
	"encoding/json"

	"tga/internal/domain"
)

func jsonRoundTrip(in any, out any) {
	b, err := json.Marshal(in)
	if err != nil {
		panic(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		panic(err)
	}
}

func putSource(ev *domain.Event, src *domain.Source) {
	var raw map[string]any
	jsonRoundTrip(src, &raw)
	ev.Payload["source"] = raw
	ev.Refs = append(ev.Refs, src.ID)
}

func sourceFromPayload(ev *domain.Event) *domain.Source {
	raw, ok := ev.Payload["source"]
	if !ok {
		return nil
	}
	var src domain.Source
	jsonRoundTrip(raw, &src)
	return &src
}

func putDerivations(ev *domain.Event, ds []*domain.Derivation) {
	var raw []any
	for _, d := range ds {
		var m map[string]any
		jsonRoundTrip(d, &m)
		raw = append(raw, m)
		ev.Refs = append(ev.Refs, d.ID)
	}
	ev.Payload["derivations"] = raw
}

func derivationsFromPayload(ev *domain.Event) []*domain.Derivation {
	raw, ok := ev.Payload["derivations"]
	if !ok {
		return nil
	}
	var ds []*domain.Derivation
	jsonRoundTrip(raw, &ds)
	return ds
}

func putDecision(ev *domain.Event, d *domain.Decision) {
	var m map[string]any
	jsonRoundTrip(d, &m)
	ev.Payload["decision"] = m
	ev.Refs = append(ev.Refs, d.ID)
}

func decisionFromPayload(ev *domain.Event) *domain.Decision {
	raw, ok := ev.Payload["decision"]
	if !ok {
		return nil
	}
	var d domain.Decision
	jsonRoundTrip(raw, &d)
	return &d
}

func judgementsFromAny(raw any, seq int64) []*domain.Judgement {
	var js []*domain.Judgement
	jsonRoundTrip(raw, &js)
	for _, j := range js {
		j.EventSeq = seq
	}
	return js
}
