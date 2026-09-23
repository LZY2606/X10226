package service

import (
	"encoding/json"

	"tga-lineage/internal/domain"
)

func summarize(e domain.Event) string {
	var pl domain.Payloads
	_ = json.Unmarshal(e.Payload, &pl)
	switch e.Kind {
	case domain.EvProjectCreated:
		return "创建项目：" + pl.Name
	case domain.EvFragmentAdded:
		if pl.Fragment != nil {
			s := "导入分层来源 " + string(pl.Fragment.Kind) + " v" + itoa(pl.Fragment.Version) + "（" + pl.Fragment.Filename + "）"
			if !pl.Fragment.Imported {
				s += "——半失败，部分行隔离"
			}
			return s
		}
	case domain.EvParamSet:
		if pl.ParamSet != nil {
			return "参数修订 v" + itoa(pl.ParamSet.Version) + "（指纹 " + pl.ParamSet.Fingerprint + "）" + reasonSuffix(pl.ParamSet.Reason)
		}
	case domain.EvDerived:
		if pl.Run != nil {
			return "派生重算 #" + itoa(pl.Run.Number) + "，" + itoa(len(pl.Run.Nodes)) + " 个结果，" + itoa(len(pl.Run.StaleIDs)) + " 个过期"
		}
	case domain.EvDecision:
		if pl.Decision != nil {
			s := "人工决定 #" + itoa(pl.Decision.ID) + "：" + pl.Decision.Action + " 节点 " + pl.Decision.NodeID
			if pl.Decision.Adjust != nil {
				s += "（调整窗口）"
			}
			return s + reasonSuffix(pl.Decision.Reason)
		}
	case domain.EvDecisionUndo:
		if pl.Undo != nil {
			return "撤销决定 #" + itoa(pl.Undo.DecisionID) + reasonSuffix(pl.Undo.Reason)
		}
	case domain.EvConflict:
		if pl.Conflict != nil {
			return "冲突裁定：" + pl.Conflict.Winner + " 胜出" + reasonSuffix(pl.Conflict.Reason)
		}
	case domain.EvNote:
		return "备注：" + pl.Text
	}
	return string(e.Kind)
}

func reasonSuffix(r string) string {
	if r == "" {
		return ""
	}
	return "——" + r
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

// Chain returns the expandable calculation chain for a node id, from raw
// sources through every derivation step to the displayed value.
func (p *Project) Chain(nodeID string) []domain.ChainItem {
	root, ok := p.findNode(nodeID)
	if !ok {
		return nil
	}
	visited := map[string]bool{}
	var walk func(n *domain.Node, role string) []domain.ChainItem
	walk = func(n *domain.Node, role string) []domain.ChainItem {
		if n == nil || visited[n.ID] {
			return nil
		}
		visited[n.ID] = true
		var items []domain.ChainItem
		for _, r := range n.Direct {
			if child, ok := p.findNode(r.ID); ok {
				items = append(items, walk(&child, r.Role)...)
			} else {
				// direct source that lives inside a fragment layer
				items = append(items, p.fragmentChainItem(r)...)
			}
		}
		ci := domain.ChainItem{Node: n, Role: role, Formula: formulaFor(n)}
		if n.Kind == "derived/residue" {
			ci.Note = residueNote(*n)
		}
		items = append(items, ci)
		return items
	}
	items := walk(&root, "result")
	// de-duplicate consecutive identical nodes
	var out []domain.ChainItem
	seen := map[string]bool{}
	for _, it := range items {
		if seen[it.Node.ID] && it.Node.Kind != "derived/residue" {
			continue
		}
		seen[it.Node.ID] = true
		out = append(out, it)
	}
	return out
}

func (p *Project) fragmentChainItem(r domain.NodeRef) []domain.ChainItem {
	// refs to raw layer nodes always exist (source/* nodes); nothing to add.
	return nil
}

func (p *Project) findNode(id string) (domain.Node, bool) {
	if n, ok := p.current[id]; ok {
		return n, true
	}
	for _, m := range p.manualNodes {
		if m.ID == id {
			return m, true
		}
	}
	for _, run := range p.runs {
		for _, n := range p.runNodes[run.Number] {
			if n.ID == id {
				return n, true
			}
		}
	}
	return domain.Node{}, false
}

func formulaFor(n *domain.Node) string {
	switch n.Kind {
	case "derived/blank_corrected":
		return "m_blank_corr(t) = m_raw(t) − m_blank(t)"
	case "derived/drift_curve":
		return "d(t) = Σ cᵢ·tⁱ（平台段最小二乘）"
	case "derived/drift_corrected":
		return "m(t) = m_blank_corr(t) − d(t)"
	case "derived/smoothed":
		return "m̄(t) = mean{m(t): |t′−t| ≤ window/2}，仅在同一有效段内"
	case "derived/dtg":
		return "DTG(t) = −dm̄/dt × 60，单位 mg/min，中心差分且不跨缺口"
	case "stage/candidate", "stage/manual":
		return "Δm = m(t_start) − m(t_end)；Δm% = Δm / m₀ × 100（缺口端点为未知）"
	case "derived/residue":
		return "residue% = m(t_end) / m₀(初重现行版本) × 100"
	}
	return ""
}

func residueNote(n domain.Node) string {
	if n.Status == "unknown" {
		return "残留率处于未知状态：初重或末点质量不可得，未以插值/外推填充。"
	}
	return "阶段百分比不强制求和为 100%；重叠阶段会重复计质量，剩余项单列。"
}
