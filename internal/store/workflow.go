package store

import (
	"encoding/json"
	"fmt"
	"sort"

	"tga-review/internal/core"
)

// Bundle 是导入包：六类层可以同批提交，单层失败不影响其他层。
type Bundle struct {
	Layers []BundleLayer `json:"layers"`
}
type BundleLayer struct {
	Kind   string          `json:"kind"`
	Format string          `json:"format"`
	Raw    json.RawMessage `json:"raw"` // 保留原始表示（原样 JSON 文本）
}

// RejectedInput 记录导入中被拒绝的层及原因。
type RejectedInput struct {
	Kind    string `json:"kind"`
	Format  string `json:"format"`
	Error   string `json:"error"`
	Preview string `json:"preview"`
}

// ImportResult 是一次导入包处理的完整结果（成功与失败都保留）。
type ImportResult struct {
	Accepted []*core.Layer  `json:"accepted"`
	Rejected []RejectedInput `json:"rejected"`
}

// ImportBundle 提交六类层。原始输入一经接受不可覆盖；修订需走新版本。
// 单层格式错误只拒绝该层，其余层照常落库，并写 import_partial_failure。
func (s *Store) ImportBundle(projectID string, b *Bundle) (*ImportResult, error) {
	res := &ImportResult{}
	for _, bl := range b.Layers {
		if !validKind(bl.Kind) {
			res.Rejected = append(res.Rejected, RejectedInput{Kind: bl.Kind, Format: bl.Format,
				Error: "未知层类型", Preview: preview(bl.Raw)})
			continue
		}
		rawText := string(bl.Raw)
		var version int
		var parentID string
		if latest, _ := s.latestLayer(projectID, bl.Kind); latest != nil {
			version = latest.Version + 1
			parentID = latest.ID
		}
		l := &core.Layer{
			ID: s.nextID("lay"), ProjectID: projectID, Kind: bl.Kind, Version: version + 1,
			ParentID: parentID, Format: bl.Format, Raw: rawText, SubmittedAt: s.clk.Now(),
		}
		norm, diags, err := core.Normalize(l)
		_ = norm
		if err != nil {
			l.Status = core.StatusRejected
			l.Diagnostics = []core.Diagnostic{{LayerID: l.ID, Field: "_layer", Raw: preview(bl.Raw), Message: err.Error()}}
			if pErr := s.PutLayer(l); pErr != nil {
				return nil, pErr
			}
			if _, e := s.AppendEvent(projectID, core.EvLayerRejected, map[string]any{
				"layer_id": l.ID, "kind": l.Kind, "error": err.Error(), "format": bl.Format,
			}); e != nil {
				return nil, e
			}
			res.Rejected = append(res.Rejected, RejectedInput{Kind: bl.Kind, Format: bl.Format,
				Error: err.Error(), Preview: preview(bl.Raw)})
			continue
		}
		l.Status = core.StatusAccepted
		l.Diagnostics = diags
		if err := s.PutLayer(l); err != nil {
			return nil, err
		}
		if _, err := s.AppendEvent(projectID, core.EvLayerImported, map[string]any{
			"layer_id": l.ID, "kind": l.Kind, "version": l.Version,
			"parent_id": parentID, "diagnostics": len(diags),
		}); err != nil {
			return nil, err
		}
		res.Accepted = append(res.Accepted, l)
	}
	if len(res.Rejected) > 0 {
		acc := make([]map[string]any, 0, len(res.Accepted))
		for _, l := range res.Accepted {
			acc = append(acc, map[string]any{"layer_id": l.ID, "kind": l.Kind, "version": l.Version})
		}
		if _, err := s.AppendEvent(projectID, core.EvImportPartialFailure, map[string]any{
			"accepted": acc, "rejected": res.Rejected,
			"note":     "导入半失败：接受层与拒绝层分别保留，未发生整批回滚",
		}); err != nil {
			return nil, err
		}
	}
	return res, nil
}

func validKind(k string) bool {
	for _, kk := range core.LayerKinds {
		if k == kk {
			return true
		}
	}
	return false
}

func preview(r json.RawMessage) string {
	s := string(r)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func (s *Store) latestLayer(project, kind string) (*core.Layer, error) {
	layers, err := s.Layers(project)
	if err != nil {
		return nil, err
	}
	var latest *core.Layer
	for _, l := range layers {
		if l.Kind == kind && l.Status == core.StatusAccepted && (latest == nil || l.Version > latest.Version) {
			latest = l
		}
	}
	return latest, nil
}

// RunDerivation 用当前各层最新版本派生全部结果；结果带算法版本、参数指纹与直接来源。
func (s *Store) RunDerivation(projectID string, p core.Params) ([]*core.Result, []*core.StageCandidate, error) {
	norm := &core.NormSet{Srcs: map[string]core.SourceRef{}}
	layers := map[string]*core.Layer{}
	for _, k := range core.LayerKinds {
		l, err := s.latestLayer(projectID, k)
		if err != nil {
			return nil, nil, err
		}
		if l == nil {
			continue
		}
		n, _, err := core.Normalize(l)
		if err != nil {
			return nil, nil, fmt.Errorf("%s 最新版本无法规范化: %w", k, err)
		}
		layers[k] = l
		norm.Srcs[k] = core.SourceRef{LayerID: l.ID, Kind: k, Version: l.Version}
		switch k {
		case core.LayerRawSamples:
			norm.Samples = n.(*core.SampleSeries)
		case core.LayerDrift:
			norm.Drift = n.(*core.DriftCurve)
		case core.LayerFurnaceProgram:
			norm.Program = n.(*core.FurnaceProgram)
		case core.LayerGasEvents:
			norm.Gas = n.(*core.GasEventList)
		case core.LayerBlankCurve:
			norm.Blank = n.(*core.BlankCurve)
		case core.LayerInitialMass:
			norm.Mass = n.(*core.InitialMass)
		}
	}
	out, err := core.Derive(norm, p)
	if err != nil {
		return nil, nil, err
	}
	seq, err := s.peekNextSeq()
	if err != nil {
		return nil, nil, err
	}
	refs := func(ks ...string) []core.SourceRef {
		var rs []core.SourceRef
		for _, k := range ks {
			if r, ok := norm.Srcs[k]; ok {
				rs = append(rs, r)
			}
		}
		return rs
	}
	put := func(kind string, payload any, inputKinds []string) *core.Result {
		b, _ := json.Marshal(payload)
		r := &core.Result{
			ID: s.nextID(resPrefix(kind)), ProjectID: projectID, Kind: kind,
			AlgoVersion: p.AlgoVersion, ParamFP: p.Fingerprint(),
			Inputs: refs(inputKinds...), Payload: b, CreatedSeq: seq,
		}
		if err := s.PutResult(r); err != nil {
			panic(err)
		}
		return r
	}
	rCorr := put(core.ResultCorrectedCurve, out.Corrected,
		[]string{core.LayerRawSamples, core.LayerDrift, core.LayerBlankCurve})
	rDTG := put(core.ResultDTG, out.DTG,
		[]string{core.LayerRawSamples, core.LayerDrift, core.LayerBlankCurve, core.LayerInitialMass})
	rGap := put(core.ResultGapPolicy, out.GapPolicies, []string{core.LayerRawSamples})
	rAtm := put(core.ResultAtmosphere, out.Atmosphere, []string{core.LayerGasEvents})
	rRes := put(core.ResultResidue, out.Residue,
		[]string{core.LayerRawSamples, core.LayerDrift, core.LayerBlankCurve, core.LayerInitialMass})

	var stageResults []*core.Result
	var cands []*core.StageCandidate
	if len(out.Candidates) > 0 {
		type stagePayload struct {
			Candidates []core.StageCandidate `json:"candidates"`
		}
		var ids []string
		var specList []core.StageCandidate
		for i, spec := range out.Candidates {
			c := &core.StageCandidate{
				ID: s.nextID("cand"), ResultID: "", ProjectID: projectID,
				Label: spec.Label, Stages: spec.Stages, TotalLossPct: spec.TotalLossPct,
				Verdict: spec.Verdict, RejectReason: spec.RejectReason,
			}
			if spec.Verdict == "rejected" {
				c.DecidedSeq = seq
			}
			specList = append(specList, *c)
			ids = append(ids, c.ID)
		}
		rStg := put(core.ResultStageSet, specList,
			[]string{core.LayerRawSamples, core.LayerDrift, core.LayerBlankCurve, core.LayerInitialMass})
		for i := range specList {
			specList[i].ResultID = rStg.ID
		}
		stageResults = append(stageResults, rStg)
		for i := range specList {
			c := specList[i]
			if err := s.PutCandidate(&c); err != nil {
				return nil, nil, err
			}
			if _, err := s.AppendEvent(projectID, core.EvCandidateGenerated, map[string]any{
				"candidate_id": c.ID, "result_id": rStg.ID,
				"label": c.Label, "verdict": c.Verdict,
				"reject_reason": c.RejectReason, "index": i,
			}); err != nil {
				return nil, nil, err
			}
			cands = append(cands, &c)
		}
		_ = ids
	}
	// 机器可读取的判断依据事件。
	if len(out.Atmosphere) > 0 {
		if _, err := s.AppendEvent(projectID, core.EvAtmosphereJudgment, map[string]any{
			"assessments": out.Atmosphere,
			"judgment":    "阀切换时间不等于样品反应时间；阶段可重叠且不强制总和为100%",
		}); err != nil {
			return nil, nil, err
		}
	}
	if len(out.GapPolicies) > 0 {
		if _, err := s.AppendEvent(projectID, core.EvGapPolicy, map[string]any{
			"gaps":   out.GapPolicies,
			"policy": "no_interpolation: 数据缺口不跨越插值，缺口两侧不生成阶段边界",
		}); err != nil {
			return nil, nil, err
		}
	}
	var all []*core.Result
	all = append(all, rCorr, rDTG, rGap, rAtm, rRes)
	all = append(all, stageResults...)
	idList := make([]string, 0, len(all))
	for _, r := range all {
		idList = append(idList, r.ID)
	}
	if _, err := s.AppendEvent(projectID, core.EvDerivationRun, map[string]any{
		"result_ids": idList, "algo_version": p.AlgoVersion, "param_fp": p.Fingerprint(),
	}); err != nil {
		return nil, nil, err
	}
	return all, cands, nil
}

// peekNextSeq 返回下一个事件序号（不提交）。
func (s *Store) peekNextSeq() (int64, error) {
	var seq int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&seq)
	return seq, err
}

func resPrefix(kind string) string {
	switch kind {
	case core.ResultCorrectedCurve:
		return "cor"
	case core.ResultDTG:
		return "dtg"
	case core.ResultStageSet:
		return "stg"
	case core.ResultResidue:
		return "res"
	case core.ResultAtmosphere:
		return "atm"
	case core.ResultGapPolicy:
		return "gap"
	}
	return "rs"
}

// Adjudicate 记录接受/否定/参数调整；与既有裁定冲突时写 conflict_recorded。
func (s *Store) Adjudicate(projectID, candidateID, verdict, reason, author, note string, paramsSnapshot *core.Params) error {
	view, err := s.ViewAt(projectID, 0)
	if err != nil {
		return err
	}
	var current *core.StageCandidate
	for _, c := range view.Candidates {
		if c.ID == candidateID {
			current = c
		}
	}
	if current == nil {
		return fmt.Errorf("候选 %s 不存在", candidateID)
	}
	conflicting := false
	if (verdict == "accepted" || verdict == "rejected") && current.Verdict != "pending" && current.Verdict != verdict {
		conflicting = true
	}
	payload := map[string]any{
		"candidate_id": candidateID, "verdict": verdict, "reason": reason,
		"author": author, "note": note, "previous": current.Verdict,
	}
	if paramsSnapshot != nil {
		payload["params_fp"] = paramsSnapshot.Fingerprint()
		payload["params"] = *paramsSnapshot
	}
	ev, err := s.AppendEvent(projectID, core.EvAdjudication, payload)
	if err != nil {
		return err
	}
	if conflicting {
		if _, err := s.AppendEvent(projectID, core.EvConflict, map[string]any{
			"candidate_id": candidateID,
			"prior_verdict": current.Verdict, "new_verdict": verdict,
			"prior_decided_seq": current.DecidedSeq, "conflicting_seq": ev.Seq,
			"detail":            "两次互相冲突的人工决定均保留，等待解决",
		}); err != nil {
			return err
		}
	}
	return s.UpdateCandidateVerdict(candidateID, verdict, reason, ev.Seq)
}

// Revert 回到任意旧事件：追加 revert 并移动头指针；旧结果重新成为可见结论。
func (s *Store) Revert(projectID string, targetSeq int64) error {
	if _, err := s.AppendEvent(projectID, core.EvRevert, map[string]any{
		"target_seq": targetSeq,
		"note":       "回到旧事件恢复当时的依赖版本与可见结论，最新结果不覆盖历史",
	}); err != nil {
		return err
	}
	return s.SetHead(projectID, targetSeq)
}
