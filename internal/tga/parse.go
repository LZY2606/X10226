package tga

import (
	"fmt"
	"math"
	"sort"
)

type driftCurve struct{ startUg, endUg float64; specified bool }

type blankCurve struct{ points []blankPoint }
type blankPoint struct{ timeMin, massMg float64 }

type initialMass struct {
	massMg float64
	valid  bool
	raw    string
}

func parseSamples(source Source, judgments *[]Judgment) ([]RawSample, []RawSample) {
	raw := []RawSample{}
	valid := []RawSample{}
	if source.ID == "" {
		*judgments = append(*judgments, judgment("missing_samples", "error", "原始采样点层缺失，无法规范化样品曲线。"))
		return raw, valid
	}
	items := payloadArray(source.Payload, "points")
	for index, item := range items {
		point := payloadObject(item)
		rawPoint := RawSample{Index: index + 1, Present: payloadBool(point, "present", true)}
		if !rawPoint.Present {
			raw = append(raw, rawPoint)
			*judgments = append(*judgments, judgment("sampling_gap", "warning", fmt.Sprintf("第 %d 个采样点缺失；缺口两侧不做插值。", rawPoint.Index), "derived:samples:corrected"))
			continue
		}
		rawPoint.FurnaceTime = quantityFromMap(point, "furnace_time")
		rawPoint.Temperature = quantityFromMap(point, "temperature")
		rawPoint.Mass = quantityFromMap(point, "mass")
		rawPoint.FurnaceTime = normalizeTime(rawPoint.FurnaceTime)
		rawPoint.Temperature = normalizeTemperature(rawPoint.Temperature)
		rawPoint.Mass = normalizeMass(rawPoint.Mass)
		raw = append(raw, rawPoint)
		if rawPoint.FurnaceTime.Valid && rawPoint.Temperature.Valid && rawPoint.Mass.Valid {
			valid = append(valid, rawPoint)
		} else {
			*judgments = append(*judgments, judgment("unconvertible_value", "warning", fmt.Sprintf("第 %d 个采样点保留原值，但不进入派生曲线。", rawPoint.Index), "raw:"+source.ID))
		}
	}
	sort.SliceStable(valid, func(i, j int) bool { return valid[i].FurnaceTime.Value < valid[j].FurnaceTime.Value })
	return raw, valid
}

func parseDrift(source Source, params Params, judgments *[]Judgment) driftCurve {
	curve := driftCurve{startUg: params.DriftOffsetStartUg, endUg: params.DriftOffsetEndUg}
	if source.ID == "" {
		*judgments = append(*judgments, judgment("missing_balance_drift", "warning", "天平漂移校正层缺失，使用零漂移。"))
		return curve
	}
	if payloadBool(source.Payload, "measured", false) {
		curve.specified = true
		start := quantityFromMap(source.Payload, "offset_start")
		end := quantityFromMap(source.Payload, "offset_end")
		if start.Valid && end.Valid {
			curve.startUg, curve.endUg = start.Value, end.Value
			return curve
		}
	}
	return curve
}

func parseFurnace(source Source, judgments *[]Judgment) []FurnaceSegment {
	if source.ID == "" {
		*judgments = append(*judgments, judgment("missing_furnace_program", "error", "炉程序缺失。"))
		return nil
	}
	raw := payloadArray(source.Payload, "segments")
	out := make([]FurnaceSegment, 0, len(raw))
	for _, item := range raw {
		mp := payloadObject(item)
		startT := normalizeTime(quantityFromMap(mp, "start_time"))
		endT := normalizeTime(quantityFromMap(mp, "end_time"))
		startC := normalizeTemperature(quantityFromMap(mp, "start_temperature"))
		endC := normalizeTemperature(quantityFromMap(mp, "end_temperature"))
		if startT.Valid && endT.Valid && startC.Valid && endC.Valid {
			rate := 0.0
			if endT.Value > startT.Value {
				rate = (endC.Value - startC.Value) / (endT.Value - startT.Value)
			}
			out = append(out, FurnaceSegment{StartMin: startT.Value, EndMin: endT.Value, StartC: startC.Value, EndC: endC.Value, RateCPerMin: rate, SourceEventID: source.ID})
		}
	}
	return out
}

func parseGas(source Source, params Params, judgments *[]Judgment) []GasInterval {
	if source.ID == "" {
		*judgments = append(*judgments, judgment("missing_gas_valve_events", "error", "气体阀事件缺失；无法把气氛时间线映射到样品反应。"))
		return nil
	}
	raw := payloadArray(source.Payload, "events")
	out := make([]GasInterval, 0, len(raw))
	lagMin := (params.GasDelayS + params.ThermalLagS) / 60
	for _, item := range raw {
		mp := payloadObject(item)
		at := normalizeTime(quantityFromMap(mp, "valve_time"))
		gas := fmt.Sprint(mp["gas"])
		if at.Valid {
			reaction := at.Value + lagMin
			out = append(out, GasInterval{ValveTimeMin: at.Value, ReactionTimeMin: reaction, Gas: gas, DelayS: params.GasDelayS + params.ThermalLagS, SourceEventID: source.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ValveTimeMin < out[j].ValveTimeMin })
	for i := range out {
		if i+1 < len(out) {
			out[i].EndReactionTimeMin = out[i+1].ReactionTimeMin
		} else {
			out[i].EndReactionTimeMin = math.Inf(1)
		}
	}
	if len(out) > 0 {
		*judgments = append(*judgments, judgment("valve_time_is_not_reaction_time", "info", fmt.Sprintf("阀时间整体增加 %.3g 秒气氛与温度滞后。", lagMin*60), "derived:gas:timeline"))
	}
	return out
}

func parseBlank(source Source, samples []RawSample, judgments *[]Judgment) blankCurve {
	if source.ID == "" {
	*judgments = append(*judgments, judgment("missing_blank_curve", "warning", "空白曲线缺失，空白贡献按 0 mg 处理。"))
		return blankCurve{}
	}
	raw := payloadArray(source.Payload, "points")
	points := make([]blankPoint, 0, len(raw))
	for _, item := range raw {
		mp := payloadObject(item)
		at := normalizeTime(quantityFromMap(mp, "time"))
		mass := normalizeMass(quantityFromMap(mp, "mass"))
		if at.Valid && mass.Valid {
			points = append(points, blankPoint{timeMin: at.Value, massMg: mass.Value})
		}
	}
	return blankCurve{points: points}
}

func parseInitialMass(source Source, judgments *[]Judgment) initialMass {
	if source.ID == "" {
		*judgments = append(*judgments, judgment("missing_initial_mass", "error", "样品初重分层缺失，残留率不可计算。"))
		return initialMass{}
	}
	q := normalizeMass(quantityFromMap(source.Payload, "mass"))
	if !q.Valid {
		*judgments = append(*judgments, judgment("unconvertible_initial_mass", "error", "初重无法转换；原始表示保留在输入层。", "raw:"+source.ID))
	}
	return initialMass{massMg: q.Value, valid: q.Valid, raw: q.Raw}
}
