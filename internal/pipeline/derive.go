package pipeline

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"tgaaudit/internal/domain"
)

type driftSeg struct {
	t0, t1      float64
	fracSlope   *float64 // fraction of initial mass per second
	massSlope   *float64 // kg per second
	interceptKg float64
}

// Derive deterministically recomputes all derived nodes for the given current
// batches and parameters. Raw batches and params are read only.
func Derive(b CurrentBatches, p domain.Params) *Derivation {
	d := &Derivation{
		Params:         p,
		Stages:         map[string][]Stage{},
		Residues:       map[string]ResidueData{},
		SourceBatchIDs: map[string]string{},
	}
	d.SetID = SetID(b, p)
	for kind, bat := range b {
		d.SourceBatchIDs[kind] = bat.ID
	}

	samples, ok := b[domain.KindSamples]
	if !ok {
		d.Diagnostics = append(d.Diagnostics, "no samples batch available")
		return d
	}

	tCol := view(&samples.Section, "t")
	mCol := view(&samples.Section, "mass")
	tsCol := view(&samples.Section, "ts")

	type rawRow struct {
		tS, mKg, tsK  *float64
		rt, rm, rtemp string
		tu, mu, tempu string
	}
	rows := make([]rawRow, 0, len(tCol.values))
	for i := range tCol.values {
		rows = append(rows, rawRow{
			tS:  toSeconds(tCol.values[i], tCol.unit),
			mKg: toKg(mCol.values[i], mCol.unit),
			tsK: toKelvin(tsCol.values[i], tsCol.unit),
			rt:  rawsAt(tCol.raws, i), rm: rawsAt(mCol.raws, i),
			rtemp: rawsAt(tsCol.raws, i),
			tu:    tCol.unit, mu: mCol.unit, tempu: tsCol.unit,
		})
	}

	var fT, fTf []float64
	if fur := b[domain.KindFurnace]; fur != nil {
		ft := view(&fur.Section, "t")
		ff := view(&fur.Section, "tf")
		for i := range ft.values {
			ts := toSeconds(ft.values[i], ft.unit)
			tf := toKelvin(ff.values[i], ff.unit)
			if ts != nil && tf != nil {
				fT = append(fT, *ts)
				fTf = append(fTf, *tf)
			}
		}
	}

	var switches []gasSwitch
	if gas := b[domain.KindGas]; gas != nil {
		gt := view(&gas.Section, "t")
		gg := view(&gas.Section, "gas")
		ge := view(&gas.Section, "event")
		prev := ""
		for i := range gt.values {
			ts := toSeconds(gt.values[i], gt.unit)
			name := strings.TrimSpace(gg.texts[i])
			if ts == nil || name == "" {
				continue
			}
			switches = append(switches, gasSwitch{t: *ts, from: prev, to: name,
				event: strings.TrimSpace(textAt(ge.texts, i))})
			prev = name
		}
		sort.Slice(switches, func(i, j int) bool { return switches[i].t < switches[j].t })
	}

	// Blank curve (dense reference series) kept in kg relative to its own mean.
	var blankX, blankY []float64
	if blk := b[domain.KindBlank]; blk != nil {
		bt := view(&blk.Section, "t")
		bm := view(&blk.Section, "mass")
		var raw []float64
		for i := range bt.values {
			ts := toSeconds(bt.values[i], bt.unit)
			m := toKg(bm.values[i], bm.unit)
			if ts != nil && m != nil {
				blankX = append(blankX, *ts)
				raw = append(raw, *m)
			}
		}
		base := meanF(raw)
		for _, m := range raw {
			blankY = append(blankY, m-base)
		}
	}

	// Piecewise balance-drift segments. Slopes may be fraction-of-initial-mass
	// per second (%/min, %/s) or mass per second (mg/s, g/s, mg/min...).
	var segs []driftSeg
	if dr := b[domain.KindDrift]; dr != nil {
		d0 := view(&dr.Section, "t0")
		d1 := view(&dr.Section, "t1")
		dsl := view(&dr.Section, "slope")
		din := view(&dr.Section, "intercept")
		for i := range d0.values {
			t0 := toSeconds(d0.values[i], d0.unit)
			t1 := toSeconds(d1.values[i], d1.unit)
			if t0 == nil || t1 == nil || dsl.values[i] == nil {
				continue
			}
			seg := driftSeg{t0: *t0, t1: *t1}
			u := strings.TrimSpace(dsl.unit)
			v := *dsl.values[i]
			switch u {
			case "%/min":
				f := v / 100.0 / 60.0
				seg.fracSlope = &f
			case "%/s":
				f := v / 100.0
				seg.fracSlope = &f
			case "mg/s":
				ms := v * 1e-6
				seg.massSlope = &ms
			case "mg/min":
				ms := v * 1e-6 / 60.0
				seg.massSlope = &ms
			case "g/s":
				ms := v * 1e-3
				seg.massSlope = &ms
			case "g/min":
				ms := v * 1e-3 / 60.0
				seg.massSlope = &ms
			case "kg/s":
				ms := v
				seg.massSlope = &ms
			default:
				continue
			}
			if din.values[i] != nil {
				if k := toKg(din.values[i], din.unit); k != nil {
					seg.interceptKg = *k
				}
			}
			segs = append(segs, seg)
		}
	}
	driftApplied := len(segs) > 0

	var initMass *float64
	initRaw, initNote := "", ""
	if im := b[domain.KindInitialMass]; im != nil {
		vc := view(&im.Section, "mass")
		nt := view(&im.Section, "note")
		if len(vc.values) > 0 {
			initMass = toKg(vc.values[len(vc.values)-1], vc.unit)
			initRaw = strings.TrimSpace(rawsAt(vc.raws, len(vc.values)-1)) + " " + vc.unit
		}
		if len(nt.texts) > 0 {
			initNote = textAt(nt.texts, len(nt.texts)-1)
		}
	}

	points := make([]Point, 0, len(rows))
	var diags []string
	for i, r := range rows {
		pt := Point{I: i, TimeUnit: r.tu, MassUnit: r.mu, TempUnit: r.tempu,
			RawTime: r.rt, RawMass: r.rm, RawTemp: r.rtemp}
		if r.tS != nil {
			pt.TS = *r.tS
		}
		if r.mKg == nil {
			pt.Missing = true
		} else {
			m := *r.mKg
			for _, sg := range segs {
				if pt.TS >= sg.t0 && pt.TS <= sg.t1 {
					if sg.fracSlope != nil && initMass != nil {
						m -= *sg.fracSlope * *initMass * pt.TS
					} else if sg.massSlope != nil {
						m -= *sg.massSlope * pt.TS
					}
					m -= sg.interceptKg
					break
				}
			}
			if len(blankX) > 0 {
				m -= interpDense(blankX, blankY, pt.TS)
			}
			v := m
			pt.MassRawKg = r.mKg
			pt.MassCorrKg = &v
		}
		if r.tsK != nil {
			pt.SampleTempK = r.tsK
		} else if len(fT) > 0 && !pt.Missing {
			pt.FurnaceTempK = interpolateAt(fT, fTf, pt.TS)
		}
		if !pt.Missing && len(switches) > 0 {
			pt.Gas = gasAt(switches, pt.TS)
		}
		points = append(points, pt)
	}

	if initMass == nil {
		for i := range points {
			if points[i].MassCorrKg != nil {
				v := *points[i].MassCorrKg
				initMass = &v
				diags = append(diags, "initial_mass: no declared initial mass; inferred from first corrected sample")
				break
			}
		}
	}
	for i := range points {
		points[i].InitialMassKg = initMass
		if points[i].MassCorrKg != nil && initMass != nil && *initMass != 0 {
			f := *points[i].MassCorrKg / *initMass
			points[i].MassFraction = &f
		}
	}

	for i := 1; i < len(points); i++ {
		a, bb := points[i-1].MassCorrKg, points[i].MassCorrKg
		if a != nil && bb != nil && *bb > *a+1e-12 {
			points[i].NonMonotonic = true
		}
	}

	blankScale := 1.0
	blankOffset := 0.0
	corr := &CorrectedData{
		Key:             "corrected",
		InitialMassKg:   initMass,
		InitialMassRaw:  initRaw,
		InitialMassNote: initNote,
		DriftApplied:    driftApplied,
		BlankScale:      blankScale,
		BlankOffsetKg:   blankOffset,
		BlankApplied:    len(blankX) > 0,
		Points:          points,
		Diagnostics:     diags,
		SourceBatchIDs:  b.ids(),
	}
	d.Corrected = corr

	d.buildGaps(p)
	d.buildDtg(p)
	d.detectPeaks(p)
	d.buildStages(p, switches)
	d.buildResidue(p)
	d.buildRules(switches)
	d.assembleNodes()
	return d
}

func rawsAt(r []string, i int) string {
	if i < len(r) {
		return r[i]
	}
	return ""
}
func textAt(r []string, i int) string {
	if i < len(r) {
		return r[i]
	}
	return ""
}

func meanF(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func minI(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// interpDense interpolates a dense reference series (blank curve / furnace
// program). It is never used to bridge gaps in sample data.
func interpDense(xs, ys []float64, x float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	if x <= xs[0] {
		return ys[0]
	}
	if x >= xs[len(xs)-1] {
		return ys[len(ys)-1]
	}
	i := sort.SearchFloat64s(xs, x)
	if i == 0 {
		return ys[0]
	}
	if xs[i] == x {
		return ys[i]
	}
	frac := (x - xs[i-1]) / (xs[i] - xs[i-1])
	return ys[i-1] + frac*(ys[i]-ys[i-1])
}

var _ = fmt.Sprintf
var _ = math.Abs
var _ = json.RawMessage(nil)
