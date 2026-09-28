package derive

import (
	"sort"
	"strings"

	"tgareview/internal/domain"
)

// BuildAtmo normalizes the furnace program and maps every gas valve switch
// onto the sample reaction timeline:
//
//	reaction_t = valve_t + atmo_delay_s        (gas transport delay)
//	sample_T   = furnace_T( valve_t − temp_lag_s) (thermal lag)
//
// Therefore the valve switch instant and the sample reaction instant are
// deliberately different coordinates.
func BuildAtmo(in Inputs, p domain.Params) AtmoResult {
	res := AtmoResult{DelayS: p.AtmoDelayS, LagS: p.TempLagS}
	if in.Furnace != nil {
		for _, row := range in.Furnace.Rows {
			if !row.Accepted {
				continue
			}
			s, e, tg, r := row.Cells["start"], row.Cells["end"], row.Cells["target"], row.Cells["rate"]
			if s.Valid && e.Valid {
				seg := FurnaceSeg{StartS: s.Value, EndS: e.Value}
				if tg.Valid {
					seg.TargetC = tg.Value
				}
				if r.Valid {
					seg.Rate = r.Value
				}
				res.Segment = append(res.Segment, seg)
			}
		}
	}
	sort.SliceStable(res.Segment, func(i, j int) bool { return res.Segment[i].StartS < res.Segment[j].StartS })

	if in.Gas != nil {
		for _, row := range in.Gas.Rows {
			if !row.Accepted {
				continue
			}
			t := row.Cells["t"]
			if !t.Valid {
				continue
			}
			ev := AtmoEvent{
				Line: row.Line, ValveTS: r3(t.Value), Gas: strings.ToUpper(strings.TrimSpace(row.Raw["gas"])),
			}
			if f := row.Cells["flow"]; f.Valid {
				ev.Flow = f.Value
			}
			ev.ReactionTS = r3(t.Value + p.AtmoDelayS)
			ev.FurnaceTemp = r3(furnaceTemp(res.Segment, nil, t.Value))
			ev.SampleTemp = r3(furnaceTemp(res.Segment, nil, t.Value-p.TempLagS))
			ev.Note = "阀在炉时钟 " + f3(t.Value) + "s 切换；样品在反应时钟 " +
				f3(ev.ReactionTS) + "s 才经历新气氛（延迟 " + f3(p.AtmoDelayS) +
				"s），该处样品温度约 " + f3(ev.SampleTemp) + "°C（滞后 " + f3(p.TempLagS) + "s）"
			res.Events = append(res.Events, ev)
		}
	}
	sort.SliceStable(res.Events, func(i, j int) bool { return res.Events[i].ValveTS < res.Events[j].ValveTS })

	res.Chain = []domain.ChainNode{
		{Step: "1 炉程序", Detail: "分段 [start,end] 规范化为 s，目标温度 °C、升温速率 °C/min；原始表示保留"},
		{Step: "2 阀事件", Detail: "气体阀切换时间保持炉时钟坐标，不与样品反应时刻混用", Value: in.sourceID(in.Gas)},
		{Step: "3 气氛延迟", Detail: "reaction_t = valve_t + atmo_delay_s（气体输运延迟）", Value: f3(p.AtmoDelayS) + " s"},
		{Step: "4 温度滞后", Detail: "sample_T = furnace_T(valve_t − temp_lag_s)（炉体-样品热滞后）", Value: f3(p.TempLagS) + " s"},
		{Step: "5 坐标基准", Detail: "结果同时给炉时钟与反应时钟两个坐标；阀切换时间 ≠ 样品反应时间"},
	}
	return res
}

func (in Inputs) sourceID(s *domain.Source) string {
	if s == nil {
		return "缺失"
	}
	return s.ID
}
