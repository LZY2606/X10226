package compute

import (
	"strconv"

	"tga-archive/internal/domain"
)

func strconvF(x float64) string {
	return strconv.FormatFloat(x, 'g', 6, 64)
}

// node builds a content-addressed node. Identity = (algo, param fp, kind,
// label, formula, value sources), giving stable ids across replays.
func (e *engine) node(kind, label, formula string, value float64, unit, status string, sources []Ref) Node {
	parts := []any{AlgoVersion, e.paramFP, kind, label, formula, strconvF(value), unit, status}
	for _, s := range sources {
		parts = append(parts, s.Kind, s.ID, s.Role)
	}
	id := domain.ContentID("node", parts...)
	n := Node{ID: id, Kind: kind, Label: label, Value: value, Unit: unit,
		Status: status, Formula: formula, Sources: sources}
	return n
}

// stageID content-addresses a candidate independently of its chain node.
func (e *engine) stageID(group string, t0, t1, peakT float64, gas string, sources []Ref, extra string) string {
	parts := []any{AlgoVersion, e.paramFP, "stage", group, strconvF(t0), strconvF(t1),
		strconvF(peakT), gas, extra}
	for _, s := range sources {
		parts = append(parts, s.Kind, s.ID, s.Role)
	}
	return domain.ContentID("stage", parts...)
}

func (e *engine) sampleTempAt(t float64) (float64, string) {
	if len(e.progPts) == 0 {
		return 0, "unknown"
	}
	// sample at time t experiences the program value programmed at t-lag.
	tp := t - e.p.TempLagS
	ts := make([]float64, len(e.progPts))
	ys := make([]float64, len(e.progPts))
	for i, p := range e.progPts {
		ts[i], ys[i] = p.TProg, p.Temp
	}
	if tp < ts[0] {
		return ys[0], "extrapolated_start"
	}
	if tp > ts[len(ts)-1] {
		return ys[len(ys)-1], "extrapolated_end"
	}
	return interp(tp, ts, ys), "ok"
}
