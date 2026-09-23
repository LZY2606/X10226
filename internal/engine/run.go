package engine

import (
	"fmt"
	"math"
	"sort"

	"tga-lineage/internal/domain"
)

// Input bundles the current (latest-accepted) fragments plus parameters.
type Input struct {
	Fragments map[domain.FragmentKind]*domain.Fragment
	Params    map[string]domain.Param
}

// Run executes the versioned TGA stage-decomposition algorithm. It is a pure
// function: the same inputs always produce the same nodes.
func Run(in Input) []domain.Node {
	params := in.Params
	if params == nil {
		params = DefaultParams()
	}
	fp := Fingerprint(params)

	sample := in.Fragments[domain.KindSample]
	blank := in.Fragments[domain.KindBlank]
	furnace := in.Fragments[domain.KindFurnace]
	gasF := in.Fragments[domain.KindGas]
	tare := in.Fragments[domain.KindTare]

	var nodes []domain.Node
	add := func(n domain.Node) domain.Node {
		n.Algorithm = AlgorithmVersion
		n.ParamFP = fp
		nodes = append(nodes, n)
		return n
	}

	union := func(sets ...[]string) []string {
		seen := map[string]bool{}
		var out []string
		for _, s := range sets {
			for _, t := range s {
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
		}
		sort.Strings(out)
		return out
	}
	srcTags := func(k domain.FragmentKind) []string {
		if f, ok := in.Fragments[k]; ok {
			return []string{srcRef(k, f.Version)}
		}
		return nil
	}
	paramTag := []string{fmt.Sprintf("params:%s", fp)}
	ref := func(id, kind, role string) domain.NodeRef { return domain.NodeRef{ID: id, Kind: kind, Role: role} }

	// --- 1. layered source series ------------------------------------------------
	sp := pointsFrom(sample, true)
	bp := pointsFrom(blank, false)
	fpPts := pointsFrom(furnace, false)

	var rawSampleID, rawBlankID, furnaceID, sampleTempID, gasID, tareID string
	if sample != nil {
		rawSampleID = nodeID("source/series", string(domain.KindSample), srcTags(domain.KindSample), fp)
		n := seriesNode(rawSampleID, "source/raw_sample", "原始采样点：样品质量（保留原始表示）", "mg", "t_since_run_start", sp, true, nil)
		n.SourceIDs = srcTags(domain.KindSample)
		if !sample.Imported {
			n.Status = "partial"
			n.Rejects = append(n.Rejects, domain.Rejection{Code: "half_failed_import", Message: "部分行未通过校验，已隔离保留并附诊断"})
		} else {
			n.Status = "current"
		}
		add(n)

		sampleTempID = nodeID("source/sample_temp", string(domain.KindSample), srcTags(domain.KindSample), fp)
		tn := sampleTempNode(sampleTempID, sp, nil)
		tn.SourceIDs = srcTags(domain.KindSample)
		tn.Status = "current"
		add(tn)
	}
	if blank != nil {
		rawBlankID = nodeID("source/series", string(domain.KindBlank), srcTags(domain.KindBlank), fp)
		n := seriesNode(rawBlankID, "source/raw_blank", "空白曲线（空坩埚基线）", "mg", "t_since_run_start", bp, false, nil)
		n.SourceIDs = srcTags(domain.KindBlank)
		n.Status = "current"
		add(n)
	}
	if furnace != nil {
		furnaceID = nodeID("source/series", string(domain.KindFurnace), srcTags(domain.KindFurnace), fp)
		n := seriesNode(furnaceID, "source/furnace_program", "炉程序（设定炉温）", "degC", "t_since_run_start", fpPts, false, nil)
		n.SourceIDs = srcTags(domain.KindFurnace)
		n.Status = "current"
		add(n)
	}
	gasDelay := num(params, PGasDelay, 0)
	var gasEvs []gasEvent
	if gasF != nil {
		gasEvs = gasEventsFrom(gasF, gasDelay)
		gasID = nodeID("source/gas", string(domain.KindGas), union(srcTags(domain.KindGas), paramTag), fp)
		timeline := map[string]any{
			"gas_delay_s": gasDelay,
			"valve":       valveView(gasEvs),
			"reaction":    reactionView(gasEvs),
		}
		n := domain.Node{ID: gasID, Kind: "source/gas_timeline", Label: "气体阀事件（阀时间与样品反应时间分列）",
			Basis: "t_since_run_start", Extra: timeline, Direct: nil,
			SourceIDs: union(srcTags(domain.KindGas), paramTag), Status: "current"}
		add(n)
	}
	var tLayers []tareLayer
	var initialMass float64
	var initialRow int
	if tare != nil {
		tLayers = tareLayersFrom(tare)
		var okM bool
		initialMass, initialRow, okM = currentInitialMass(tLayers)
		tareID = nodeID("source/tare", string(domain.KindTare), srcTags(domain.KindTare), fp)
		n := domain.Node{ID: tareID, Kind: "source/initial_mass", Label: "样品初重（分层保存，修订追加新版本）",
			Basis: "tare_layer", Extra: map[string]any{"layers": tareView(tLayers)},
			SourceIDs: srcTags(domain.KindTare), Status: "current"}
		if okM {
			n.Value = &initialMass
			n.Unit = "mg"
			n.Extra["applied_row"] = initialRow
		} else {
			n.Status = "unknown"
			n.Rejects = append(n.Rejects, domain.Rejection{Code: "no_initial_mass", Message: "尚无可用初重，残留率无法归一化"})
		}
		if !tare.Imported {
			n.Rejects = append(n.Rejects, domain.Rejection{Code: "half_failed_import", Message: "初重分层导入部分失败"})
		}
		add(n)
	}

	if sample == nil {
		return nodes
	}

	// --- 2. segmentation, gaps, diagnostics --------------------------------------
	valid := func(p point) bool { return p.ok }
	segs := validSegments(sp, valid)
	gaps := gapList(sp, segs)
	dtMed := medianDelta(sp, segs)
	gapFactor := num(params, PGapFactor, 3)

	okBools := make([]bool, len(sp))
	rawVals := make([]float64, len(sp))
	ts := timesOf(sp)
	for i, p := range sp {
		okBools[i] = p.ok
		rawVals[i] = p.v
	}

	corrTags := union(srcTags(domain.KindSample), srcTags(domain.KindBlank), paramTag)
	corrRefs := []domain.NodeRef{}
	if rawSampleID != "" {
		corrRefs = append(corrRefs, ref(rawSampleID, "source/raw_sample", "sample"))
	}
	if rawBlankID != "" {
		corrRefs = append(corrRefs, ref(rawBlankID, "source/raw_blank", "blank"))
	}

	// --- 3. blank correction ------------------------------------------------------
	blankCorr := makeSeries(len(sp), math.NaN())
	bpValid := []point{}
	for _, p := range bp {
		if p.ok {
			bpValid = append(bpValid, p)
		}
	}
	for i, p := range sp {
		if p.ok && len(bpValid) > 0 {
			blankCorr[i] = p.v - interpLinear(bpValid, p.t)
		} else if p.ok {
			blankCorr[i] = p.v
		}
	}
	blankID := nodeID("derived/blank_corrected", "series", corrTags, fp)
	add(seriesFromFloats(blankID, "derived/blank_corrected", "空白扣除后质量 m_raw − m_blank(t)", "mg", "t_since_run_start", ts, blankCorr, okBools, corrRefs, corrTags))

	// --- 4. baseline drift correction ---------------------------------------------
	order := int(num(params, PDriftOrder, 1))
	if order < 0 {
		order = 0
	}
	if order > 3 {
		order = 3
	}
	fitPts := driftFitPoints(blankCorr, ts, segs)
	coef := polyFit(fitPts, order)
	driftCurve := makeSeries(len(sp), 0)
	driftCorr := makeSeries(len(sp), math.NaN())
	for i := range sp {
		driftCurve[i] = polyEval(coef, ts[i])
		if okBools[i] {
			driftCorr[i] = blankCorr[i] - driftCurve[i]
		}
	}
	driftTags := union(corrTags)
	driftID := nodeID("derived/drift_curve", "poly", driftTags, fp)
	add(seriesFromFloats(driftID, "derived/drift_curve", fmt.Sprintf("天平基线漂移（%d 阶多项式，由平台段拟合）", order), "mg", "t_since_run_start", ts, driftCurve, nil,
		[]domain.NodeRef{ref(blankID, "derived/blank_corrected", "input")}, driftTags))
	corrID := nodeID("derived/drift_corrected", "series", driftTags, fp)
	add(seriesFromFloats(corrID, "derived/drift_corrected", "漂移校正后质量（TG 曲线）", "mg", "t_since_run_start", ts, driftCorr, okBools,
		[]domain.NodeRef{ref(blankID, "derived/blank_corrected", "input"), ref(driftID, "derived/drift_curve", "drift")}, driftTags))

	// --- 5. smoothing, DTG, non-monotonic noise -----------------------------------
	win := num(params, PSmoothWindow, 20)
	sm := smoothWithin(driftCorr, ts, okBools, segs, win)
	smoothID := nodeID("derived/smoothed", "series", driftTags, fp)
	add(seriesFromFloats(smoothID, "derived/smoothed", fmt.Sprintf("段内滑动平均（窗口 %.0f s，不跨缺口）", win), "mg", "t_since_run_start", ts, sm, okBools,
		[]domain.NodeRef{ref(corrID, "derived/drift_corrected", "input")}, driftTags))

	dtgV, _, _ := dtg(ts, sm, segs)
	dtgID := nodeID("derived/dtg", "series", driftTags, fp)
	add(seriesFromFloats(dtgID, "derived/dtg", "DTG = −dm/dt（段内中心差分）", "mg/min", "t_since_run_start", ts, dtgV, okBools,
		[]domain.NodeRef{ref(smoothID, "derived/smoothed", "input")}, driftTags))

	nm := findNonMonotonic(ts, driftCorr, segs)
	nmID := nodeID("derived/nonmonotonic", "markers", driftTags, fp)
	nmNode := seriesFromFloats(nmID, "derived/nonmonotonic", "非单调噪声（局部质量回升，保留不平滑掉）", "mg", "t_since_run_start", ts, nm, okBools,
		[]domain.NodeRef{ref(corrID, "derived/drift_corrected", "input")}, driftTags)
	if countValid(nm) == 0 {
		nmNode.Status = "empty"
		nmNode.Rejects = []domain.Rejection{{Code: "none_found", Message: "未检测到非单调回升"}}
	} else {
		nmNode.Status = "current"
	}
	add(nmNode)

	// --- 6. peaks, gaps, stage candidates -----------------------------------------
	pks := detectPeaks(ts, dtgV, segs, num(params, PPeakProminence, 0.01))
	ovs := findOverlaps(pks)
	stageTags := union(driftTags, srcTags(domain.KindGas), srcTags(domain.KindTare))
	ctx := buildCtx{
		ts: ts, sm: sm, ok: okBools, segs: segs, gaps: gaps, dtMed: dtMed, gapFactor: gapFactor,
		pks: pks, ovs: ovs, gasEvs: gasEvs, fpPts: fpPts, sp: sp,
		initialMass: initialMass, initialRow: initialRow,
		stageTags: stageTags, fp: fp,
		refs: map[string]domain.NodeRef{
			"dtg":    ref(dtgID, "derived/dtg", "dtg"),
			"tg":     ref(corrID, "derived/drift_corrected", "tg"),
			"gas":    ref(gasID, "source/gas_timeline", "atmosphere"),
			"tare":   ref(tareID, "source/initial_mass", "initial_mass"),
			"sampleTemp": ref(sampleTempID, "source/sample_temp", "sample_temp"),
			"furnace": ref(furnaceID, "source/furnace_program", "furnace_setpoint"),
		},
	}

	// gaps node
	gapID := nodeID("derived/gaps", "gaps", driftTags, fp)
	gapNode := domain.Node{ID: gapID, Kind: "derived/sampling_gaps", Label: "采样缺口（不跨缺口插值）",
		Basis: "t_since_run_start", Extra: map[string]any{
			"gaps":       gapsView(gaps),
			"median_dt":  dtMed,
			"gap_factor": gapFactor,
		},
		Direct: []domain.NodeRef{ref(corrID, "derived/drift_corrected", "input")}, SourceIDs: driftTags, Status: "current"}
	if len(gaps) == 0 {
		gapNode.Status = "empty"
		gapNode.Rejects = []domain.Rejection{{Code: "no_gap", Message: "未检测到采样缺口"}}
	}
	add(gapNode)

	// peaks node
	peaksID := nodeID("derived/peaks", "peaks", driftTags, fp)
	add(peaksNode(peaksID, pks, ts, ctx))

	// stages
	stageIDs := buildStages(&ctx, add)

	// residue + coverage
	residueID := nodeID("derived/residue", "final", stageTags, fp)
	add(residueNode(residueID, ctx, stageIDs))

	// quality/diagnostics node
	qualID := nodeID("derived/import_quality", "quality", union(srcTags(domain.KindSample), srcTags(domain.KindBlank), srcTags(domain.KindFurnace), srcTags(domain.KindGas), srcTags(domain.KindTare)), fp)
	add(qualityNode(qualID, in.Fragments, gaps, countValid(nm)))

	return nodes
}

func countValid(v []float64) int {
	n := 0
	for _, x := range v {
		if !math.IsNaN(x) {
			n++
		}
	}
	return n
}
