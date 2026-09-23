package domain

import "fmt"

func (p *Project) findCandidate(id string) (StageCandidate, bool) {
	for _, c := range p.frame.Stages.Candidates {
		if c.ID == id {
			return c, true
		}
	}
	return StageCandidate{}, false
}

// applyDecision 处理接受/否定/参数调整。决定固化当时候选快照与参数指纹,
// 因此回到该事件时恢复的是当时版本, 而不是最新计算结果。
func (p *Project) applyDecision(ev *Event, cmd Command) error {
	if cmd.CandidateID == "" {
		return fmt.Errorf("裁定必须指定候选 id")
	}
	cand, ok := p.findCandidate(cmd.CandidateID)
	if !ok {
		return fmt.Errorf("当前派生结果中没有候选 %q (过期候选请在历史帧中查看)", cmd.CandidateID)
	}
	if cmd.Decision != "accept" && cmd.Decision != "reject" && cmd.Decision != "adjust" {
		return fmt.Errorf("决定必须是 accept/reject/adjust")
	}
	// 参数调整: 先产生一次参数修订事件语义(同一事件内重算), 旧候选随之过期,
	// 但决定记录的是调整前快照 + 调整键值。
	if cmd.Decision == "adjust" {
		if cmd.AdjustKey == "" || cmd.AdjustRaw == "" {
			return fmt.Errorf("adjust 需要 adjustKey 和 adjustRaw")
		}
		raw := []RawParam{{Line: 0, Key: cmd.AdjustKey, RawText: cmd.AdjustRaw,
			Note: "人工参数调整"}}
		affected := p.applyRawParams(raw, ev)
		p.recompute(ev, affected)
	}
	d := Decision{
		ID:      stableID("dec", ev.ID, cmd.CandidateID, cmd.Decision),
		EventID: ev.ID, CandidateID: cmd.CandidateID, Action: cmd.Decision,
		Rationale:   cmd.Rationale,
		Candidate:   cand,
		ParamFinger: p.frame.ParamFingerprint,
		ResidueID:   p.frame.Residue.ID,
		Active:      true,
		Time:        ev.Time,
	}
	p.Decisions = append(p.Decisions, d)

	// 冲突检测: 同一峰组内两个 legal 候选同时被接受 => 开放冲突
	if cmd.Decision == "accept" {
		var prior []string
		for _, od := range p.Decisions {
			if od.Active && od.Action == "accept" && od.CandidateID != cand.ID {
				if oc, ok := p.findHistoricalCandidate(od.CandidateID); ok {
					if oc.PeakGroup == cand.PeakGroup && oc.ID != cand.ID {
						prior = append(prior, od.CandidateID)
					}
				}
			}
		}
		if len(prior) > 0 {
			cf := Conflict{
				ID:           stableID("conf", ev.ID, cmd.CandidateID),
				CandidateIDs: append(append([]string{}, prior...), cand.ID),
				Message: fmt.Sprintf("峰组 %d 的互斥候选被同时接受(合并/拆分不能同时成立)",
					cand.PeakGroup),
				Open: true, Time: ev.Time,
			}
			p.Conflicts = append(p.Conflicts, cf)
			ev.Payload["conflictId"] = cf.ID
		}
	}
	ev.Type = EvDecision
	ev.Payload["candidateId"] = cmd.CandidateID
	ev.Payload["decision"] = cmd.Decision
	ev.Payload["decisionId"] = d.ID
	ev.Payload["rationale"] = cmd.Rationale
	if cmd.Decision == "adjust" {
		ev.Payload["adjustKey"] = cmd.AdjustKey
		ev.Payload["adjustRaw"] = cmd.AdjustRaw
	}
	fr := copyFrame(p.frame)
	ev.Derived = &fr
	return nil
}

// findHistoricalCandidate 在当前帧找不到时, 搜索历史事件快照(过期仍可裁定引用)。
func (p *Project) findHistoricalCandidate(id string) (StageCandidate, bool) {
	if c, ok := p.findCandidate(id); ok {
		return c, true
	}
	for i := len(p.Events) - 1; i >= 0; i-- {
		if p.Events[i].Derived != nil {
			for _, c := range p.Events[i].Derived.Stages.Candidates {
				if c.ID == id {
					return c, true
				}
			}
		}
	}
	return StageCandidate{}, false
}

// applyResolve 解决冲突: 指定获胜候选, 其他被接受候选标记为 superseded。
func (p *Project) applyResolve(ev *Event, cmd Command) error {
	var cf *Conflict
	for i := range p.Conflicts {
		if p.Conflicts[i].ID == cmd.ConflictID && p.Conflicts[i].Open {
			cf = &p.Conflicts[i]
			break
		}
	}
	if cf == nil {
		return fmt.Errorf("未找到开放冲突 %q", cmd.ConflictID)
	}
	winnerOK := false
	for _, id := range cf.CandidateIDs {
		if id == cmd.Winner {
			winnerOK = true
		}
	}
	if !winnerOK {
		return fmt.Errorf("获胜候选必须属于该冲突")
	}
	cf.Open = false
	cf.ResolvedBy = ev.ID
	cf.Winner = cmd.Winner
	for i := range p.Decisions {
		d := &p.Decisions[i]
		if !d.Active || d.Action != "accept" {
			continue
		}
		belongs := false
		for _, id := range cf.CandidateIDs {
			if d.CandidateID == id {
				belongs = true
			}
		}
		if belongs && d.CandidateID != cmd.Winner {
			d.SupersededBy = ev.ID
			d.Active = false
		}
	}
	ev.Type = EvConflictResolved
	ev.Payload["conflictId"] = cf.ID
	ev.Payload["winner"] = cmd.Winner
	fr := copyFrame(p.frame)
	ev.Derived = &fr
	return nil
}

// applyUndo 撤销一次决定(冲突解决也可撤销, 重新开放冲突)。
func (p *Project) applyUndo(ev *Event, cmd Command) error {
	target := -1
	for i := range p.Decisions {
		if p.Decisions[i].EventID == cmd.UndoEventID ||
			p.Decisions[i].ID == cmd.UndoEventID {
			target = i
			break
		}
	}
	if target >= 0 {
		if !p.Decisions[target].Active {
			return fmt.Errorf("该决定已被撤销或已在冲突解决中被取代")
		}
		p.Decisions[target].Active = false
		p.Decisions[target].UndoneBy = ev.ID
		ev.Type = EvUndone
		ev.Payload["undoEventId"] = cmd.UndoEventID
		ev.Payload["kind"] = "decision"
		fr := copyFrame(p.frame)
		ev.Derived = &fr
		return nil
	}
	// 撤销冲突解决 => 重开冲突并恢复被取代的接受决定
	for i := range p.Conflicts {
		cf := &p.Conflicts[i]
		if cf.ResolvedBy == cmd.UndoEventID {
			cf.Open = true
			resolvedBy := cf.ResolvedBy
			cf.ResolvedBy = ""
			cf.Winner = ""
			for j := range p.Decisions {
				if p.Decisions[j].SupersededBy == resolvedBy {
					p.Decisions[j].SupersededBy = ""
					p.Decisions[j].Active = true
				}
			}
			ev.Type = EvUndone
			ev.Payload["undoEventId"] = cmd.UndoEventID
			ev.Payload["kind"] = "conflict_resolution"
			fr := copyFrame(p.frame)
			ev.Derived = &fr
			return nil
		}
	}
	return fmt.Errorf("未找到可撤销的事件 %q", cmd.UndoEventID)
}
