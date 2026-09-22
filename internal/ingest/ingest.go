// Package ingest normalizes parsed sections into typed layer records.
// Nothing here discards an unconvertible value: it is kept in the Q raw form
// with a diagnostic and the row is flagged.
package ingest

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"tgasys/internal/domain"
	"tgasys/internal/parse"
)

// LayerSet holds typed views of the six source layers plus parameters.
type LayerSet struct {
	Samples    []domain.SamplePoint
	DriftRows  []DriftRow
	Furnace    []domain.FurnaceStep
	Gas        []domain.GasEvent
	Blank      []BlankPoint
	Initial    []InitialMass
	Meta       map[string]string
	Parameters *domain.Parameters
	Issues     []domain.Issue
}

type DriftRow struct {
	T    float64
	Off  domain.Q
	Note string
}

type BlankPoint struct {
	T float64
	M domain.Q
}

type InitialMass struct {
	M       domain.Q
	Basis   string // declared|tare|revised
	Note    string
	Order   int
}

// Normalize converts parsed sections into typed records and reports issues.
func Normalize(secs []domain.Section) LayerSet {
	ls := LayerSet{Meta: map[string]string{}}
	for _, sec := range secs {
		switch sec.Kind {
		case domain.KindSamples:
			ls.readSamples(sec)
		case domain.KindDrift:
			ls.readDrift(sec)
		case domain.KindFurnace:
			ls.readFurnace(sec)
		case domain.KindGas:
			ls.readGas(sec)
		case domain.KindBlank:
			ls.readBlank(sec)
		case domain.KindInitial:
			ls.readInitial(sec)
		case domain.KindMeta:
			ls.readMeta(sec)
		case domain.KindParameter:
			p, iss := ReadParameters(sec.Raw)
			ls.Parameters = p
			ls.Issues = append(ls.Issues, iss...)
		default:
			ls.Issues = append(ls.Issues, domain.Issue{
				Code: "ingest.unretained_section", Severity: "error",
				Message: "section " + sec.Header + " is not a recognized layer; raw retained",
			})
		}
	}
	sort.SliceStable(ls.Gas, func(i, j int) bool { return ls.Gas[i].T < ls.Gas[j].T })
	return ls
}

func (ls *LayerSet) add(code, sev, msg string) {
	ls.Issues = append(ls.Issues, domain.Issue{Code: code, Severity: sev, Message: msg})
}

func (ls *LayerSet) readSamples(sec domain.Section) {
	for ri, rec := range sec.Records {
		if len(rec) < 2 {
			ls.add("samples.bad_row", "error", fmt.Sprintf("samples row %d has %d fields (need t,m[,temp])", ri+1, len(rec)))
			continue
		}
		tq := parse.ParseQ(rec[0]+" s", "furnace_time_s")
		mq := parse.ParseQ(strings.Join(rec[1:min(2, len(rec))], " "), "mass_mg_actual")
		sp := domain.SamplePoint{T: tq.Value, M: mq}
		if tq.Unconvertible {
			ls.add("samples.bad_time", "error", fmt.Sprintf("row %d: %s", ri+1, tq.Diagnostic))
		}
		if mq.Unconvertible {
			ls.add("samples.bad_mass", "error", fmt.Sprintf("row %d: %s", ri+1, mq.Diagnostic))
		}
		if len(rec) >= 3 {
			sp.TS = parse.ParseQ(strings.Join(rec[2:3], " "), "sample_temp_C")
			if sp.TS.Unconvertible {
				ls.add("samples.bad_temp", "warning", fmt.Sprintf("row %d: %s", ri+1, sp.TS.Diagnostic))
			}
		}
		if len(rec) >= 4 {
			sp.TF = parse.ParseQ(strings.Join(rec[3:4], " "), "furnace_temp_C")
		}
		if len(rec) >= 5 {
			sp.Note = strings.Join(rec[4:], " ")
		}
		ls.Samples = append(ls.Samples, sp)
	}
	if len(ls.Samples) < 2 {
		ls.add("samples.too_few", "error", "need at least two sample points")
	}
}

func (ls *LayerSet) readDrift(sec domain.Section) {
	// supported: "model linear slope_mg_per_min ..." or rows t_s offset
	for _, line := range nonCommentLines(sec.Raw) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if strings.EqualFold(f[0], "model") {
			ls.DriftRows = append(ls.DriftRows, DriftRow{Note: strings.Join(f[1:], " ")})
			continue
		}
		if len(f) < 2 {
			ls.add("drift.bad_row", "error", "drift row: "+line)
			continue
		}
		tq := parse.ParseQ(f[0]+" s", "furnace_time_s")
		off := parse.ParseQ(strings.Join(f[1:], " "), "mass_mg_actual")
		if off.Unconvertible || tq.Unconvertible {
			ls.add("drift.bad_value", "warning", "drift row kept raw: "+line)
		}
		ls.DriftRows = append(ls.DriftRows, DriftRow{T: tq.Value, Off: off})
	}
}

func (ls *LayerSet) readFurnace(sec domain.Section) {
	for ri, rec := range sec.Records {
		if len(rec) < 4 {
			ls.add("furnace.bad_row", "error", fmt.Sprintf("furnace row %d needs from_s,to_s,rate,target", ri+1))
			continue
		}
		from := parse.ParseQ(rec[0]+" s", "furnace_time_s")
		to := parse.ParseQ(rec[1]+" s", "furnace_time_s")
		rate := parse.ParseQ(strings.Join(rec[2:3], " "), "rate_K_min")
		target := parse.ParseQ(strings.Join(rec[3:4], " "), "setpoint_C")
		for _, q := range []domain.Q{from, to, rate, target} {
			if q.Unconvertible {
				ls.add("furnace.bad_value", "error", "row "+strconv.Itoa(ri+1)+": "+q.Diagnostic)
			}
		}
		ls.Furnace = append(ls.Furnace, domain.FurnaceStep{
			FromT: from.Value, ToT: to.Value, Rate: rate, Target: target,
		})
	}
}

func (ls *LayerSet) readGas(sec domain.Section) {
	for _, rec := range sec.Records {
		if len(rec) < 3 {
			ls.add("gas.bad_row", "error", "gas row needs t, action, gas[flow]")
			continue
		}
		tq := parse.ParseQ(rec[0]+" s", "furnace_time_s")
		ev := domain.GasEvent{T: tq.Value, Action: strings.ToLower(rec[1]), Gas: strings.ToUpper(rec[2]), RawLine: strings.Join(rec, " ")}
		if len(rec) >= 4 {
			ev.Flow = parse.ParseQ(strings.Join(rec[3:], " "), "flow_mL_min")
		}
		if tq.Unconvertible {
			ls.add("gas.bad_time", "error", tq.Diagnostic)
		}
		ls.Gas = append(ls.Gas, ev)
	}
}

func (ls *LayerSet) readBlank(sec domain.Section) {
	for ri, rec := range sec.Records {
		if len(rec) < 2 {
			ls.add("blank.bad_row", "error", fmt.Sprintf("blank row %d", ri+1))
			continue
		}
		tq := parse.ParseQ(rec[0]+" s", "furnace_time_s")
		mq := parse.ParseQ(strings.Join(rec[1:], " "), "mass_mg_actual")
		if mq.Unconvertible {
			ls.add("blank.bad_mass", "warning", mq.Diagnostic)
		}
		ls.Blank = append(ls.Blank, BlankPoint{T: tq.Value, M: mq})
	}
}

func (ls *LayerSet) readInitial(sec domain.Section) {
	order := 0
	for _, line := range nonCommentLines(sec.Raw) {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		basis := "declared"
		var tokens []string
		if len(f) >= 2 && (f[1] == "declared" || f[1] == "tare" || f[1] == "revised") {
			basis = f[1]
			tokens = f[:1]
			if len(f) > 2 {
				tokens = append(tokens, f[2:]...)
			}
		} else {
			tokens = f
		}
		mq := parse.ParseQ(strings.Join(tokens, " "), "mass_mg_actual")
		note := ""
		if i := strings.Index(line, "#"); i >= 0 {
			note = strings.TrimSpace(line[i+1:])
		}
		if mq.Unconvertible {
			ls.add("initial.bad_mass", "error", mq.Diagnostic)
		}
		ls.Initial = append(ls.Initial, InitialMass{M: mq, Basis: basis, Note: note, Order: order})
		order++
	}
}

func (ls *LayerSet) readMeta(sec domain.Section) {
	for _, line := range nonCommentLines(sec.Raw) {
		if i := strings.Index(line, "="); i > 0 {
			k := strings.TrimSpace(line[:i])
			v := strings.TrimSpace(line[i+1:])
			ls.Meta[k] = v
		}
	}
}

func nonCommentLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, "##") {
			continue
		}
		out = append(out, line)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
