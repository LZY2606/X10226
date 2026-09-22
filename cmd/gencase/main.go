// Command gencase generates the offline domain case pack under ./casepack.
// It is deterministic: fixed seed, fixed virtual origin, no network. The pack
// is real TGA material (sampled mass/temp, drift, furnace program, valve
// events, blank curve, initial mass) — never a generic table.
package main

import (
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
)

type opt struct {
	gapWindow      [2]float64 // seconds, drop points inside
	extraDrift     float64    // extra terminal drift mg
	blipAt         float64    // non-monotone perturbation time, 0 = none
	blipAmp        float64
	overlapSqueeze bool       // move oxidation step closer
	initialRev     bool       // second initial mass statement
	halfFail       bool       // append malformed program section
	extraGasDelay  float64    // documented in manifest (parameter scenario)
}

const (
	t0, tEnd, dt = 0.0, 900.0, 10.0
	m0           = 12.500
	w            = 12.0 // logistic half-width seconds
)

// step centers (s): moisture, decomposition1, decomposition2, oxidation
var centers = []float64{120, 320, 560, 760}
var drops = []float64{0.45, 3.10, 1.85, 1.10}

func main() {
	dir := flag.String("out", "casepack", "output directory")
	flag.Parse()
	if err := os.MkdirAll(*dir, 0o755); err != nil {
		fail(err)
	}
	scenarios := []struct {
		name, desc string
		o          opt
	}{
		{"01_baseline/import.txt", "baseline: all six stratified layers, mild overlap and noise", opt{blipAt: 0}},
		{"edge_cases/edge_drift.txt", "balance baseline drift: steep linear drift correction", opt{extraDrift: 0.20}},
		{"edge_cases/edge_overlap.txt", "overlapping peaks: oxidation squeezed next to decomposition", opt{overlapSqueeze: true}},
		{"edge_cases/edge_nonmonotone.txt", "non-monotone noise: positive blip retained, DTG downgraded", opt{blipAt: 250, blipAmp: 0.06}},
		{"edge_cases/edge_gap.txt", "sampling gap 180..260s: no interpolation across it", opt{gapWindow: [2]float64{180, 260}}},
		{"edge_cases/edge_initial_revision.txt", "initial mass revision: second initial-mass statement", opt{initialRev: true}},
		{"edge_cases/edge_half_failure.txt", "import half failure: malformed program layer, siblings accepted", opt{halfFail: true}},
	}
	for _, sc := range scenarios {
		path := filepath.Join(*dir, sc.name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			fail(err)
		}
		rng := rand.New(rand.NewSource(seedOf(sc.name)))
		text := buildBundle(rng, sc.o)
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %s\n", path)
	}
	if err := os.WriteFile(filepath.Join(*dir, "README.md"), []byte(readme()), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("wrote casepack/README.md")
}

func seedOf(name string) int64 {
	// deterministic named seeds
	var h int64 = 1469598103934665603
	for _, b := range []byte(name) {
		h ^= int64(b)
		h *= 1099511628211
	}
	if h < 0 {
		h = -h
	}
	return h
}

func tempAt(t float64) float64 {
	// 0-60s 30C; 60-660s +2.5C/min -> 305C at 660; 660-840 +5C/min -> 455C at840; hold
	switch {
	case t < 60:
		return 30
	case t <= 660:
		return 30 + (t-60)/60*2.5
	case t <= 840:
		return 305 + (t-660)/60*5
	default:
		return 455
	}
}

func trueMass(t float64, o opt) float64 {
	cs := centers
	ds := drops
	if o.overlapSqueeze {
		c2 := make([]float64, len(cs))
		copy(c2, cs)
		c2[3] = cs[2] + 45 // 605s: tight overlap
		cs = c2
	}
	m := m0
	for i, c := range cs {
		m -= ds[i] / (1 + math.Exp(-(t-c)/w))
	}
	if o.blipAt > 0 && math.Abs(t-o.blipAt) < 1e-9 {
		m += o.blipAmp
	}
	return m
}

func blankAt(t float64, o opt) float64 {
	return 0.020 + 0.00006*t + o.extraDrift*(t/tEnd)
}

func driftAt(t float64, o opt) float64 {
	// separate stated balance drift correction (linear), grows with scenario
	return 0.010 + 0.00002*t + 0.5*o.extraDrift*(t/tEnd)
}

func buildBundle(rng *rand.Rand, o opt) string {
	var b strings.Builder
	// samples
	b.WriteString("# @layer samples\n")
	b.WriteString("t[s] mass[mg] temp[degC]\n")
	for t := t0; t <= tEnd+1e-9; t += dt {
		if t > o.gapWindow[0] && t < o.gapWindow[1] {
			continue
		}
		tm := trueMass(t, o)
		bm := blankAt(t, o)
		df := driftAt(t, o)
		noise := (rng.Float64() - 0.5) * 0.004
		reading := tm + bm + df + noise
		b.WriteString(fmt.Sprintf("%.0f %.4f %.2f\n", t, reading, tempAt(t)))
	}
	// drift
	b.WriteString("# @layer drift\n")
	b.WriteString("t[s] offset[mg] kind\n")
	for _, t := range []float64{0, 300, 600, 900} {
		b.WriteString(fmt.Sprintf("%.0f %.4f linear\n", t, driftAt(t, o)))
	}
	// program
	b.WriteString("# @layer program\n")
	b.WriteString("t_start[s] t_end[s] temp_start[degC] temp_end[degC] label\n")
	b.WriteString("0 60 30 30 \"equilibrate\"\n")
	b.WriteString("60 660 30 305 \"heat 2.5C/min\"\n")
	b.WriteString("660 840 305 455 \"heat 5C/min\"\n")
	b.WriteString("840 900 455 455 hold\n")
	// gas valve events (valve time, not reaction time)
	b.WriteString("# @layer gas\n")
	b.WriteString("t[s] from_gas to_gas flow[sccm]\n")
	b.WriteString("0 none N2 50\n")
	b.WriteString("680 N2 air 50\n")
	b.WriteString("820 air N2 50\n")
	// blank curve
	b.WriteString("# @layer blank\n")
	b.WriteString("t[s] mass[mg]\n")
	for t := t0; t <= tEnd+1e-9; t += 30 {
		b.WriteString(fmt.Sprintf("%.0f %.4f\n", t, blankAt(t, o)))
	}
	// initial mass (revision adds a second statement)
	b.WriteString("# @layer initial\n")
	b.WriteString("mass[mg] reason\n")
	b.WriteString("12.500 \"pan+sample at t0\"\n")
	if o.initialRev {
		b.WriteString("12.420 \"re-weighed after tare correction\"\n")
	}
	if o.halfFail {
		b.WriteString("# @layer program\n")
		b.WriteString("t_start[s] t_end[s] temp_start[degC] temp_end[degC]\n")
		b.WriteString("0 60 thirty thirty EXTRA\n")
	}
	return b.String()
}

func readme() string {
	return `# 失重谱系：TGA 阶段分解、气氛切换与残留物证据链（离线案例包）

所有样例由 ` + "`cmd/gencase`" + ` 以固定 seed 与固定时间原点确定性生成，不访问网络。
每行保留原始表示（数值+单位），派生层才规范化。

- ` + "`01_baseline/import.txt`" + `：六层基线（原始采样点、天平漂移校正、炉程序、气体阀事件、空白曲线、样品初重）。
- ` + "`edge_cases/edge_drift.txt`" + `：天平基线漂移加大。
- ` + "`edge_cases/edge_overlap.txt`" + `：重叠峰（氧化峰向分解峰挤压）。
- ` + "`edge_cases/edge_nonmonotone.txt`" + `：非单调噪声（正鼓包保留，DTG 降级）。
- ` + "`edge_cases/edge_gap.txt`" + `：采样缺口 180–260s，不跨缺口插值/求导。
- ` + "`edge_cases/edge_initial_revision.txt`" + `：初重修订（新版本，不覆盖旧值）。
- ` + "`edge_cases/edge_half_failure.txt`" + `：导入半失败（program 层格式错误，兄弟层仍接受）。

领域判断：阀切换时间 != 样品反应时间（气体延迟+温度滞后）；阶段可重叠且不强制总和 100%；
缺口不跨越插值；无法唯一归属时保留全部合法候选。
`
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
