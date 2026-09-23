package compute

import (
	"encoding/json"
	"fmt"
	"sort"

	"tga-review/internal/core"
	"tga-review/internal/importer"
	"tga-review/internal/quantity"
)

// DocEntry 是某类文档的当前版本（原文+解析结果）。
type DocEntry struct {
	Doc    *core.Document
	Parsed *importer.ParsedDoc
}

// DocSet 是各文档种类的当前版本集合。
type DocSet map[core.DocKind]DocEntry

// StageOutput 是一个计算阶段的输出。
type StageOutput struct {
	Stage            string
	InputFingerprint string
	Results          []core.Result
	Diagnostics      []core.Diagnostic
}

// 阶段名称（固定顺序）。
const (
	StageSeries     = "series"     // 规范化+空白/漂移校正+缺口
	StageDTG        = "dtg"        // 微分失重+非单调噪声
	StageAtmosphere = "atmosphere" // 气氛有效时间线（阀时间≠反应时间）
	StageStages     = "stages"     // 阶段候选（可重叠，不强制100%）
	StageResidue    = "residue"    // 最终残留率（含计算链）
)

// StageOrder 给出阶段的固定计算顺序。
var StageOrder = []string{StageSeries, StageDTG, StageAtmosphere, StageStages, StageResidue}

// ComputeAll 计算全部阶段；调用方按输入指纹决定哪些阶段需要落新事件。
func ComputeAll(set DocSet, p Params) []StageOutput {
	ctx := &pipeCtx{set: set, params: p}
	series := ctx.computeSeries()
	dtg := ctx.computeDTG(series)
	atmo := ctx.computeAtmosphere()
	stages := ctx.computeStages(series, dtg, atmo)
	residue := ctx.computeResidue(series)
	return []StageOutput{*series, *dtg, *atmo, *stages, *residue}
}

type pipeCtx struct {
	set    DocSet
	params Params
}

func (c *pipeCtx) srcIDs(kinds ...core.DocKind) []string {
	var ids []string
	for _, k := range kinds {
		if e, ok := c.set[k]; ok {
			ids = append(ids, e.Doc.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func (c *pipeCtx) fingerprint(stage string, srcIDs []string, upstream ...string) string {
	parts := append([]string{stage, AlgoVersion, c.params.Fingerprint()}, srcIDs...)
	parts = append(parts, upstream...)
	return core.ContentID("fp", parts...)
}

func mkResult(stage, kind, fp string, ordinal int, srcIDs []string, p Params, payload any) core.Result {
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte(fmt.Sprintf(`{"error":%q}`, err.Error()))
	}
	return core.Result{
		ID:               core.ResultID(kind, fp, ordinal),
		Kind:             kind,
		Stage:            stage,
		AlgoVersion:      AlgoVersion,
		ParamFingerprint: p.Fingerprint(),
		SourceIDs:        srcIDs,
		Payload:          b,
		Status:           core.StatusCurrent,
	}
}

// ---- series 阶段 ----

// SeriesPoint 是校正后序列的一点；无效点保留在原位置并附诊断。
type SeriesPoint struct {
	T        quantity.Value `json:"t"`
	Temp     quantity.Value `json:"temp"`
	MassRaw  quantity.Value `json:"mass_raw"`
	MassCorr quantity.Value `json:"mass_corr"`
	GapAfter bool           `json:"gap_after,omitempty"`
	Valid    bool           `json:"valid"`
	Diag     string         `json:"diag,omitempty"`
}

// SeriesPayload 是 corrected_series 结果的负载。
type SeriesPayload struct {
	Points    []SeriesPoint `json:"points"`
	MedianDtS float64       `json:"median_dt_s"`
}

func (c *pipeCtx) computeSeries() *StageOutput {
	out := &StageOutput{Stage: StageSeries}
	src := c.srcIDs(core.DocRawSamples, core.DocBlankCurve, core.DocDriftCorrection)
	out.InputFingerprint = c.fingerprint(StageSeries, src)
	entry, ok := c.set[core.DocRawSamples]
	if !ok {
		out.Diagnostics = append(out.Diagnostics, core.Diagnostic{
			Severity: "error", Code: "missing_raw_samples", Message: "缺少原始采样点文档"})
		return out
	}
	pd := entry.Parsed
	blank := c.curve(core.DocBlankCurve, "mass")
	drift := c.curve(core.DocDriftCorrection, "offset")
	var pts []SeriesPoint
	var ts []float64
	for i := range pd.Rows {
		tv := pd.Value(i, "t").To(quantity.UnitSecond)
		tv.Basis = "furnace_time"
		tp := pd.Value(i, "temp").To(quantity.UnitCelsius)
		tp.Basis = "sample_temp"
		mv := pd.Value(i, "mass").To(quantity.UnitMg)
		pt := SeriesPoint{T: tv, Temp: tp, MassRaw: mv}
		if !tv.OK || !tp.OK || !mv.OK {
			pt.Valid = false
			pt.Diag = "原始值不可解析或单位不可转换，保留在原位置"
			pts = append(pts, pt)
			continue
		}
		pt.Valid = true
		corr := mv
		corr.Val = mv.Val - interp(blank, tv.Val) - interp(drift, tv.Val)
		corr.Raw = ""
		pt.MassCorr = corr
		pts = append(pts, pt)
		ts = append(ts, tv.Val)
	}
	// 缺口检测：不跨越插值。
	med := medianDt(ts)
	var gaps []core.Result
	ord := 0
	for i := 0; i+1 < len(pts); i++ {
		if !pts[i].Valid || !pts[i+1].Valid {
			continue
		}
		dt := pts[i+1].T.Val - pts[i].T.Val
		if med > 0 && dt > c.params.GapFactor*med {
			pts[i].GapAfter = true
			gaps = append(gaps, mkResult(StageSeries, "gap", out.InputFingerprint, ord, src, c.params, map[string]any{
				"start_t": pts[i].T, "end_t": pts[i+1].T,
				"note": "采样缺口：两侧分属不同段，不跨越插值",
			}))
			ord++
		}
	}
	sp := SeriesPayload{Points: pts, MedianDtS: med}
	out.Results = append(out.Results, mkResult(StageSeries, "corrected_series", out.InputFingerprint, 0, src, c.params, sp))
	out.Results = append(out.Results, gaps...)
	return out
}

// curve 把两列表格文档解析为 (t_s, value) 折线；t 规范化为秒。
func (c *pipeCtx) curve(kind core.DocKind, valCol string) [][2]float64 {
	entry, ok := c.set[kind]
	if !ok {
		return nil
	}
	pd := entry.Parsed
	var pts [][2]float64
	for i := range pd.Rows {
		tv := pd.Value(i, "t").To(quantity.UnitSecond)
		vv := pd.Value(i, valCol).To(quantity.UnitMg)
		if tv.OK && vv.OK {
			pts = append(pts, [2]float64{tv.Val, vv.Val})
		}
	}
	sort.Slice(pts, func(a, b int) bool { return pts[a][0] < pts[b][0] })
	return pts
}

// interp 分段线性插值；域外取最近端点（不外推）。
func interp(curve [][2]float64, x float64) float64 {
	if len(curve) == 0 {
		return 0
	}
	if x <= curve[0][0] {
		return curve[0][1]
	}
	if x >= curve[len(curve)-1][0] {
		return curve[len(curve)-1][1]
	}
	for i := 0; i+1 < len(curve); i++ {
		if x >= curve[i][0] && x <= curve[i+1][0] {
			span := curve[i+1][0] - curve[i][0]
			if span == 0 {
				return curve[i][1]
			}
			f := (x - curve[i][0]) / span
			return curve[i][1] + f*(curve[i+1][1]-curve[i][1])
		}
	}
	return curve[len(curve)-1][1]
}

func medianDt(ts []float64) float64 {
	if len(ts) < 2 {
		return 0
	}
	var ds []float64
	for i := 0; i+1 < len(ts); i++ {
		if d := ts[i+1] - ts[i]; d > 0 {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 {
		return 0
	}
	sort.Float64s(ds)
	return ds[len(ds)/2]
}
