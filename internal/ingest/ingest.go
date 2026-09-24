// Package ingest parses copy-pasteable small CSV samples into the six
// preservation strata. Every input fragment is stored verbatim; malformed
// rows are rejected (or staged) rather than aborting the batch, which is how
// import half-failure is represented.
package ingest

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// Section header grammar: a line beginning with "# @" introduces a stratum:
//
//	# @raw_sample_points columns=furnace_time[min],mass[mg],temperature[C]
//	# @balance_drift slope[mg/min],intercept[mg],source
//	# @furnace_program from[min],to[min],t0[C],t1[C],kind
//	# @gas_valve_event time[min],action,species:flow[mL/min];...,note
//	# @blank_curve time[min],mass[mg],temperature[C]
//	# @initial_mass revision,mass[mg],reason,supersedes_revision
//
// Column headers declare units explicitly so raw text is preserved.
type Fragment struct {
	Stratum    domain.Stratum
	LineNo     int
	Original   string
	Status     domain.ImportStatus
	Diagnostic string
	Payload    any
	Supersedes string
}

// Result is the parsed outcome of one import stream.
type Result struct {
	Fragments []Fragment
	Sections  []domain.Stratum
}

// Parse reads the sectioned CSV format.
func Parse(r io.Reader) (Result, error) {
	var out Result
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var cur domain.Stratum
	var units []string
	seq := map[domain.Stratum]int{}
	lineNo := 0
	for sc.Scan() {
		lineNo++
		raw := sc.Text()
		trim := strings.TrimSpace(raw)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "# @") {
			h, err := parseHeader(trim[3:])
			if err != nil {
				out.Fragments = append(out.Fragments, Fragment{
					LineNo: lineNo, Original: raw, Status: domain.StatusRejected,
					Diagnostic: "bad section header: " + err.Error(),
				})
				cur = ""
				continue
			}
			cur = h.stratum
			units = h.units
			if !seen(out.Sections, cur) {
				out.Sections = append(out.Sections, cur)
			}
			continue
		}
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if cur == "" {
			out.Fragments = append(out.Fragments, Fragment{
				LineNo: lineNo, Original: raw, Status: domain.StatusRejected,
				Diagnostic: "data row outside any # @section",
			})
			continue
		}
		seq[cur]++
		f := parseRow(cur, lineNo, raw, units, seq[cur])
		out.Fragments = append(out.Fragments, f)
	}
	return out, sc.Err()
}

func seen(xs []domain.Stratum, s domain.Stratum) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

type header struct {
	stratum domain.Stratum
	units   []string
}

func parseHeader(h string) (header, error) {
	h = strings.TrimSpace(h)
	fields := strings.Fields(h)
	if len(fields) == 0 {
		return header{}, fmt.Errorf("empty header")
	}
	st := domain.Stratum(fields[0])
	if !st.Valid() {
		return header{}, fmt.Errorf("unknown stratum %q", fields[0])
	}
	hd := header{stratum: st}
	for _, f := range fields[1:] {
		if strings.HasPrefix(f, "columns=") {
			for _, c := range strings.Split(f[len("columns="):], ",") {
				hd.units = append(hd.units, strings.TrimSpace(c))
			}
		}
	}
	return hd, nil
}

func splitRow(line string) []string {
	var cols []string
	var b strings.Builder
	inQ := false
	for _, r := range line {
		switch {
		case r == '"':
			inQ = !inQ
		case r == ',' && !inQ:
			cols = append(cols, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	cols = append(cols, b.String())
	return cols
}

func parseRow(st domain.Stratum, lineNo int, raw string, units []string, seqNo int) Fragment {
	cols := splitRow(raw)
	f := Fragment{Stratum: st, LineNo: lineNo, Original: raw, Status: domain.StatusAccepted}
	reject := func(format string, args ...any) Fragment {
		f.Status = domain.StatusRejected
		f.Diagnostic = fmt.Sprintf(format, args...)
		return f
	}
	switch st {
	case domain.StratumRawSample:
		if len(cols) < 3 {
			return reject("need time,mass,temperature, got %d columns", len(cols))
		}
		sp := domain.SamplePoint{
			Seq:         seqNo,
			FurnaceTime: q(cols[0], unitAt(units, 0, "min"), "furnace_time"),
			Mass:        q(cols[1], unitAt(units, 1, "mg"), "sample_pan_mass"),
			Temperature: q(cols[2], unitAt(units, 2, "C"), "measured_sample"),
		}
		f.Payload = sp
		if bad := badQuantity(sp.FurnaceTime, sp.Mass, sp.Temperature); bad != "" {
			return reject("%s", bad)
		}
	case domain.StratumBlank:
		if len(cols) < 3 {
			return reject("need time,mass,temperature, got %d columns", len(cols))
		}
		bp := domain.BlankPoint{
			Seq:         seqNo,
			FurnaceTime: q(cols[0], unitAt(units, 0, "min"), "furnace_time"),
			Mass:        q(cols[1], unitAt(units, 1, "mg"), "blank_pan_mass"),
			Temperature: q(cols[2], unitAt(units, 2, "C"), "measured_sample"),
		}
		f.Payload = bp
		if bad := badQuantity(bp.FurnaceTime, bp.Mass, bp.Temperature); bad != "" {
			return reject("%s", bad)
		}
	case domain.StratumDrift:
		if len(cols) < 2 {
			return reject("need slope,intercept[,source], got %d columns", len(cols))
		}
		src := ""
		if len(cols) > 2 {
			src = strings.TrimSpace(cols[2])
		}
		dc := domain.DriftCorrection{
			Slope:     q(cols[0], "mg/min", "drift_slope"),
			Intercept: q(cols[1], "mg", "drift_intercept"),
			Source:    src,
		}
		f.Payload = dc
		if dc.Slope.Diagnostic != "" {
			return reject("slope: %s", dc.Slope.Diagnostic)
		}
		if dc.Intercept.Diagnostic != "" {
			return reject("intercept: %s", dc.Intercept.Diagnostic)
		}
	case domain.StratumProgram:
		if len(cols) < 5 {
			return reject("need from,to,t0,t1,kind, got %d columns", len(cols))
		}
		kind := strings.ToLower(strings.TrimSpace(cols[4]))
		if kind != "ramp" && kind != "hold" {
			return reject("program kind must be ramp|hold, got %q", kind)
		}
		ps := domain.ProgramSegment{
			Seq:       seqNo,
			FromTime:  q(cols[0], "min", "furnace_time"),
			ToTime:    q(cols[1], "min", "furnace_time"),
			Setpoint0: q(cols[2], "C", "furnace_setpoint"),
			Setpoint1: q(cols[3], "C", "furnace_setpoint"),
			Kind:      kind,
		}
		f.Payload = ps
		if bad := badQuantity(ps.FromTime, ps.ToTime, ps.Setpoint0, ps.Setpoint1); bad != "" {
			return reject("%s", bad)
		}
	case domain.StratumGasEvent:
		if len(cols) < 2 {
			return reject("need time,action,species..., got %d columns", len(cols))
		}
		action := strings.ToLower(strings.TrimSpace(cols[1]))
		if action != "open" && action != "close" && action != "switch" {
			return reject("action must be open|close|switch, got %q", action)
		}
		ge := domain.GasValveEvent{
			Seq:         seqNo,
			FurnaceTime: q(cols[0], "min", "furnace_time"),
			Action:      action,
		}
		if len(cols) > 2 && strings.TrimSpace(cols[2]) != "" {
			for _, spec := range strings.Split(cols[2], ";") {
				name, flow, ok := strings.Cut(spec, ":")
				if !ok {
					return reject("species token %q needs name:flow", spec)
				}
				ft := q(flow, "mL/min", "gas_flow")
				if ft.Diagnostic != "" {
					return reject("species %s flow: %s", name, ft.Diagnostic)
				}
				ge.Species = append(ge.Species, domain.Species{
					Name: strings.TrimSpace(name), Flow: ft, Original: spec,
				})
			}
		}
		if len(cols) > 3 {
			ge.Note = strings.TrimSpace(cols[3])
		}
		f.Payload = ge
		if ge.FurnaceTime.Diagnostic != "" {
			return reject("time: %s", ge.FurnaceTime.Diagnostic)
		}
	case domain.StratumInitial:
		if len(cols) < 2 {
			return reject("need revision,mass[,reason,supersedes], got %d columns", len(cols))
		}
		rev, err := strconv.Atoi(strings.TrimSpace(cols[0]))
		if err != nil || rev < 1 {
			return reject("revision must be positive integer, got %q", cols[0])
		}
		im := domain.InitialMass{
			Revision: rev,
			Mass:     q(cols[1], unitAt(units, 1, "mg"), "sample_initial_mass"),
		}
		if len(cols) > 2 {
			im.Reason = strings.TrimSpace(cols[2])
		}
		if len(cols) > 3 && strings.TrimSpace(cols[3]) != "" {
			sup, err := strconv.Atoi(strings.TrimSpace(cols[3]))
			if err != nil || sup < 1 {
				return reject("supersedes_revision must be positive integer, got %q", cols[3])
			}
			im.Supersedes = strconv.Itoa(sup)
			f.Supersedes = im.Supersedes
		}
		f.Payload = im
		if im.Mass.Diagnostic != "" {
			return reject("mass: %s", im.Mass.Diagnostic)
		}
	}
	return f
}

func q(text, unit, basis string) domain.Quantity {
	return domain.Q(strings.TrimSpace(text), unit, basis)
}

// unitAt extracts the bracketed unit of the nth declared column, e.g.
// "mass[mg]" -> "mg".
func unitAt(units []string, i int, fallback string) string {
	if i < len(units) {
		c := units[i]
		if l := strings.IndexByte(c, '['); l >= 0 {
			if r := strings.IndexByte(c[l:], ']'); r > 0 {
				return c[l+1 : l+r]
			}
		}
	}
	return fallback
}

func badQuantity(qs ...domain.Quantity) string {
	for _, x := range qs {
		if !x.Valid {
			return "unparseable quantity: " + x.Original + " (" + x.Diagnostic + ")"
		}
		if x.Diagnostic != "" {
			return "unconvertible quantity: " + x.Original + " " + x.Unit + " (" + x.Diagnostic + ")"
		}
	}
	return ""
}
