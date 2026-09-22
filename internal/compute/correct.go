package compute

import (
	"fmt"
	"math"

	"tgasys/internal/domain"
	"tgasys/internal/ingest"
)

// linearDrift is a balance baseline drift model m_off(t)=a+b*t.
type linearDrift struct {
	a, b float64
	used bool
	desc string
}

func (d *linearDrift) at(t float64) float64 {
	if d == nil || !d.used {
		return 0
	}
	return d.a + d.b*t
}

func fitDrift(rows []ingest.DriftRow, samples []domain.SamplePoint) *linearDrift {
	d := &linearDrift{}
	var pts [][2]float64
	model := ""
	for _, r := range rows {
		if r.Note != "" && len(rows) <= 2 {
			model = r.Note
		}
		if !r.Off.Unconvertible {
			pts = append(pts, [2]float64{r.T, r.Off.Value})
		}
	}
	switch {
	case len(pts) >= 2:
		var sx, sy, sxx, sxy float64
		for _, p := range pts {
			sx += p[0]
			sy += p[1]
			sxx += p[0] * p[0]
			sxy += p[0] * p[1]
		}
		n := float64(len(pts))
		den := n*sxx - sx*sx
		if math.Abs(den) > 1e-12 {
			d.b = (n*sxy - sx*sy) / den
			d.a = (sy - d.b*sx) / n
		}
		d.used = true
		d.desc = fmt.Sprintf("linear least-squares on %d drift anchors", len(pts))
	case contains(model, "linear"):
		// model linear slope <mg/min> [intercept]
		d.used = true
		d.desc = "declared linear drift model"
	default:
		d.desc = "no drift anchors; zero correction"
	}
	return d
}

type blankCurve struct {
	a, b, c float64 // quadratic offset vs t
	n       int
}

func (b *blankCurve) at(t float64) float64 {
	if b == nil {
		return 0
	}
	return b.a + b.b*t + b.c*t*t
}

func fitBlank(rows []ingest.BlankPoint) *blankCurve {
	good := make([]ingest.BlankPoint, 0, len(rows))
	for _, r := range rows {
		if !r.M.Unconvertible {
			good = append(good, r)
		}
	}
	if len(good) < 2 {
		return nil
	}
	if len(good) == 2 {
		slope := (good[1].M.Value - good[0].M.Value) / (good[1].T - good[0].T)
		return &blankCurve{a: good[0].M.Value - slope*good[0].T, b: slope, n: 2}
	}
	// Normal equations for quadratic fit.
	var s [4]float64
	var r [3]float64
	for _, p := range good {
		x := p.T
		y := p.M.Value
		xp := [4]float64{1, x, x * x, x * x * x}
		for i := 0; i < 4; i++ {
			s[i] += xp[i]
		}
		r[0] += y
		r[1] += y * x
		r[2] += y * x * x
	}
	n := float64(len(good))
	A := [3][3]float64{
		{n, s[1], s[2]},
		{s[1], s[2], s[3]},
		{s[2], s[3], sum4(good)},
	}
	b := solve3(A, r)
	return &blankCurve{a: b[0], b: b[1], c: b[2], n: len(good)}
}

func sum4(pts []ingest.BlankPoint) float64 {
	var v float64
	for _, p := range pts {
		v += p.T * p.T * p.T * p.T
	}
	return v
}

func solve3(A [3][3]float64, y [3]float64) [3]float64 {
	for col := 0; col < 3; col++ {
		piv := col
		for r := col + 1; r < 3; r++ {
			if math.Abs(A[r][col]) > math.Abs(A[piv][col]) {
				piv = r
			}
		}
		A[col], A[piv] = A[piv], A[col]
		y[col], y[piv] = y[piv], y[col]
		if math.Abs(A[col][col]) < 1e-18 {
			return [3]float64{}
		}
		for r := 0; r < 3; r++ {
			if r == col {
				continue
			}
			f := A[r][col] / A[col][col]
			for k := col; k < 3; k++ {
				A[r][k] -= f * A[col][k]
			}
			y[r] -= f * y[col]
		}
	}
	return [3]float64{y[0] / A[0][0], y[1] / A[1][1], y[2] / A[2][2]}
}
