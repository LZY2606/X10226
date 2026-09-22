package pipeline

import (
	"fmt"
	"math"

	"tga-review/internal/domain"
)

// selectM0 chooses the latest reported_initial reading (fallback: latest
// buoyancy_corrected), and reports which layers were considered.
func selectM0(layers []domain.M0Layer) (float64, domain.ValueInput, string) {
	best := -1
	prefer := func(want string) bool {
		for i := len(layers) - 1; i >= 0; i-- {
			if layers[i].Status == domain.RowOK && layers[i].Layer == want {
				best = i
				return true
			}
		}
		return false
	}
	if !prefer("reported_initial") {
		prefer("buoyancy_corrected")
	}
	if best < 0 {
		return 0, domain.ValueInput{}, "no usable initial mass layer"
	}
	l := layers[best]
	return l.Mass.Value, domain.ValueInput{
		Path: "sources.m0", UID: l.UID,
		Value: fmt.Sprintf("%g", l.Mass.Value), Unit: "mg",
	}, ""
}

func buildValues(pts []domain.SamplePoint, stages []domain.Stage, m0 float64,
	m0in domain.ValueInput, m0Diag string, atm []domain.AtmWindow,
	refs []domain.SourceRef, fp string) []domain.ValueNode {

	nodes := []domain.ValueNode{}
	src := func(path, uid, label string) domain.SourceRef {
		return domain.SourceRef{Kind: "import", UID: uid, Label: label}
	}
	_ = src
	srcs := append([]domain.SourceRef(nil), refs...)

	// m0 node
	m0status := "ok"
	if m0Diag != "" {
		m0status = "unknown"
	}
	nodes = append(nodes, domain.ValueNode{
		Path: "m0", Label: "样品初重 m0",
		Value: fmt.Sprintf("%g", m0), Unit: "mg", Status: m0status,
		Algorithm: "m0.layer_selection@" + AlgoVersion, AlgoVer: AlgoVersion, ParamFP: fp,
		Sources: srcs, Diag: m0Diag,
	})

	// final residue node: mean mass over the last 5 valid points
	lastVals := []float64{}
	for i := len(pts) - 1; i >= 0 && len(lastVals) < 5; i-- {
		if pts[i].Status == domain.RowOK {
			lastVals = append(lastVals, pts[i].Mass)
		}
	}
	residStatus, residDiag := "ok", ""
	if len(lastVals) == 0 || m0 <= 0 {
		residStatus, residDiag = "unknown", "insufficient tail points or missing m0"
	}
	mEnd := 0.0
	for _, v := range lastVals {
		mEnd += v
	}
	if len(lastVals) > 0 {
		mEnd /= float64(len(lastVals))
	}
	nodes = append(nodes, domain.ValueNode{
		Path: "final_mass", Label: "末端质量（最后5个有效点均值）",
		Value: fmt.Sprintf("%g", round4(mEnd)), Unit: "mg",
		Status: boolStatus(len(lastVals) > 0),
		Algorithm: "tail_mean@" + AlgoVersion, AlgoVer: AlgoVersion, ParamFP: fp,
		Sources: srcs, Diag: diagIf(len(lastVals) == 0, "no valid tail points"),
	})
	pct := math.NaN()
	if residStatus == "ok" {
		pct = mEnd / m0 * 100
	}
	pctValue := "unknown"
	if residStatus == "ok" {
		pctValue = fmt.Sprintf("%.2f", pct)
	}
	nodes = append(nodes, domain.ValueNode{
		Path: "final_residue_pct", Label: "最终残留率",
		Value: pctValue, Unit: "%", Status: residStatus,
		Algorithm: "residue=m_end/m0*100@" + AlgoVersion, AlgoVer: AlgoVersion, ParamFP: fp,
		Sources: srcs,
		Inputs: []domain.ValueInput{
			m0in,
			{Path: "final_mass", Value: fmt.Sprintf("%g", round4(mEnd)), Unit: "mg"},
		},
		Diag: residDiag,
	})

	// per-stage residue/loss nodes, each carrying an expandable chain
	for _, st := range stages {
		lossPct := math.NaN()
		if m0 > 0 {
			lossPct = st.LossMg / m0 * 100
		}
		status := "ok"
		if st.NoiseSpike {
			status = "ok"
		}
		nodes = append(nodes, domain.ValueNode{
			Path:  fmt.Sprintf("stages.s%d.loss_pct", st.Index),
			Label: fmt.Sprintf("阶段 %d 失重 %%m0", st.Index),
			Value: fmt.Sprintf("%.2f", lossPct), Unit: "%", Status: status,
			Algorithm: "stage_loss=(m_start-m_end)/m0@" + AlgoVersion,
			AlgoVer: AlgoVersion, ParamFP: fp, Sources: srcs,
			Inputs: []domain.ValueInput{
				m0in,
				{Path: fmt.Sprintf("stages.s%d.start_mass", st.Index), Value: fmt.Sprintf("%g", round4(st.StartMass)), Unit: "mg"},
				{Path: fmt.Sprintf("stages.s%d.end_mass", st.Index), Value: fmt.Sprintf("%g", round4(st.EndMass)), Unit: "mg"},
			},
			Diag: st.Diag,
		})
	}

	// sum of stage losses: informational only; explicitly NOT forced to 100%
	sumPct := 0.0
	overlapNote := ""
	for _, st := range stages {
		if st.NoiseSpike {
			continue
		}
		if m0 > 0 {
			sumPct += st.LossMg / m0 * 100
		}
	}
	for i := 0; i < len(stages); i++ {
		for j := i + 1; j < len(stages); j++ {
			a, b := stages[i], stages[j]
			if !a.NoiseSpike && !b.NoiseSpike && a.StartT < b.EndT && b.StartT < a.EndT {
				overlapNote = "stages overlap; losses are not additive and are NOT normalized to 100%"
			}
		}
	}
	nodes = append(nodes, domain.ValueNode{
		Path: "stage_loss_sum_pct", Label: "阶段失重合计（不强制=100%）",
		Value: fmt.Sprintf("%.2f", sumPct), Unit: "%", Status: "ok",
		Algorithm: "informational_sum@" + AlgoVersion, AlgoVer: AlgoVersion, ParamFP: fp,
		Sources: srcs, Diag: overlapNote,
	})
	return nodes
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unknown"
}
func diagIf(cond bool, s string) string {
	if cond {
		return s
	}
	return ""
}
func round4(x float64) float64 { return fround(x, 4) }
