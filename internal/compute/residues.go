package compute

import "fmt"

// buildResidues reports the final residual fraction per gap-free segment. A
// residue at the end of the whole run is the headline number; earlier
// segments also get one so a gap can never silently erase evidence.
func buildResidues(p Params, r *Result) []Residue {
	var out []Residue
	for _, seg := range r.Segments {
		if len(seg.Points) == 0 {
			continue
		}
		last := seg.Points[len(seg.Points)-1]
		basis := r.InitialMassMG
		basisSrc := "stated initial mass"
		if basis <= 0 {
			basis = seg.Points[0].MassCorrect
			basisSrc = "first accepted point of segment"
		}
		res := Residue{
			SegmentID: seg.ID,
			AtT:       round(last.T, 6),
			AtTk:      round(last.Tk, 4),
			Sources:   appendSource(r, seg.ID),
			Status:    "computed",
		}
		res.ID = ID("residue", seg.ID, ftoa(res.AtT))
		if basis > 0 {
			res.ValuePct = round(last.MassCorrect/basis*100, 5)
			res.Unknown = false
		} else {
			res.Unknown = true
			res.Status = "unknown"
			res.Reason = "no mass basis available"
		}
		res.Chain = []ChainStep{
			{Order: 1, Op: "final_corrected_mass", Detail: fmt.Sprintf("last corrected mass of %s", seg.ID), Value: round(last.MassCorrect, 6), Unit: "mg", Ref: seg.ID},
			{Order: 2, Op: "basis", Detail: basisSrc, Value: round(basis, 6), Unit: "mg"},
			{Order: 3, Op: "ratio", Detail: "residue = final_mg / basis_mg * 100", Value: res.ValuePct, Unit: "%", Unknown: res.Unknown},
		}
		out = append(out, res)
	}
	return out
}
