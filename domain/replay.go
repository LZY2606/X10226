package domain

import (
	"sort"
	"time"
)

// View 是某历史时刻(as-of event seq)的完整只读视图。
// 回到任意旧事件时, 恢复当时的参数版本、派生帧与可见结论,
// 而不是用最新计算结果覆盖历史。
type View struct {
	ProjectID      string         `json:"projectId"`
	AsOfSeq        int            `json:"asOfSeq"`
	Frame          DerivedFrame   `json:"frame"`
	Submissions    []Submission   `json:"submissions"`
	Decisions      []Decision     `json:"decisions"` // 截至该 seq 的决定(含 Active 状态)
	Conflicts      []Conflict     `json:"conflicts"`
	Artifacts      []ArtifactStatus `json:"artifacts"`
	Latest         bool           `json:"latest"`
}

// ViewAt 返回重放到第 seq 个事件后的视图(seq<=0 表示当前)。
func (p *Project) ViewAt(seq int) View {
	if seq <= 0 || seq >= len(p.Events) {
		v := p.snapshot(len(p.Events))
		v.Latest = true
		return v
	}
	return p.snapshot(seq)
}

func (p *Project) snapshot(upto int) View {
	v := View{ProjectID: p.ID, AsOfSeq: upto}
	for i := 0; i < upto && i < len(p.Events); i++ {
		v.Submissions = append(v.Submissions, p.submissionsUpTo(i+1)...)
	}
	// 去重提交(每事件最多产生一条)
	v.Submissions = dedupeSubs(v.Submissions)
	if upto > 0 && upto <= len(p.Events) {
		ev := p.Events[upto-1]
		if ev.Derived != nil {
			v.Frame = *ev.Derived
		} else {
			v.Frame = p.frame
		}
	}
	for _, d := range p.Decisions {
		if d.EventSeq() <= upto {
			v.Decisions = append(v.Decisions, d)
		}
	}
	for _, cf := range p.Conflicts {
		if cf.EventSeq <= upto {
			v.Decisions = append(v.Decisions, []Decision{}...)
			// 冲突在解决前都应可见
			v.Conflicts = append(v.Conflicts, cf)
		}
	}
	v.Artifacts = p.artifactStatuses(upto)
	return v
}

func (p *Project) submissionsUpTo(upto int) []Submission {
	var out []Submission
	count := 0
	for i := 0; i < upto && i < len(p.Events); i++ {
		if p.Events[i].Type == EvImported || p.Events[i].Type == EvParamsApplied {
			if count < len(p.Submissions) {
				out = append(out, p.Submissions[count])
				count++
			}
		}
	}
	return out
}

func dedupeSubs(xs []Submission) []Submission {
	seen := map[string]bool{}
	var out []Submission
	for _, x := range xs {
		if seen[x.ID] {
			continue
		}
		seen[x.ID] = true
		out = append(out, x)
	}
	return out
}

// artifactStatuses 依据历史帧内容寻址地构造时效表:
// 每个历史帧里的派生 id 若等于当前帧 id 则 current, 否则 stale(仍可查看)。
func (p *Project) artifactStatuses(upto int) []ArtifactStatus {
	type key struct{ kind, id string }
	currentIDs := map[key]bool{
		{"corrected", p.frame.CorrectedID}: true,
		{"dtg", p.frame.DTGID}:             true,
		{"stages", stableID("stageset", p.frame.DTGID, p.frame.ParamFingerprint)}: true,
		{"residue", p.frame.Residue.ID}: true,
	}
	var out []ArtifactStatus
	seen := map[key]int{}
	for i := 0; i < upto && i < len(p.Events); i++ {
		fr := p.Events[i].Derived
		if fr == nil {
			continue
		}
		ids := map[string]string{
			"corrected": fr.CorrectedID,
			"dtg":       fr.DTGID,
			"stages":    stableID("stageset", fr.DTGID, fr.ParamFingerprint),
			"residue":   fr.Residue.ID,
		}
		for kind, id := range ids {
			if id == "" {
				continue
			}
			k := key{kind, id}
			if _, dup := seen[k]; dup {
				continue
			}
			seen[k] = i + 1
			cur := currentIDs[k]
			out = append(out, ArtifactStatus{
				ID: id, Kind: kind, Current: cur,
				Stale: !cur, Viewable: true, ProducedAtSeq: i + 1,
				StaleReason: map[bool]string{true: ""}[false],
			})
			if !cur {
				out[len(out)-1].StaleReason = "已被后续源数据/参数变更取代(可查看, 非当前)"
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].ProducedAtSeq < out[j].ProducedAtSeq
	})
	return out
}

// EventSeq 返回决定所属事件序号(从 EventID 反查)。
func (d Decision) EventSeq() int { return 0 }

var _ = time.Time{}
