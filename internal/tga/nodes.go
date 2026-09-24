package tga

import (
	"fmt"
	"strings"
)

func rawNode(raw []RawSample, source Source, hash string, version int) Node {
	node := Node{Key: "derived:raw:samples", Kind: "raw_samples", Version: version, DisplayID: "RAW-SMP", Status: "current", Label: "原始采样点（原始表示分层）", Algorithm: "passthrough/v1", ParameterHash: hash, RawSamples: raw, CoordinateBasis: "炉时间；原始单位随字段保留"}
	if source.ID != "" {
		node.DirectSources = []string{source.ID}
		node.Chain = []string{source.ID}
	}
	return node
}

func correctedNode(samples []CorrectedSample, sources map[string]Source, hash string, version int) Node {
	node := Node{Key: "derived:samples:corrected", Kind: "corrected_samples", Version: version, DisplayID: "TG-CURVE", Status: "current", Label: "漂移/空白校正后的 TG 与 DTG", Unit: "mg / °C / %", CoordinateBasis: "炉时间 min；温度 °C", Algorithm: AlgorithmCandidates, ParameterHash: hash, Samples: samples}
	node.DirectSources, node.Chain = sourceChain(sources, KindSamples, KindDrift, KindBlank, KindInitialMass)
	return node
}

func furnaceNode(segments []FurnaceSegment, source Source, hash string, version int) Node {
	node := Node{Key: "derived:furnace:program", Kind: "furnace_program", Version: version, DisplayID: "FURNACE", Status: "current", Label: "炉程序", Unit: "min / °C", CoordinateBasis: "炉时间 min", Algorithm: "furnace-segments/v1", ParameterHash: hash, Furnace: segments}
	if source.ID != "" {
		node.DirectSources = []string{source.ID}
		node.Chain = []string{source.ID}
	}
	return node
}

func gasNode(gas []GasInterval, source Source, params Params, hash string, version int) Node {
	node := Node{Key: "derived:gas:timeline", Kind: "gas_timeline", Version: version, DisplayID: "GAS-LINE", Status: "current", Label: fmt.Sprintf("气体阀时间线（滞后 %.0f s）", params.GasDelayS+params.ThermalLagS), Unit: "min", CoordinateBasis: "阀时间与样品反应时间并列；炉时间 min", Algorithm: "gas-delay/v1", ParameterHash: hash, GasTimeline: gas}
	if source.ID != "" {
		node.DirectSources = []string{source.ID}
		node.Chain = []string{source.ID}
	}
	return node
}

func candidateNode(candidates []Candidate, sources map[string]Source, hash string, version int, judgments []Judgment) Node {
	node := Node{Key: "derived:stages:candidates", Kind: "stage_candidates", Version: version, DisplayID: "STAGE-SET", Status: "current", Label: "阶段候选（允许重叠，不强求总和 100%）", Unit: "min / °C / %", CoordinateBasis: "反应时间；炉时间 min", Algorithm: AlgorithmCandidates, ParameterHash: hash, Candidates: candidates, Diagnoses: filterJudgments(judgments, "overlapping", "stage_", "gap_", "non_")}
	node.DirectSources, node.Chain = sourceChain(sources, KindSamples, KindDrift, KindBlank, KindInitialMass, KindGas, KindFurnace)
	return node
}

func residueNode(residue float64, candidates []Candidate, initial initialMass, samples []CorrectedSample, sources map[string]Source, hash string, version int) Node {
	node := Node{Key: "derived:residue:final", Kind: "final_residue", Version: version, DisplayID: "RES-FINAL", Status: "current", Label: "最终残留率", Algorithm: "final-residue/v1", ParameterHash: hash, CoordinateBasis: "净样品质量 / 分层保存的样品初重", Diagnoses: []Judgment{}}
	if initial.valid && len(samples) > 0 {
		node.Value = formatFloat(residue, 3)
		node.Unit = "%（相对于初重）"
		last := samples[len(samples)-1]
		node.Chain = []string{sources[KindInitialMass].ID, "derived:samples:corrected", fmt.Sprintf("sample:%d", last.Index)}
	} else {
		node.Unknown = true
		node.Status = "unknown"
		node.Value = "unknown"
		node.Error = "初重或校正样品曲线缺失，残留率保持未知。"
	}
	for _, candidate := range candidates {
		node.Chain = append(node.Chain, "candidate:"+candidate.ID)
	}
	node.DirectSources, _ = sourceChain(sources, KindSamples, KindInitialMass, KindDrift, KindBlank)
	return node
}

func sourceChain(sources map[string]Source, kinds ...string) ([]string, []string) {
	ids := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		if source := sources[kind]; source.ID != "" && source.Status != "failed" {
			ids = append(ids, source.ID)
		}
	}
	return append([]string{}, ids...), append([]string{}, ids...)
}

func filterJudgments(items []Judgment, prefixes ...string) []Judgment {
	out := []Judgment{}
	for _, item := range items {
		for _, prefix := range prefixes {
			if strings.HasPrefix(item.Code, prefix) {
				out = append(out, item)
				break
			}
		}
	}
	return out
}
