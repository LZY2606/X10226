package domain

import (
	"math"
)

// AnalysisInput is the resolved latest-per-layer snapshot fed to the
// tga-v1 algorithm, together with exact source references.
type AnalysisInput struct {
	Params      Params
	Sources     []SourceRef
	Points      []RawPoint
	Drift       *DriftSpec
	Furnace     []FurnaceSegment
	Valves      []ValveEvent
	Blank       []BlankPoint
	InitialMass *InitialMass
}

// FitBlankLinear fits drift rate (mg/s) and intercept (mg) to blank points
// by ordinary least squares. Points with unparseable values are ignored but
// reported as diagnostics (never silently dropped).
type blankFit struct {
	slopeMgPerS float64
	interceptMg float64
	n           int
	diags       []Diag
}

func FitBlankLinear(blank []BlankPoint) blankFit {
	var f blankFit
	var sx, sy, sxx, sxy float64
	for _, bp := range blank {
		if !bp.Time.OK || !bp.Mass.OK {
			f.diags = append(f.diags, Diag{
				Level: "warning", Code: "blank_point_unparseable",
				Layer: string(LayerBlankCurve), Line: bp.Line,
				Message: "空白曲线点无法转换，已保留在原位置且不参与拟合",
			})
			continue
		}
		x, y := bp.Time.Value, bp.Mass.Value
		sx += x
		sy += y
		sxx += x * x
		sxy += x * y
		f.n++
	}
	if f.n < 2 {
		f.diags = append(f.diags, Diag{
			Level: "warning", Code: "blank_fit_under_determined",
			Layer:   string(LayerBlankCurve),
			Message: "有效空白点不足两个，无法拟合线性漂移，按零漂移处理",
		})
		return f
	}
	denom := float64(f.n)*sxx - sx*sx
	if math.Abs(denom) < 1e-12 {
		f.diags = append(f.diags, Diag{
			Level: "warning", Code: "blank_fit_singular",
			Layer:   string(LayerBlankCurve),
			Message: "空白曲线时间坐标退化，按零漂移处理",
		})
		return f
	}
	f.slopeMgPerS = (float64(f.n)*sxy - sx*sy) / denom
	f.interceptMg = (sy - f.slopeMgPerS*sx) / float64(f.n)
	return f
}

// segmentsByGaps splits sorted valid point indices into contiguous runs;
// gaps wider than p.GapMaxS must not be interpolated across.
func segmentsByGaps(pts []SeriesPoint, gapMaxS float64) [][]int {
	var segs [][]int
	var cur []int
	for i := range pts {
		if pts[i].Excluded {
			continue
		}
		if len(cur) > 0 {
			dt := pts[i].TimeS - pts[cur[len(cur)-1]].TimeS
			if dt > gapMaxS {
				segs = append(segs, cur)
				cur = nil
			}
		}
		cur = append(cur, i)
	}
	if len(cur) > 0 {
		segs = append(segs, cur)
	}
	return segs
}
