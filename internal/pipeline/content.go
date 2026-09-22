package pipeline

import "tga-review/internal/domain"

// contentUID derives a stable identity from the artifact inputs so that
// rebuilding from identical sources+parameters reuses the same identity.
func contentUID(d *domain.Derived) string {
	return domain.StableID("derived",
		d.ParamFP, d.Sources, stageFingerprint(d.Stages), pointFingerprint(d.Points),
		m0Fingerprint(d.Drift), blankFingerprint(d.Blank), furnaceFingerprint(d.Furnace))
}

func stageFingerprint(s []domain.Stage) any {
	type slim struct {
		UID string; Start, End, Peak, Loss float64; Noise bool
	}
	out := make([]slim, len(s))
	for i, st := range s {
		out[i] = slim{st.UID, st.StartT, st.EndT, st.PeakT, st.LossMg, st.NoiseSpike}
	}
	return out
}
func pointFingerprint(p []domain.SamplePoint) any {
	type slim struct{ T, M, Temp float64; Spike, Gap bool; Status string }
	out := make([]slim, len(p))
	for i, q := range p {
		out[i] = slim{q.T, q.Mass, q.Temp, q.Spike, q.GapAfter || q.GapBefore, string(q.Status)}
	}
	return out
}
func m0Fingerprint(d []domain.DriftPoint) any {
	type slim struct{ T, V float64; Status string }
	out := make([]slim, len(d))
	for i, q := range d {
		v := 0.0
		if q.Status == "ok" {
			v = q.Offset.Value
		}
		out[i] = slim{q.T, v, string(q.Status)}
	}
	return out
}
func blankFingerprint(b []domain.BlankPoint) any {
	type slim struct{ T, M float64; Status string }
	out := make([]slim, len(b))
	for i, q := range b {
		m := 0.0
		if q.Status == "ok" {
			m = q.Mass.Value
		}
		out[i] = slim{q.T, m, string(q.Status)}
	}
	return out
}
func furnaceFingerprint(f []domain.FurnaceSegment) any {
	type slim struct{ S, E, T0, T1 float64 }
	out := make([]slim, len(f))
	for i, q := range f {
		out[i] = slim{q.StartT, q.EndT, q.T0, q.T1}
	}
	return out
}
