package tga

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// computeReport derives one full analysis report from the given committed
// batches and parameters. It is deterministic: identical inputs always yield
// identical node and value identities.
func computeReport(projectID string, batches []*Batch, params Params) *Report {
	rep := &Report{
		ProjectID:     projectID,
		Points:        []Point{},
		Gaps:          []Gap{},
		Corrected:     []CorrectedPoint{},
		Tg:            []TgPoint{},
		Dtg:           []DtgPoint{},
		Switches:      []GasSwitch{},
		Program:       []ProgramSeg{},
		Peaks:         []Peak{},
		Candidates:    []StageCandidate{},
		Residues:      []Residue{},
		SumConstrained: false,
		Warnings:      []string{},
		DriftModel:    params.DriftModel,
	}
	rep.ParamFingerprint = Fingerprint(params)

	byLayer := map[string][]*Batch{}
	for _, b := range batches {
		byLayer[b.Layer] = append(byLayer[b.Layer], b)
	}
	for k := range byLayer {
		sort.SliceStable(byLayer[k], func(i, j int) bool {
			return byLayer[k][i].Seq < byLayer[k][j].Seq
		})
	}
	sourceBatches := []string{}
	for _, layer := range Layers {
		for _, b := range byLayer[layer] {
			sourceBatches = append(sourceBatches, b.ID)
		}
	}
	rep.SourceBatches = sourceBatches

	rep.Initial = buildInitialChoice(byLayer[LayerInitialMg])
	parseProgram(rep, byLayer[LayerProgram])
	parseSwitches(rep, byLayer[LayerGas], params)
	parsePoints(rep, byLayer[LayerSamples])
	detectGaps(rep)
	applyDrift(rep, byLayer[LayerBlank], byLayer[LayerDrift], params)
	flagNoise(rep, params)
	normalizeTG(rep)
	computeDTG(rep, params)
	detectPeaks(rep, params)
	buildCandidates(rep, params)
	buildResidues(rep)

	rep.NodeID = HashID("node", map[string]any{
		"project": projectID,
		"params":  string(CanonicalJSON(params)),
		"batches": sourceBatches,
	})
	return rep
}

func latestBatch(bs []*Batch) *Batch {
	if len(bs) == 0 {
		return nil
	}
	return bs[len(bs)-1]
}

func buildInitialChoice(bs []*Batch) *InitialMassChoice {
	if len(bs) == 0 {
		return nil
	}
	var choice *InitialMassChoice
	revs := []InitialMassRev{}
	for _, b := range bs {
		for _, it := range b.Items {
			if it.Kind != "initial_mass" {
				continue
			}
			raw, _ := rawGet(it.Raw, "mass", "m0", "initial_mass")
			mg, _, ok := parseMassMg(raw)
			revs = append(revs, InitialMassRev{
				BatchID: b.ID, Seq: b.Seq, Mg: mg, Raw: raw,
				Supersedes: b.Supersedes,
			})
			if b == latestBatch(bs) && ok {
				choice = &InitialMassChoice{BatchID: b.ID, ItemID: it.ID, Mg: mg, Raw: raw}
			}
		}
	}
	if choice == nil {
		if len(revs) > 0 {
			last := revs[len(revs)-1]
			return &InitialMassChoice{BatchID: last.BatchID, Revisions: revs}
		}
		return nil
	}
	choice.Revisions = revs
	return choice
}
func parseProgram(rep *Report, bs []*Batch) {
	b := latestBatch(bs)
	if b == nil {
		return
	}
	items := []RawItem{}
	for _, it := range b.Items {
		if it.Kind == "program" || it.Kind == "segment" {
			items = append(items, it)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Idx < items[j].Idx })
	for i, it := range items {
		rawT0, _ := rawGet(it.Raw, "start_s", "t0", "start")
		rawT1, _ := rawGet(it.Raw, "end_s", "t1", "end")
		rawC0, _ := rawGet(it.Raw, "start_c", "temp0", "t_start")
		rawC1, _ := rawGet(it.Raw, "end_c", "temp1", "t_end")
		t0, _, ok0 := parseTimeS(rawT0, "s")
		t1, _, ok1 := parseTimeS(rawT1, "s")
		c0, _, ok2 := parseTempC(rawC0)
		c1, _, ok3 := parseTempC(rawC1)
		if !(ok0 && ok1 && ok2 && ok3 && t1 > t0) {
			continue
		}
		kind := "ramp"
		if math.Abs(c1-c0) < 1e-9 {
			kind = "hold"
		}
		rate := (c1 - c0) / (t1 - t0) * 60
		seg := ProgramSeg{
			ID: HashID("seg", map[string]any{"b": b.ID, "i": i}),
			StartS: t0, EndS: t1, StartC: c0, EndC: c1,
			RateCPerMin: rate, Kind: kind,
		}
		rep.Program = append(rep.Program, seg)
	}
}

func parseSwitches(rep *Report, bs []*Batch, p Params) {
	for _, b := range bs {
		for _, it := range b.Items {
			if it.Kind != "gas" && it.Kind != "valve" && it.Kind != "gas_switch" {
				continue
			}
			rawT, _ := rawGet(it.Raw, "time_s", "time", "t", "valve_time_s")
			t, _, ok := parseTimeS(rawT, "s")
			if !ok {
				continue
			}
			from, _ := rawGet(it.Raw, "from_gas", "from", "gas_from")
			to, _ := rawGet(it.Raw, "to_gas", "to", "gas_to", "gas")
			sw := GasSwitch{
				ID:       HashID("gas", map[string]any{"item": it.ID}),
				SourceID: it.ID,
				ValveS:   t,
				ReactorS: t + p.AtmDelayS,
				DelayS:   p.AtmDelayS,
				FromGas:  strings.TrimSpace(from),
				ToGas:    strings.TrimSpace(to),
				RawTime:  rawT,
			}
			rep.Switches = append(rep.Switches, sw)
		}
	}
	sort.SliceStable(rep.Switches, func(i, j int) bool {
		if rep.Switches[i].ValveS == rep.Switches[j].ValveS {
			return rep.Switches[i].ID < rep.Switches[j].ID
		}
		return rep.Switches[i].ValveS < rep.Switches[j].ValveS
	})
}

func parsePoints(rep *Report, bs []*Batch) {
	type src struct {
		b  *Batch
		it RawItem
	}
	var all []src
	for _, b := range bs {
		for _, it := range b.Items {
			if it.Kind == "sample" || it.Kind == "point" || it.Kind == "" {
				all = append(all, src{b, it})
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].b.Seq != all[j].b.Seq {
			return all[i].b.Seq < all[j].b.Seq
		}
		return all[i].it.Idx < all[j].it.Idx
	})
	for _, s := range all {
		rawT, _ := rawGet(s.it.Raw, "time_s", "time", "t", "furnace_time_s")
		rawC, _ := rawGet(s.it.Raw, "temp_c", "temperature", "temp", "t_c")
		rawM, _ := rawGet(s.it.Raw, "mass_mg", "mass", "m", "weight")
		t, tu, okT := parseTimeS(rawT, "s")
		c, cu, okC := parseTempC(rawC)
		m, mu, okM := parseMassMg(rawM)
		pt := Point{
			ID:       HashID("pt", map[string]any{"item": s.it.ID}),
			SourceID: s.it.ID,
			RawTime:  rawT, RawTemp: rawC, RawMass: rawM,
			TimeUnit: tu, TempUnit: cu, MassUnit: mu,
		}
		if okT && okC && okM {
			pt.TimeS, pt.TempC, pt.MassMg = t, c, m
			pt.Valid = true
		} else {
			if !okT {
				pt.Diags = append(pt.Diags, Diagnostic{Code: "unconvertible_time", Field: "time", Message: fmt.Sprintf("无法转换时间值 %q，保留原位", rawT)})
			}
			if !okC {
				pt.Diags = append(pt.Diags, Diagnostic{Code: "unconvertible_temp", Field: "temp", Message: fmt.Sprintf("无法转换温度值 %q，保留原位", rawC)})
			}
			if !okM {
				pt.Diags = append(pt.Diags, Diagnostic{Code: "unconvertible_mass", Field: "mass", Message: fmt.Sprintf("无法转换质量值 %q，保留原位", rawM)})
			}
		}
		rep.Points = append(rep.Points, pt)
	}
	sort.SliceStable(rep.Points, func(i, j int) bool {
		if rep.Points[i].Valid != rep.Points[j].Valid {
			return rep.Points[i].Valid
		}
		if rep.Points[i].TimeS != rep.Points[j].TimeS {
			return rep.Points[i].TimeS < rep.Points[j].TimeS
		}
		return rep.Points[i].ID < rep.Points[j].ID
	})
}
