package domain

import (
	"fmt"
	"sort"
)

// mergeLayers 用新提交中出现的分层覆盖当前投影(原始提交本身不变)。
// 返回受影响(失效)的派生种类。
func (p *Project) mergeLayers(sub Submission) []string {
	affected := map[string]bool{}
	if sub.Present[LayerSamples] {
		p.layers.Samples = append(p.layers.Samples, sub.Layers.Samples...)
		p.layerSource[string(LayerSamples)] = sub.ID
		affected["corrected"] = true
		affected["dtg"] = true
		affected["stages"] = true
		affected["residue"] = true
	}
	if sub.Present[LayerDrift] {
		p.layers.DriftAnchors = append(p.layers.DriftAnchors, sub.Layers.DriftAnchors...)
		p.layerSource[string(LayerDrift)] = sub.ID
		affected["corrected"] = true
		affected["dtg"] = true
		affected["stages"] = true
		affected["residue"] = true
	}
	if sub.Present[LayerBlank] {
		p.layers.Blank = append(p.layers.Blank, sub.Layers.Blank...)
		p.layerSource[string(LayerBlank)] = sub.ID
		affected["corrected"] = true
		affected["dtg"] = true
		affected["stages"] = true
		affected["residue"] = true
	}
	if sub.Present[LayerFurnace] {
		p.layers.FurnaceSegs = append(p.layers.FurnaceSegs, sub.Layers.FurnaceSegs...)
		p.layerSource[string(LayerFurnace)] = sub.ID
		affected["stages"] = true
	}
	if sub.Present[LayerGas] {
		p.layers.GasEvents = append(p.layers.GasEvents, sub.Layers.GasEvents...)
		p.layerSource[string(LayerGas)] = sub.ID
		affected["stages"] = true
	}
	// 局部依赖改动: 初重修订只使残留率过期, 校正/DTG/阶段仍为当前
	if sub.Present[LayerInitialMass] {
		p.layers.InitialMasses = append(p.layers.InitialMasses, sub.Layers.InitialMasses...)
		p.layerSource[string(LayerInitialMass)] = sub.ID
		affected["residue"] = true
	}
	var out []string
	for k := range affected {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (p *Project) applyImport(ev *Event, cmd Command) error {
	if cmd.Text == "" {
		return fmt.Errorf("导入内容为空")
	}
	data, present, title, diags := ParseImport(cmd.Text)
	if data.Empty() && len(data.Params) == 0 {
		status := "rejected"
		_ = status
		return fmt.Errorf("导入被拒绝: 未解析出任何分层(原始文本不予保存为提交)")
	}
	sub := Submission{
		ID:          stableID("sub", p.ID, itoa(len(p.Submissions)+1), hashText(cmd.Text)),
		Seq:         len(p.Submissions) + 1,
		ParentID:    cmd.ParentID,
		RawText:     cmd.Text,
		Layers:      data,
		Present:     present,
		Diagnostics: diags,
		CreatedAt:   ev.Time,
		Title:       firstNonEmpty(cmd.Title, title, "提交 #"+itoa(len(p.Submissions)+1)),
	}
	// 半失败: 解析出了对象但存在诊断 => partial; 否则 accepted
	sub.Status = "accepted"
	if len(diags) > 0 {
		sub.Status = "partial"
	}
	// 若有 params 分层, 作为参数修订处理
	affected := p.mergeLayers(sub)
	p.Submissions = append(p.Submissions, sub)
	p.lastDiags = diags
	// 参数段也随导入应用
	if len(data.Params) > 0 {
		af2 := p.applyRawParams(data.Params, ev)
		affected = unionStr(affected, af2)
	}
	p.recompute(ev, affected)
	ev.Type = EvImported
	ev.Payload["submissionId"] = sub.ID
	ev.Payload["status"] = sub.Status
	ev.Payload["title"] = sub.Title
	ev.Payload["affected"] = affected
	return nil
}

func (p *Project) applyParams(ev *Event, cmd Command) error {
	data, _, _, diags := ParseImport("## params\n" + cmd.Text)
	if len(data.Params) == 0 {
		return fmt.Errorf("没有可识别的参数声明")
	}
	p.lastDiags = diags
	affected := p.applyRawParams(data.Params, ev)
	sub := Submission{
		ID:  stableID("sub", p.ID, itoa(len(p.Submissions)+1), hashText(cmd.Text)),
		Seq: len(p.Submissions) + 1, Kind: "revision",
		Title:   firstNonEmpty(cmd.Title, "参数修订 #"+itoa(len(p.Submissions)+1)),
		RawText: "## params\n" + cmd.Text, Layers: data,
		Present: map[Layer]bool{}, Status: "accepted",
		Diagnostics: diags, CreatedAt: ev.Time,
	}
	p.Submissions = append(p.Submissions, sub)
	p.recompute(ev, affected)
	ev.Type = EvParamsApplied
	ev.Payload["submissionId"] = sub.ID
	ev.Payload["affected"] = affected
	return nil
}

// applyRawParams 把原始参数叠加为新版本(父=当前参数版本), 返回受影响派生。
func (p *Project) applyRawParams(raws []RawParam, ev *Event) []string {
	oldFP := FingerprintParams(p.params)
	np, diags := ResolveParams(p.params, raws)
	p.params = np
	fp := FingerprintParams(np)
	p.parentParamID = p.paramVerID
	p.paramVerID = stableID("paramv", p.paramVerID, fp, itoa(ev.Seq))
	p.lastDiags = append(p.lastDiags, diags...)
	if fp != oldFP {
		return []string{"dtg", "residue", "stages", "corrected"}
	}
	return nil
}

func unionStr(a, b []string) []string {
	m := map[string]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		m[x] = true
	}
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
