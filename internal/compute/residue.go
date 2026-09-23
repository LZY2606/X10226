package compute

import (
	"sort"

	"tga-archive/internal/domain"
)

func strconvI(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	p := len(b)
	for i > 0 {
		p--
		b[p] = digits[i%10]
		i /= 10
	}
	if neg {
		p--
		b[p] = '-'
	}
	return string(b[p:])
}

func (e *engine) buildResidue() {
	if len(e.curve) == 0 {
		return
	}
	lastSeg := maxSeg(e.curve)
	var tail []CurvePoint
	for _, c := range e.curve {
		if c.Segment == lastSeg {
			tail = append(tail, c)
		}
	}
	if len(tail) < 1 {
		return
	}
	// tail = last 10% of the final segment duration
	t0, t1 := tail[0].T, tail[len(tail)-1].T
	winStart := t1 - 0.1*(t1-t0)
	var win []CurvePoint
	for _, c := range tail {
		if c.T >= winStart {
			win = append(win, c)
		}
	}
	var sum float64
	ids := []Ref{}
	for _, c := range win {
		sum += c.MCorr
		ids = append(ids, Ref{Kind: "node", ID: c.Node.ID, Role: "tail"})
	}
	mean := sum / float64(len(win))
	m0 := e.lookupM0()

	r := Residue{TailStart: winStart, TailEnd: t1, TailMeanMg: mean}
	r.M0Mg = 0
	status := "ok"
	if m0 == nil {
		status = "unknown_m0"
		r.PercentUnknown = true
	} else {
		r.M0Mg = m0.Num
		r.Percent = 100 * mean / m0.Num
	}
	chainTail := e.node("residue.tail", "tail mean corrected mass",
		"mean of last 10% of final segment", mean, UnitMass, "ok", ids)
	chain := []Node{
		e.node("residue.win", "tail window",
			"[t_end-0.1*span, t_end] of final segment", winStart, UnitTime, "ok", nil),
		chainTail,
	}
	if m0 != nil {
		m0node := e.node("residue.m0", "sample initial mass m0",
			"current accepted mass0 revision", m0.Num, UnitMass, "ok",
			[]Ref{{Kind: "raw", ID: e.mass0ID(), Role: "mass0"}})
		chain = append(chain, m0node)
		chain = append(chain, e.node("residue.pct", "final residue %",
			"100 * tail_mean / m0", r.Percent, "%", status,
			[]Ref{{Kind: "node", ID: chainTail.ID}, {Kind: "node", ID: m0node.ID}}))
	} else {
		chain = append(chain, Node{
			ID: domain.ContentID("node", AlgoVersion, e.paramFP, "residue.m0-missing"),
			Kind: "residue.m0", Label: "sample initial mass m0",
			Status: "unknown", Formula: "no accepted mass0 revision",
			Sources: []Ref{{Kind: "rule", ID: "rule:mass0-required"}},
		})
		chain = append(chain, Node{
			ID: domain.ContentID("node", AlgoVersion, e.paramFP, "residue.pct-unknown"),
			Kind: "residue.pct", Label: "final residue %", Status: "unknown",
			Formula: "cannot compute percentage without m0; value left unknown, never guessed",
			Sources: []Ref{{Kind: "rule", ID: "rule:no-guessing"}},
		})
	}
	r.Chain = chain
	r.Status = status
	r.Node = chain[len(chain)-1]
	r.ResiduesSortDummy()
	sort.Slice(e.gaps, func(i, j int) bool { return e.gaps[i].TBefore < e.gaps[j].TBefore })
	_ = sort.Float64Slice{}
	e.residues = append(e.residues, r)
}

func (e *engine) mass0ID() string {
	var best Row
	seq := -1
	for _, r := range e.mass0 {
		if r.Order > seq {
			best, seq = r, r.Order
		}
	}
	return best.ID
}
