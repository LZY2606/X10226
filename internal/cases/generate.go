// Package cases builds the repository-bundled offline TGA case pack.
// All synthetic material is deterministic (fixed seed): raw sample points,
// balance drift, furnace program, gas valve events, blank curve, initial
// mass, plus the seven counterexample variants.
package cases

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"tga-review/internal/domain"
)

const (
	// Mutation names (also the machine-readable tags kept in history).
	MutGasDelay = "atmosphere_delay"
	MutDrift    = "balance_baseline_drift"
	MutOverlap  = "overlapping_peaks"
	MutNonMono  = "non_monotonic_noise"
	MutGap      = "sampling_gap"
	MutInitMass = "initial_mass_revision"
	MutPartial  = "import_partial_failure"
)

// AllMutations is the fixed acceptance list.
var AllMutations = []string{
	MutGasDelay, MutDrift, MutOverlap, MutNonMono,
	MutGap, MutInitMass, MutPartial,
}

// lcg is a tiny deterministic RNG (fixed seed, no global rand state).
type lcg struct{ state uint64 }

func newLCG(seed int) *lcg { return &lcg{state: uint64(seed) + 1} }
func (r *lcg) float64() float64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407
	return float64(r.state>>11) / float64(1<<53)
}
func (r *lcg) normal(sigma float64) float64 {
	// Irwin-Hall centered, scaled — deterministic and bounded.
	sum := 0.0
	for i := 0; i < 12; i++ {
		sum += r.float64()
	}
	return (sum - 6) * sigma / 3
}

type lossStep struct{ t0, w, amp float64 }

func (s lossStep) loss(t float64) float64 { return s.amp / (1 + math.Exp(-(t-s.t0)/s.w)) }

type sampleOpts struct {
	seedName   string
	steps      []lossStep
	driftRate  float64
	noiseSigma float64
	gapWindow  [2]float64 // skip points in this time window (0 = off)
	spikes     map[float64]float64
	badRows    map[int]string // line (1-based data line) -> malformed raw text
}

func baseOpts() sampleOpts {
	return sampleOpts{
		seedName:   "base",
		driftRate:  0.000020,
		noiseSigma: 0.012,
		steps: []lossStep{
			{300, 26, 0.5},
			{800, 42, 3.0},
			{1105, 20, 0.8},
			{1225, 19, 0.7},
		},
		spikes:  map[float64]float64{},
		badRows: map[int]string{},
	}
}

const (
	tEnd           = 1800.0
	dtSample       = 10.0
	m0BaseMg       = 10.0
	driftIntercept = 0.0012 // mg
)

func furnaceTemp(t float64) float64 {
	switch {
	case t <= 60:
		return 30
	case t <= 1740:
		return 30 + 770*(t-60)/(1740-60)
	default:
		return 800
	}
}

func totalLoss(o sampleOpts, t float64) float64 {
	l := 0.0
	for _, s := range o.steps {
		l += s.loss(t)
	}
	return l
}

// Point is a generated raw sample (domain coordinates, before text export).
type Point struct {
	Line      int
	TimeS     float64
	TempC     float64
	MassMg    float64
	Malformed string
}

func generatePoints(o sampleOpts) []Point {
	rng := newLCG(domain.MutationID("TGA-CASE-" + o.seedName))
	var pts []Point
	line := 1
	for t := 0.0; t <= tEnd+0.01; t += dtSample {
		if o.gapWindow[1] > 0 && t >= o.gapWindow[0] && t <= o.gapWindow[1] {
			line++
			continue
		}
		if bad, ok := o.badRows[line]; ok {
			pts = append(pts, Point{Line: line, Malformed: bad})
			line++
			continue
		}
		m := m0BaseMg - totalLoss(o, t)
		m += driftIntercept + o.driftRate*t
		if o.noiseSigma > 0 {
			m += rng.normal(o.noiseSigma)
		}
		if spike, ok := o.spikes[math.Round(t)]; ok {
			m += spike
		}
		pts = append(pts, Point{Line: line, TimeS: t, TempC: furnaceTemp(t), MassMg: m})
		line++
	}
	return pts
}

func generateBlank(seedName string) [][2]float64 {
	rng := newLCG(domain.MutationID("TGA-BLANK-" + seedName))
	var out [][2]float64
	for t := 0.0; t <= tEnd+0.01; t += 30 {
		out = append(out, [2]float64{t, driftIntercept + 0.000020*t + rng.normal(0.002)})
	}
	return out
}

// Layer files and descriptions of a packed case variant.
type Variant struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Changes     string            `json:"layer"`
	Note        string            `json:"note"`
	ExpectScope []string          `json:"expect_affected_layers"`
	Files       map[string]string `json:"-"`
}

// Variants builds the base and all mutation variants.
func Variants() (map[string]*Variant, error) {
	out := map[string]*Variant{}

	mk := func(name, title, layer, note string, expect []string, mutate func(*sampleOpts)) (*Variant, error) {
		o := baseOpts()
		o.seedName = name
		if mutate != nil {
			mutate(&o)
		}
		v := &Variant{Name: name, Title: title, Changes: layer, Note: note,
			ExpectScope: expect, Files: map[string]string{}}
		v.Files["raw_points.csv"] = writePointsCSV(generatePoints(o))
		v.Files["blank_curve.csv"] = writeBlankCSV(generateBlank(name))
		v.Files["furnace_program.csv"] = furnaceCSV
		v.Files["gas_valve.csv"] = valveCSV
		v.Files["initial_mass.csv"] = "mass,basis\n10.000 mg,tared\n"
		v.Files["params.json"] = writeParams(domain.DefaultParams())
		return v, nil
	}

	var err error
	out["base"], err = mk("base", "基线版本：完整六层源数据", "all",
		"原始采样点、天平漂移校正、炉程序、气体阀事件、空白曲线与样品初重分层保存", nil, nil)
	if err != nil {
		return nil, err
	}

	out[MutGasDelay], err = mk(MutGasDelay, "反例：气氛延迟 + 温度滞后", "params",
		"阀切换时间不等于样品反应时间；仅改参数层，候选/归因应局部过期",
		[]string{"params"}, func(o *sampleOpts) {
			// handled via params.json override below
		})
	if err != nil {
		return nil, err
	}
	pp := domain.DefaultParams()
	pp.GasDelayS = 90
	pp.TempLagS = 35
	out[MutGasDelay].Files["params.json"] = writeParams(pp)

	out[MutDrift], err = mk(MutDrift, "反例：天平基线漂移记录被修订", "balance_drift",
		"以一条漂移记录替代空白拟合（仅漂移层直接来源变化）",
		[]string{"balance_drift"}, nil)
	if err != nil {
		return nil, err
	}
	out[MutDrift].Files["balance_drift.csv"] =
		"model,slope_mg_per_s,intercept_mg,note\nblank_linear,0.000080,0.0012,人工修订的较大漂移\n"

	out[MutOverlap], err = mk(MutOverlap, "反例：重叠峰（双峰靠近）", "raw_points",
		"空气中两个氧化峰靠近，合并/拆分均为合法候选，阶段可重叠且不强制和为100%",
		[]string{"raw_points"}, func(o *sampleOpts) {
			o.steps[2] = lossStep{1130, 22, 0.8}
			o.steps[3] = lossStep{1185, 20, 0.7}
		})
	if err != nil {
		return nil, err
	}

	out[MutNonMono], err = mk(MutNonMono, "反例：非单调噪声", "raw_points",
		"失重段中出现质量上跳，保留原值并标注，不得抹平",
		[]string{"raw_points"}, func(o *sampleOpts) {
			o.spikes[600] = 0.22
			o.spikes[1320] = 0.18
		})
	if err != nil {
		return nil, err
	}

	out[MutGap], err = mk(MutGap, "反例：采样缺口", "raw_points",
		"920–1010 s 采样缺失；缺口不跨越插值，DTG/候选分段计算",
		[]string{"raw_points"}, func(o *sampleOpts) {
			o.gapWindow = [2]float64{920, 1010}
		})
	if err != nil {
		return nil, err
	}

	out[MutInitMass], err = mk(MutInitMass, "反例：初重修订", "initial_mass",
		"初重由 10.000 mg 修订为 10.042 mg（新版本+父版本，不覆盖原件）",
		[]string{"initial_mass"}, nil)
	if err != nil {
		return nil, err
	}
	out[MutInitMass].Files["initial_mass.csv"] = "mass,basis\n10.042 mg,revised\n"

	out[MutPartial], err = mk(MutPartial, "反例：导入半失败", "raw_points",
		"含不可转换单位与缺列行：好行入库、坏行留诊断，影响仅限原始采样点层",
		[]string{"raw_points"}, func(o *sampleOpts) {
			o.badRows[61] = `600 s,286 °C,bad-value mg`
			o.badRows[62] = `610 s,,6.60 mg`
			o.badRows[63] = `620 s,294 °C,6.55 mg furlong`
		})
	if err != nil {
		return nil, err
	}

	return out, nil
}

func writeParams(p domain.Params) string {
	b, _ := json.MarshalIndent(p, "", "  ")
	return string(b) + "\n"
}

func writePointsCSV(pts []Point) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "time,temp,mass")
	for _, p := range pts {
		if p.Malformed != "" {
			fmt.Fprintln(&b, p.Malformed)
			continue
		}
		fmt.Fprintf(&b, "%g s,%.2f °C,%.4f mg\n", p.TimeS, p.TempC, p.MassMg)
	}
	return b.String()
}

func writeBlankCSV(pts [][2]float64) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "time,mass")
	for _, p := range pts {
		fmt.Fprintf(&b, "%g s,%.5f mg\n", p[0], p[1])
	}
	return b.String()
}

const furnaceCSV = `start_s,end_s,start_temp_c,end_temp_c,kind
0 s,60 s,30 °C,30 °C,hold
60 s,1740 s,30 °C,800 °C,ramp
1740 s,1800 s,800 °C,800 °C,hold
`

const valveCSV = `at_s,from_gas,to_gas,flow_sccm
0 s,,N2,200 sccm
900 s,N2,air,200 sccm
`

// Meta is the case pack manifest entry.
type Meta struct {
	CaseID    string         `json:"case_id"`
	Name      string         `json:"name"`
	Material  string         `json:"material"`
	Algorithm string         `json:"algorithm"`
	Variants  []VariantIndex `json:"variants"`
}

// VariantIndex is the manifest row for one variant.
type VariantIndex struct {
	Name  string   `json:"name"`
	Title string   `json:"title"`
	Layer string   `json:"direct_source_changed"`
	Scope []string `json:"expect_affected_layers"`
	Note  string   `json:"note"`
}

// WritePack writes the full offline case pack under dir/cases.
func WritePack(dir string) error {
	variants, err := Variants()
	if err != nil {
		return err
	}
	root := filepath.Join(dir, "cases")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	var idx []VariantIndex
	names := append([]string{"base"}, AllMutations...)
	for _, name := range names {
		v := variants[name]
		if v == nil {
			return fmt.Errorf("missing variant %q", name)
		}
		vd := filepath.Join(root, name)
		if err := os.MkdirAll(vd, 0o755); err != nil {
			return err
		}
		files := make([]string, 0, len(v.Files))
		for fn := range v.Files {
			files = append(files, fn)
		}
		sort.Strings(files)
		for _, fn := range files {
			if err := os.WriteFile(filepath.Join(vd, fn), []byte(v.Files[fn]), 0o644); err != nil {
				return err
			}
		}
		idx = append(idx, VariantIndex{
			Name: name, Title: v.Title, Layer: v.Changes, Scope: v.ExpectScope, Note: v.Note,
		})
	}
	meta := Meta{
		CaseID:    "tga-lineage-case-001",
		Name:      "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Material:  "分子筛负载金属氧化物（合成离线样例，固定 seed）",
		Algorithm: "tga-v1",
		Variants:  idx,
	}
	mb, _ := json.MarshalIndent(meta, "", "  ")
	return os.WriteFile(filepath.Join(root, "manifest.json"), append(mb, '\n'), 0o644)
}
