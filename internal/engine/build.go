package engine

import (
	"encoding/json"
	"fmt"

	"tgaline/internal/domain"
)

// BuildNodes is the complete deterministic derived set for one parameter set.
type BuildNodes struct {
	Corrected      *domain.Node
	Furnace        *domain.Node
	Gap            *domain.Node
	Noise          *domain.Node
	DTG            *domain.Node
	TG             *domain.Node
	Peaks          *domain.Node
	Reaction       *domain.Node
	Stages         *domain.Node
	Residues       *domain.Node
	Series         *domain.Series
	DTGRate        []float64
	DTGOK          []bool
	FurnaceUnknown int
	BuildID        string
}

func layerRefs(cur domain.CurrentLayers, only ...domain.LayerKind) []domain.SourceRef {
	var out []domain.SourceRef
	want := map[domain.LayerKind]bool{}
	for _, k := range only {
		want[k] = true
	}
	for _, k := range domain.AllLayerKinds() {
		if len(want) > 0 && !want[k] {
			continue
		}
		if l := cur[k]; l != nil {
			out = append(out, domain.SourceRef{
				Kind: "layer", ID: l.ID, Version: l.Version,
				Detail: fmt.Sprintf("%s v%d (%s)", k.Title(), l.Version, l.Origin),
			})
		}
	}
	return out
}

// ComputeBuild runs the full pipeline. Pure function of layers + params.
func ComputeBuild(cur domain.CurrentLayers, params map[string]float64, buildID string) BuildNodes {
	in := extractInput(cur)
	fp := fingerprintParams(params)

	mk := func(kind domain.NodeKind, body any, sources []domain.SourceRef, notes ...string) *domain.Node {
		id := nodeID(kind, []any{string(kind), AlgoVersion, fp, sources, body})
		return &domain.Node{
			ID: id, Kind: kind, AlgoVersion: AlgoVersion, ParamsFP: fp,
			DirectSources: sources, Body: mustJSON(body), Status: "ok", Notes: notes,
		}
	}

	furnaceSeries, fUnknown := furnaceTrace(in)
	fsrc := layerRefs(cur, domain.LayerFurnace, domain.LayerSample)
	furnaceNode := mk(domain.NodeFurnaceTrace, furnaceSeries, fsrc)
	if fUnknown > 0 {
		furnaceNode.Status = "unknown"
		furnaceNode.Notes = append(furnaceNode.Notes,
			fmt.Sprintf("%d 个样品时刻落在炉程序支撑范围外，保持未知，不外推", fUnknown))
	}

	corr := buildCorrected(in, params)
	corrSrc := layerRefs(cur, domain.LayerSample, domain.LayerDrift, domain.LayerBlank)
	correctedNode := mk(domain.NodeCorrectedSeries, corr.series, corrSrc)

	gapBody := map[string]any{"gap_after": corr.series.GapAfter, "gap_count": corr.gapCount}
	gapNode := mk(domain.NodeGapMask, gapBody,
		[]domain.SourceRef{
			{Kind: "layer", ID: safeLayerID(cur, domain.LayerSample), Detail: "sample"},
			{Kind: "params", ID: fp, Detail: "gap_min_s"},
		})
	if corr.gapCount > 0 {
		gapNode.Notes = append(gapNode.Notes,
			"检测到采样缺口；任何派生量都不跨越缺口插值")
	}

	noiseNode := mk(domain.NodeNoiseReport, corr.noise,
		[]domain.SourceRef{{Kind: "node", ID: correctedNode.ID},
			{Kind: "params", ID: fp, Detail: "noise_jump_mg,noise_neighbors"}})

	dtgRate := buildDTG(corr.series, params)
	dtgOK := dtgMask(corr.series, dtgRate, params)
	dtgSeries := &domain.Series{
		TimeS: corr.series.TimeS, RateMgMin: dtgRate, Mask: dtgOK,
		GapAfter: corr.series.GapAfter,
	}
	dtgNode := mk(domain.NodeDTG, dtgSeries,
		[]domain.SourceRef{{Kind: "node", ID: correctedNode.ID},
			{Kind: "params", ID: fp, Detail: "dtg_window_s"}})

	// TG normalized against m0
	tgSeries := &domain.Series{TimeS: corr.series.TimeS, TK: corr.series.TK,
		MassMG: corr.series.MassMG, Mask: corr.series.Mask, GapAfter: corr.series.GapAfter}
	tgSrc := []domain.SourceRef{{Kind: "node", ID: correctedNode.ID}}
	tgNotes := []string{}
	if in.m0 != nil {
		tgSrc = append(tgSrc, domain.SourceRef{Kind: "layer", ID: in.m0.layerID,
			Detail: fmt.Sprintf("initial mass %.4f mg source=%s", in.m0.mg, in.m0.source)})
	} else {
		tgNodeStatus(&tgNotes)
	}
	tgNode := mk(domain.NodeTGNormalized, tgSeries, tgSrc, tgNotes...)

	pf := findPeaks(corr.series, dtgRate, dtgOK, params)
	peaksNode := mk(domain.NodePeakSet, map[string]any{
		"peaks": pf.peaks, "groups": pf.groups,
	}, []domain.SourceRef{{Kind: "node", ID: dtgNode.ID},
		{Kind: "params", ID: fp, Detail: "peak_*"}})

	rw := reactionWindows(in.gas, params)
	reactionNode := mk(domain.NodeReactionWindows, rw,
		append(layerRefs(cur, domain.LayerGas),
			domain.SourceRef{Kind: "params", ID: fp, Detail: "atmo_delay_s,atmo_lag_s"}))

	sr := stageCandidates(corr.series, pf, rw, in)
	stagesNode := mk(domain.NodeStageCandidates, map[string]any{
		"candidates": sr.candidates,
		"rule":       "阶段可重叠；不强制各阶段百分比之和为 100%；缺口内失重不插值",
	}, []domain.SourceRef{{Kind: "node", ID: peaksNode.ID},
		{Kind: "node", ID: reactionNode.ID}, {Kind: "node", ID: gapNode.ID},
		{Kind: "node", ID: correctedNode.ID}})

	rr := finalResidue(corr.series, in, params, correctedNode.ID, gapNode.ID, noiseNode.ID, tgNode.ID)
	residuesNode := mk(domain.NodeResidues, map[string]any{"residues": rr.residues},
		[]domain.SourceRef{{Kind: "node", ID: tgNode.ID}, {Kind: "node", ID: gapNode.ID},
			{Kind: "node", ID: correctedNode.ID}})
	residuesNode.Status = "ok"
	for _, r := range rr.residues {
		if r.Status == "unknown" {
			residuesNode.Status = "unknown"
		}
	}

	return BuildNodes{
		Corrected: correctedNode, Furnace: furnaceNode, Gap: gapNode,
		Noise: noiseNode, DTG: dtgNode, TG: tgNode, Peaks: peaksNode,
		Reaction: reactionNode, Stages: stagesNode, Residues: residuesNode,
		Series: corr.series, DTGRate: dtgRate, DTGOK: dtgOK,
		FurnaceUnknown: fUnknown, BuildID: buildID,
	}
}

func tgNodeStatus(notes *[]string) {
	*notes = append(*notes, "缺少样品初重分层：TG 只能以 mg 表示，无法归一化为分数（不丢弃数据）")
}

func dtgMask(s *domain.Series, rate []float64, p map[string]float64) []bool {
	n := len(s.TimeS)
	ok := make([]bool, n)
	gapSet := map[int]bool{}
	for _, g := range s.GapAfter {
		gapSet[g] = true
	}
	winS := p["dtg_window_s"]
	for i := 0; i < n; i++ {
		lo, hi := i, i
		for lo > 0 && !gapSet[lo-1] && s.Mask[lo-1] && s.TimeS[i]-s.TimeS[lo-1] <= winS/2 {
			lo--
		}
		for hi < n-1 && !gapSet[hi] && s.Mask[hi+1] && s.TimeS[hi+1]-s.TimeS[i] <= winS/2 {
			hi++
		}
		known := 0
		for j := lo; j <= hi; j++ {
			if j > 0 && j < n-1 && !gapSet[j-1] && !gapSet[j] &&
				s.Mask[j-1] && s.Mask[j] && s.Mask[j+1] {
				known++
			}
		}
		ok[i] = known > 0
	}
	return ok
}

// DecodeNodeBody unmarshals a node body into target.
func DecodeNodeBody[T any](n *domain.Node) (T, error) {
	var v T
	err := json.Unmarshal([]byte(n.Body), &v)
	return v, err
}

func safeLayerID(cur domain.CurrentLayers, k domain.LayerKind) string {
	if l := cur[k]; l != nil {
		return l.ID
	}
	return ""
}
