package cases

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

func Generate() Pack {
	const n = 121
	var sample, blank strings.Builder
	sample.WriteString("index\ttime_s\tfurnace_C\tsample_C\tmass_mg\n")
	blank.WriteString("index\ttime_s\tblank_signal_mg\n")
	for i := 0; i < n; i++ {
		t := float64(i) * 10
		furnace := 25 + math.Min(12.5*t/60, 1000)
		if t > 480 {
			furnace = 1000 + math.Min(2.0*(t-480)/60, 20)
		}
		sampleTemp := 25 + math.Max(0, furnace-25)*math.Exp(-45/math.Max(t, 1))
		mass := 10.000
		mass -= 2.1 * logistic(t, 80, 35)
		mass -= 1.15 * logistic(t, 165, 28)
		mass -= 0.95 * logistic(t, 235, 30)
		mass -= 0.72 * logistic(t, 445, 40)
		mass -= 0.42 * logistic(t, 710, 45)
		mass += 0.003 * math.Sin(float64(i)*2.1)
		if i == 67 || i == 68 {
			continue
		}
		sample.WriteString(fmt.Sprintf("%d\t%.1f\t%.3f\t%.3f\t%.5f\n", i+1, t, furnace, sampleTemp, mass))
		b := 0.018*math.Sin(t/130) - 0.006*t/1200
		blank.WriteString(fmt.Sprintf("%d\t%.1f\t%.6f\n", i+1, t, b))
	}
	initial := "batch\tinitial_mass_mg\tbasis\nsample-A\t10.000\ttared balance before atmosphere stabilization\nsample-A-check\t10.012\tpre-run audit re-weigh\n"
	furnace := "segment\tstart_s\tend_s\ttarget_C\trate_C_per_min\nramp-1\t0\t480\t1000\t12.5\nramp-2\t480\t1200\t1020\t2\n"
	gas := "event\ttime_s\tgas\taction\tnote\nv-n2-start\t0\tN2\ton\nbaseline inert\nv-switch-co2\t360\tN2/off CO2/on\tswitch\tvalve command logged at panel\nv-co2-hold\t1200\tCO2\ton\tmaintain\n"
	bad := "index\ttime_s\tmass_mg\n1\tnot-a-time\t10.0\n2\t20\tmissing balance read\n"
	base := PackInputs{SampleTSV: sample.String(), BlankTSV: blank.String(), InitialTSV: initial, FurnaceTSV: furnace, GasTSV: gas, BadTSV: bad}
	variants := []Variant{
		variantAtmosphere(base),
		variantDrift(base),
		variantOverlap(base),
		variantNoise(base),
		variantGap(base),
		variantInitial(base),
		variantHalfFailure(base),
	}
	return Pack{
		ID:        "tga-stage-decomposition-atmosphere-residue-v1",
		Name:      "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Narrative: "热分析团队导入质量、样品温度、炉温、炉时间、空白曲线、初重与气体阀事件；从原始点到残留率逐层复核。",
		Seed:      20260928,
		Baseline:  base,
		Variants:  variants,
		Expectations: []Expectation{
			{Code: "GAP_NO_INTERPOLATION", Want: "缺口两侧不生成跨缺口 DTG 或插值点"},
			{Code: "VALVE_NOT_REACTION", Want: "阀切换窗口与反应窗口同时显示且可不同"},
			{Code: "OVERLAP_NO_100", Want: "重叠阶段保留候选，不强制失重百分比总和为100%"},
			{Code: "LOCAL_STALENESS", Want: "只让直接或传递依赖被改源数据的结果过期"},
			{Code: "IMPORT_PARTIAL", Want: "合法片段分层保留，失败片段以诊断保留并阻断该批导入"},
		},
	}
}

func logistic(t, center, width float64) float64 { return 1 / (1 + math.Exp(-(t-center)/width)) }

func cloneInputs(in PackInputs) PackInputs { return in }

func variantAtmosphere(base PackInputs) Variant {
	return Variant{ID: "atmosphere-delay", Name: "气氛延迟：阀时间不等于反应时间", ChangedDirectSource: "parameter:gas_delay", Inputs: cloneInputs(base),
		ExpectedScope: []string{"analysis.run", "candidate", "residue"}, ProtectedScope: []string{"sample-point", "blank-point", "furnace-segment", "gas-valve-event"},
		ExpectedFindings: []string{"CO2 阀命令仍在 360 s，有效气氛窗口随延迟参数移动", "反应峰不被吸附到阀命令时间"}}
}

func variantDrift(base PackInputs) Variant {
	lines := strings.Split(base.BlankTSV, "\n")
	for i := range lines {
		if strings.HasPrefix(lines[i], "61\t") {
			parts := strings.Split(lines[i], "\t")
			v, _ := strconv.ParseFloat(parts[2], 64)
			parts[2] = strconv.FormatFloat(v+0.08, 'f', 6, 64)
			lines[i] = strings.Join(parts, "\t")
		}
	}
	in := cloneInputs(base)
	in.BlankTSV = strings.Join(lines, "\n")
	return Variant{ID: "baseline-drift", Name: "天平基线漂移：局部空白改变校正", ChangedDirectSource: "batch:blank", Inputs: in,
		ExpectedScope: []string{"analysis.run", "candidate", "residue"}, ProtectedScope: []string{"furnace-segment", "gas-valve-event"},
		ExpectedFindings: []string{"漂移诊断附加到校正质量", "未修改的炉程序和阀事件不过期"}}
}

func perturbSample(base PackInputs, predicate func(int) bool, transform func(string) string) string {
	lines := strings.Split(base.SampleTSV, "\n")
	for i, line := range lines {
		parts := strings.Split(line, "\t")
		if len(parts) == 5 {
			if idx, err := strconv.Atoi(parts[0]); err == nil && predicate(idx) {
				lines[i] = transform(line)
			}
		}
	}
	return strings.Join(lines, "\n")
}

func variantOverlap(base PackInputs) Variant {
	lines := strings.Split(base.SampleTSV, "\n")
	for i, line := range lines {
		p := strings.Split(line, "\t")
		if len(p) == 5 {
			t, _ := strconv.ParseFloat(p[1], 64)
			v, _ := strconv.ParseFloat(p[4], 64)
			p[4] = strconv.FormatFloat(v-0.18*logistic(t, 190, 22), 'f', 5, 64)
			lines[i] = strings.Join(p, "\t")
		}
	}
	in := cloneInputs(base)
	in.SampleTSV = strings.Join(lines, "\n")
	return Variant{ID: "overlapping-peaks", Name: "重叠峰：保留多个合法阶段候选", ChangedDirectSource: "batch:sample", Inputs: in,
		ExpectedScope: []string{"analysis.run", "candidate", "residue"}, ProtectedScope: []string{"blank-point", "furnace-segment", "gas-valve-event"},
		ExpectedFindings: []string{"肩峰与主峰候选可重叠", "拒绝原因或未知状态作为一等数据保留"}}
}

func variantNoise(base PackInputs) Variant {
	lines := strings.Split(base.SampleTSV, "\n")
	for i, line := range lines {
		p := strings.Split(line, "\t")
		if len(p) == 5 {
			idx, _ := strconv.Atoi(p[0])
			if idx >= 90 && idx <= 100 && idx%2 == 0 {
				v, _ := strconv.ParseFloat(p[4], 64)
				p[4] = strconv.FormatFloat(v+0.018, 'f', 5, 64)
				lines[i] = strings.Join(p, "\t")
			}
		}
	}
	in := cloneInputs(base)
	in.SampleTSV = strings.Join(lines, "\n")
	return Variant{ID: "nonmonotonic-noise", Name: "非单调噪声：质量短时回升", ChangedDirectSource: "batch:sample", Inputs: in,
		ExpectedScope: []string{"analysis.run", "candidate", "residue"}, ProtectedScope: []string{"furnace-segment", "gas-valve-event"}, ExpectedFindings: []string{"非单调片段不被当作增重阶段删除", "噪声诊断绑定到采样点"}}
}

func variantGap(base PackInputs) Variant {
	lines := strings.Split(base.SampleTSV, "\n")
	kept := lines[:0]
	for _, line := range lines {
		p := strings.Split(line, "\t")
		if len(p) == 5 {
			idx, _ := strconv.Atoi(p[0])
			if idx >= 54 && idx <= 72 {
				continue
			}
		}
		kept = append(kept, line)
	}
	in := cloneInputs(base)
	in.SampleTSV = strings.Join(kept, "\n")
	return Variant{ID: "sampling-gap", Name: "采样缺口：不跨越插值", ChangedDirectSource: "batch:sample", Inputs: in, ExpectedScope: []string{"analysis.run", "candidate", "residue"}, ProtectedScope: []string{"furnace-segment", "gas-valve-event"}, ExpectedFindings: []string{"DTG 计算在缺口两侧断开", "缺口期间不补点"}}
}

func variantInitial(base PackInputs) Variant {
	in := cloneInputs(base)
	in.InitialTSV = "batch\tinitial_mass_mg\tbasis\nsample-A\t9.986\tpost-import calibrated re-weigh\nsample-A-check\t10.012\tpre-run audit re-weigh\n"
	return Variant{ID: "initial-mass-revision", Name: "初重修订：新版本和父版本并存", ChangedDirectSource: "batch:initial", Inputs: in, ExpectedScope: []string{"analysis.run", "candidate", "residue"}, ProtectedScope: []string{"sample-point", "furnace-segment", "gas-valve-event"}, ExpectedFindings: []string{"原始初重不可覆盖", "残留率以最新采纳初重版本为直接来源"}}
}

func variantHalfFailure(base PackInputs) Variant {
	in := cloneInputs(base)
	in.BadTSV = base.BadTSV + "3\t30\t9.99\n3\tnan-ish\t9.98\n"
	return Variant{ID: "import-half-failure", Name: "导入半失败：成功片段与失败诊断同在", ChangedDirectSource: "batch:malformed", Inputs: in, ExpectedScope: []string{"import.batch"}, ProtectedScope: []string{"analysis.run", "candidate", "residue"}, ExpectedFindings: []string{"格式错误不覆盖已提交输入", "半失败批状态为 partial_failed"}}
}
