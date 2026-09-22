// Package engine implements the domain math. Every function is pure: given the
// same source versions, Params and algorithm versions it returns identical
// derived nodes (content-addressed), so replay after reopen reproduces history.
package engine

import (
	"math"

	"tga-review/internal/domain"
)

type BuildError struct{ Msg string }

func (e *BuildError) Error() string { return e.Msg }

func fail(msg string) (*domain.DerivedNode, error) { return nil, &BuildError{Msg: msg} }

func normQ(q domain.Quantity) (float64, error) {
	n, err := q.Normalize()
	if err != nil {
		return math.NaN(), err
	}
	return n.Value, nil
}

// NormalizeSeries canonicalizes raw sample points into minutes/kelvin/mg.
// Rows that normalize successfully are emitted; rows that cannot normalize were
// already excluded at parse time, so here we only defend against non-finite data.
func NormalizeSeries(projectID, batchID string, fp domain.Fingerprint, srcID string, samples []domain.SamplePoint) (*domain.DerivedNode, error) {
	pts := make([]domain.CanonicalPoint, 0, len(samples))
	for _, s := range samples {
		t, err := normQ(s.Time)
		if err != nil {
			return fail("sample row " + itoa(s.Row) + ": " + err.Error())
		}
		tk, err := normQ(s.Temperature)
		if err != nil {
			return fail("sample row " + itoa(s.Row) + ": " + err.Error())
		}
		m, err := normQ(s.Mass)
		if err != nil {
			return fail("sample row " + itoa(s.Row) + ": " + err.Error())
		}
		if math.IsNaN(t) || math.IsNaN(tk) || math.IsNaN(m) {
			return fail("non-finite canonical value at row " + itoa(s.Row))
		}
		pts = append(pts, domain.CanonicalPoint{Row: s.Row, TMin: t, TK: tk, MassMg: m, Present: true})
	}
	payload := domain.SeriesPayload{Name: "normalized", Points: pts}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKNormalizedSeries), struct {
		Src string
		FP  domain.Fingerprint
		P   domain.SeriesPayload
	}{srcID, fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKNormalizedSeries, BatchID: batchID,
		Fingerprint: fp, SourceVersionIDs: []string{srcID},
		Payload: payload,
	}, nil
}

func itoa(i int) string {
	if i == 0 { return "0" }
	neg := i < 0
	if neg { i = -i }
	var b [20]byte
	p := len(b)
	for i > 0 { p--; b[p] = byte('0' + i%10); i /= 10 }
	if neg { p--; b[p] = '-' }
	return string(b[p:])
}
