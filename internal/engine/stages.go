package engine

import (
	"fmt"
	"math"
	"sort"

	"tga/internal/model"
)

// peak is a DTG minimum (mass-loss rate maximum), rate is negative.
type peak struct {
	Index         int
	TimeSec       float64
	TempC         float64
	RateMgPerSec  float64
	Prominence    float64
	ValleyIndex   int
	ValleyTimeSec float64
}

// detectPeaks finds negative-rate local minima with prominence above threshold.
// Points across gaps are never joined: each contiguous valid block is scanned
// separately.
func detectPeaks(dtg []DTGPoint, threshold float64) []peak {
	var peaks []peak
	n := len(dtg)
	i := 0
	for i < n {
		if dtg[i].State != "ok" {
			i++
			continue
		}
		j := i
		for j+1 < n && dtg[j+1].State == "ok" {
			j++
		}
		peaks = append(peaks, peaksInBlock(dtg, i, j, threshold)...)
		i = j + 1
	}
	sort.Slice(peaks, func(a, b int) bool { return peaks[a].TimeSec < peaks[b].TimeSec })
	return peaks
}

func peaksInBlock(dtg []DTGPoint, lo, hi int, threshold float64) []peak {
	var out []peak
	for k := lo + 1; k < hi; k++ {
		v := dtg[k].DtgMgPerSec
		if v >= 0 {
			continue
		}
		if v <= dtg[k-1].DtgMgPerSec && v <= dtg[k+1].DtgMgPerSec {
			// prominence: depth above the higher of the two neighboring maxima
			leftMax := v
			for x := k; x >= lo; x-- {
				leftMax = math.Min(leftMax, dtg[x].DtgMgPerSec)
			}
			_ = leftMax
			baseL := maxRate(dtg, lo, k)
			baseR := maxRate(dtg, k, hi)
			base := math.Min(baseL, baseR)
			prom := math.Abs(base - v)
			if prom >= threshold {
				// valley (saddle) to nearest neighbor peak
				vi, vt := nearestValley(dtg, k, lo, hi)
				out = append(out, peak{
					Index: k, TimeSec: dtg[k].TimeSec, TempC: dtg[k].TempC,
					RateMgPerSec: v, Prominence: prom,
					ValleyIndex: vi, ValleyTimeSec: vt,
				})
			}
		}
	}
	return out
}

func maxRate(dtg []DTGPoint, a, b int) float64 {
	m := dtg[a].DtgMgPerSec
	for k := a; k <= b; k++ {
		if dtg[k].DtgMgPerSec > m {
			m = dtg[k].DtgMgPerSec
		}
	}
	return m
}

// nearestValley locates the shallowest saddle between this peak and the closest
// other peak on either side (used to flag overlap and propose cut points).
func nearestValley(dtg []DTGPoint, k, lo, hi int) (int, float64) {
	bestIdx := -1
	best := math.Inf(-1)
	consider := func(a, b int) {
		for x := a; x <= b; x++ {
			if dtg[x].DtgMgPerSec < 0 && dtg[x].DtgMgPerSec > best {
				best = dtg[x].DtgMgPerSec
				bestIdx = x
			}
		}
	}
	// find extent between neighboring minima
	left := lo
	for x := k - 1; x > lo; x-- {
		if dtg[x].DtgMgPerSec <= dtg[x-1].DtgMgPerSec && dtg[x].DtgMgPerSec <= dtg[x+1].DtgMgPerSec {
			left = x
			break
		}
	}
	right := hi
	for x := k + 1; x < hi; x++ {
		if dtg[x].DtgMgPerSec <= dtg[x-1].DtgMgPerSec && dtg[x].DtgMgPerSec <= dtg[x+1].DtgMgPerSec {
			right = x
			break
		}
	}
	consider(left, k)
	consider(k, right)
	if bestIdx < 0 {
		return -1, 0
	}
	return bestIdx, dtg[bestIdx].TimeSec
}

func (e *Engine) peakArtifacts(cs *model.Artifact, dtgArts []*model.Artifact, params *model.Params) []*model.Artifact {
	dtg := dtgArts[0].Data["points"].([]DTGPoint)
	threshold := num(params.Values["min_prominence_mg_per_s"], 0.0005)
	peaks := detectPeaks(dtg, threshold)
	out := make([]*model.Artifact, 0, len(peaks))
	for i, pk := range peaks {
		a := &model.Artifact{
			Kind: "dtg_peak", Title: fmt.Sprintf("DTG 峰 P%d", i+1),
			Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
			Sources:    []model.SourceRef{artRef(dtgArts[0], "dtg_series")},
			CreatedSeq: cs.CreatedSeq, CreatedAt: cs.CreatedAt,
		}
		a.ID = e.hashID("dtg_peak", dtgArts[0].ID, fmt.Sprintf("%.0f", pk.TimeSec))
		a.Quantities = []model.Quantity{
			{Name: "t", Label: "峰时间", Value: pk.TimeSec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s", pk.TimeSec)},
			{Name: "T", Label: "峰温", Value: pk.TempC, Unit: "°C", State: "ok", Display: fmt.Sprintf("%.1f °C", pk.TempC)},
			{Name: "rate", Label: "最大失重速率", Value: pk.RateMgPerSec, Unit: "mg/s", State: "ok", Display: fmt.Sprintf("%.5f mg/s", pk.RateMgPerSec)},
			{Name: "prom", Label: "显著性", Value: pk.Prominence, Unit: "mg/s", State: "ok", Display: fmt.Sprintf("%.5f mg/s", pk.Prominence)},
		}
		if pk.ValleyIndex >= 0 {
			a.Quantities = append(a.Quantities, model.Quantity{Name: "saddle", Label: "相邻鞍点", Value: pk.ValleyTimeSec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s（与邻峰重叠）", pk.ValleyTimeSec)})
			a.Diags = append(a.Diags, model.Diag{Code: "OVERLAPPING_PEAK", Severity: model.SevInfo, Message: "该峰与相邻峰未回到基线，存在重叠，阶段边界为多候选"})
		}
		a.Steps = []model.Step{{Label: "检出", Formula: fmt.Sprintf("prominence ≥ %.5f mg/s", threshold), Result: "负向局部极小；分块扫描，缺口不连接"}}
		a.Data = map[string]any{
			"index": pk.Index, "time_sec": pk.TimeSec, "temp_c": pk.TempC,
			"rate": pk.RateMgPerSec, "prominence": pk.Prominence,
			"saddle_time_sec": pk.ValleyTimeSec, "overlap": pk.ValleyIndex >= 0,
		}
		out = append(out, a)
	}
	return out
}

// boundary is one stage cut between two peaks (may have multiple variants).
type boundary struct {
	BetweenPeakI int
	Variant      string // saddle | midway | d2zero
	TimeSec      float64
	TempC        float64
	Note         string
}

// proposeBoundaries turns peak list into 1-3 legal boundary variants per gap.
func proposeBoundaries(dtg []DTGPoint, peaks []peak) [][]boundary {
	var groups [][]boundary
	for i := 0; i+1 < len(peaks); i++ {
		p1, p2 := peaks[i], peaks[i+1]
		var bs []boundary
		// 1) saddle between the peaks
		if si := saddleBetween(dtg, p1.Index, p2.Index); si >= 0 {
			bs = append(bs, boundary{BetweenPeakI: i, Variant: "saddle", TimeSec: dtg[si].TimeSec, TempC: dtg[si].TempC,
				Note: "两峰间最浅点（常用切分）"})
		}
		// 2) midpoint in time
		mt := (p1.TimeSec + p2.TimeSec) / 2
		bs = append(bs, boundary{BetweenPeakI: i, Variant: "midway", TimeSec: mt, TempC: tempAt(dtg, mt),
			Note: "两峰时间中点"})
		// 3) if overlap is weak (rate returns near 0 at saddle), the split is
		//    effectively unique -> keep saddle only; otherwise a third variant
		//    at the local curvature change is offered.
		if si := saddleBetween(dtg, p1.Index, p2.Index); si >= 0 {
			if math.Abs(dtg[si].DtgMgPerSec) > 0.35*math.Min(math.Abs(p1.RateMgPerSec), math.Abs(p2.RateMgPerSec)) {
				if di := d2Switch(dtg, p1.Index, p2.Index); di >= 0 {
					bs = append(bs, boundary{BetweenPeakI: i, Variant: "d2zero", TimeSec: dtg[di].TimeSec, TempC: dtg[di].TempC,
						Note: "曲率变号点（重叠峰的另一合法切法）"})
				}
			}
		}
		groups = append(groups, bs)
	}
	return groups
}

func saddleBetween(dtg []DTGPoint, a, b int) int {
	best, bi := math.Inf(-1), -1
	for k := a + 1; k < b; k++ {
		if dtg[k].State != "ok" {
			continue
		}
		if dtg[k].DtgMgPerSec > best {
			best = dtg[k].DtgMgPerSec
			bi = k
		}
	}
	return bi
}

func d2Switch(dtg []DTGPoint, a, b int) int {
	// where second derivative of DTG changes sign (rate of rate change)
	for k := a + 2; k < b-2; k++ {
		if dtg[k-1].State != "ok" || dtg[k+1].State != "ok" {
			continue
		}
		d1 := dtg[k+1].DtgMgPerSec - 2*dtg[k].DtgMgPerSec + dtg[k-1].DtgMgPerSec
		d0 := dtg[k].DtgMgPerSec - 2*dtg[k-1].DtgMgPerSec + dtg[k-2].DtgMgPerSec
		if d0*d1 < 0 {
			return k
		}
	}
	return -1
}

func tempAt(dtg []DTGPoint, t float64) float64 {
	for i := 0; i+1 < len(dtg); i++ {
		if dtg[i].State == "ok" && dtg[i+1].State == "ok" && t >= dtg[i].TimeSec && t <= dtg[i+1].TimeSec {
			f := 0.5
			if dtg[i+1].TimeSec > dtg[i].TimeSec {
				f = (t - dtg[i].TimeSec) / (dtg[i+1].TimeSec - dtg[i].TimeSec)
			}
			return dtg[i].TempC + f*(dtg[i+1].TempC-dtg[i].TempC)
		}
	}
	return 0
}

func massAt(rows []CorrectedPoint, t float64) (float64, bool) {
	for i := 0; i+1 < len(rows); i++ {
		if !rows[i].Valid || !rows[i+1].Valid {
			continue
		}
		if t >= rows[i].TimeSec && t <= rows[i+1].TimeSec {
			f := 0.5
			if rows[i+1].TimeSec > rows[i].TimeSec {
				f = (t - rows[i].TimeSec) / (rows[i+1].TimeSec - rows[i].TimeSec)
			}
			return rows[i].CorrMg + f*(rows[i+1].CorrMg-rows[i].CorrMg), true
		}
	}
	return 0, false
}
