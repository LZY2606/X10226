package analysis

import "tga-evidence/internal/domain"

func assignSegments(d *preparedData, p domain.Params) {
	segment, last := 0, -1.0
	for i := range d.points {
		if i > 0 && d.points[i].timeS-d.points[i-1].timeS > p.GapThresholdSeconds {
			segment++
		}
		if i == 0 { segment = 0 }
		d.points[i].segment = segment
		if last >= 0 && d.points[i].timeS-last > p.GapThresholdSeconds {
			d.diags = appendDiag(d.diags, "sampling_gap", last, d.points[i].timeS)
		}
		last = d.points[i].timeS
	}
}

func appendDiag(diags []string, code string, a, b float64) []string {
	return append(diags, code+" "+domain.FormatFloat(a)+"s->"+domain.FormatFloat(b)+"s is not interpolated")
}

func correctPoints(d *preparedData) {
	if d.initial == nil {
		d.diags = append(d.diags, "no valid initial_mass layer; percentages remain unknown")
		return
	}
	for i := range d.points {
		pt := &d.points[i]
		corrected := pt.rawMg
		note := "raw sample mass"
		if b, ok, src := interpolateBlanks(d.blanks, pt.timeS); ok {
			corrected -= b
			note += "; blank-subtracted@" + src
		}
		if dr, ok, src := interpolateDrifts(d.drifts, pt.timeS); ok {
			corrected -= dr
			note += "; drift-corrected@" + src
		}
		pt.correctedMg = corrected
		pt.normalized = corrected / d.initial.valueMg
		pt.correctionNote = note
	}
	for i := 1; i < len(d.points); i++ {
		if d.points[i].segment == d.points[i-1].segment && d.points[i].correctedMg > d.points[i-1].correctedMg {
			d.points[i].nonMonotonic = true
		}
	}
}

func interpolateBlanks(points []blankPoint, t float64) (float64,bool,string) {
	if len(points) == 0 { return 0,false,"" }
	if t < points[0].timeS || t > points[len(points)-1].timeS { return 0,false,"" }
	for i:=1;i<len(points);i++ {
		if t >= points[i-1].timeS && t <= points[i].timeS {
			if points[i].timeS == points[i-1].timeS { return points[i].valueMg,true,points[i].recID }
			frac := (t-points[i-1].timeS)/(points[i].timeS-points[i-1].timeS)
			v := points[i-1].valueMg+frac*(points[i].valueMg-points[i-1].valueMg)
			return v,true,points[i-1].recID+"|"+points[i].recID
		}
	}
	return 0,false,"""
}

func interpolateDrifts(points []driftPoint, t float64) (float64,bool,string) {
	if len(points) == 0 { return 0,false,"" }
	if t < points[0].timeS || t > points[len(points)-1].timeS { return 0,false,"" }
	for i:=1;i<len(points);i++ {
		if t >= points[i-1].timeS && t <= points[i].timeS {
			if points[i].timeS == points[i-1].timeS { return points[i].valueMg,true,points[i].recID }
			frac := (t-points[i-1].timeS)/(points[i].timeS-points[i-1].timeS)
			v := points[i-1].valueMg+frac*(points[i].valueMg-points[i-1].valueMg)
			return v,true,points[i-1].recID+"|"+points[i].recID
		}
	}
	return 0,false,""
}

func computeDTG(d *preparedData, p domain.Params) {
	for i := range d.points {
		if i == 0 || i == len(d.points)-1 ||
			d.points[i].segment != d.points[i-1].segment ||
			d.points[i].segment != d.points[i+1].segment {
			continue
		}
		dt := (d.points[i+1].timeS-d.points[i-1].timeS)/60.0
		if dt > 0 {
			d.points[i].dtgMgPerMin = (d.points[i+1].correctedMg-d.points[i-1].correctedMg)/dt
		}
	}
	half := p.SmoothWindow / 2
	for i := range d.points {
		var sum float64
		n := 0
		for j := i-half; j <= i+half; j++ {
			if j >= 0 && j < len(d.points) && d.points[j].segment == d.points[i].segment {
				sum += d.points[j].dtgMgPerMin
				n++
			}
		}
		if n > 0 { d.points[i].smoothDTG = sum/float64(n) }
	}
}
