package tga

import (
	"bufio"
	"fmt"
	"strings"
)

// FormatID is the accepted small copyable input format.
//
// A file is a sequence of layered sections; unknown layers are rejected, and a
// malformed section half-fails (retained raw, not promoted) while other
// sections still import:
//
//	# TGA-ASCII/1
//	## samples
//	t, T, m
//	0.0min, 30C, 10.00mg
//	...
//	## drift
//	rate, basis
//	-0.002mg/min, baseline 2026-09-01
//	## furnace
//	start, end, from, to, kind
//	0min, 10min, 30C, 120C, ramp
//	## gas
//	at, gas, flow, action
//	20min, N2, 50mL/min, switch
//	## blank
//	t, blank_mass
//	0min, 0.000mg
//	## initial_mass
//	mass, balance, note
//	10.00mg, X10226, as-loaded
const FormatID = "tga-ascii/1"

// ParsedSections holds the per-section parse result plus the raw text.
type ParsedSections struct {
	Sections    map[Section]string
	Samples     []RawSamplePoint
	Furnace     []RawFurnaceStep
	Gas         []RawGasEvent
	Blank       []RawBlankPoint
	InitialMass *RawInitialMass
	Drift       *RawDrift
	Diags       []Diagnostic
	// SectionDiags counts hard errors per section; a section with a hard error
	// half-fails.
	SectionOK map[Section]bool
}

// ParseInput parses one submitted chunk. It never panics on bad data: bad rows
// become diagnostics and the offending section is marked failed.
func ParseInput(rawText, commitID string) (*ParsedSections, error) {
	out := &ParsedSections{
		Sections:   map[Section]string{},
		SectionOK:  map[Section]bool{},
	}
	sc := bufio.NewScanner(strings.NewReader(rawText))
	sc.Buffer(make([]byte, 1<<20), 1<<22)
	var cur Section
	var headerSeen bool
	row := 0
	var formatOK bool
	addErr := func(s Section, r int, field, msg, cell string) {
		out.Diags = append(out.Diags, Diagnostic{Severity: "error", Section: s, Row: r, Field: field, Message: msg, Cell: cell})
	}
	addWarn := func(s Section, r int, field, msg, cell string) {
		out.Diags = append(out.Diags, Diagnostic{Severity: "warning", Section: s, Row: r, Field: field, Message: msg, Cell: cell})
	}
	for sc.Scan() {
		row++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "##") {
			if strings.HasPrefix(line, "#") && strings.Contains(strings.ToLower(line), "tga-ascii") {
				formatOK = true
			}
			continue
		}
		if strings.HasPrefix(line, "##") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			key := Section(sanitizeHeader(name))
			cur = ""
			headerSeen = false
			for _, s := range AllSections {
				if s == key {
					cur = s
				}
			}
			if cur == "" {
				addErr(Section(sanitizeHeader(name)), row, "section", "unknown section "+name, line)
			} else {
				out.Sections[cur] = ""
				out.SectionOK[cur] = true
			}
			continue
		}
		if cur == "" {
			addErr("", row, "", "data outside any section", line)
			continue
		}
		fields := splitCSV(line)
		if !headerSeen {
			headerSeen = true
			if !headerCompatible(cur, fields) {
				// Allow a section to omit its header: treat the first row as
				// data, but flag it so reviewers see the relaxed parse.
				addWarn(cur, row, "header", "missing header row; treating this line as data", line)
			} else {
				continue
			}
		}
		switch cur {
		case SecSamples:
			if len(fields) < 3 {
				addErr(cur, row, "row", "need 3 columns t,T,m", line)
				out.SectionOK[cur] = false
				continue
			}
			tq := parseQuantity(DimTime, fields[0])
			tp := parseQuantity(DimTemperature, fields[1])
			mq := parseQuantity(DimMass, fields[2])
			p := RawSamplePoint{Row: row, CommitID: commitID, Time: tq, Temp: tp, Mass: mq}
			bad := false
			for _, q := range []struct{ f string; q Quantity }{{"t", tq}, {"T", tp}, {"m", mq}} {
				if !q.OK() {
					addErr(cur, row, q.f, q.Diagnostic, q.Raw)
					bad = true
				}
			}
			if !bad {
				out.Samples = append(out.Samples, p)
			}
		case SecFurnace:
			if len(fields) < 5 {
				addErr(cur, row, "row", "need start,end,from,to,kind", line)
				out.SectionOK[cur] = false
				continue
			}
			st := parseQuantity(DimTime, fields[0])
			en := parseQuantity(DimTime, fields[1])
			fr := parseQuantity(DimTemperature, fields[2])
			to := parseQuantity(DimTemperature, fields[3])
			kind := strings.TrimSpace(fields[4])
			step := RawFurnaceStep{Row: row, CommitID: commitID, Start: st, End: en, From: fr, To: to, Kind: kind}
			bad := kind != "hold" && kind != "ramp"
			if bad {
				addErr(cur, row, "kind", "kind must be hold|ramp", kind)
			}
			for _, q := range []Quantity{st, en, fr, to} {
				if !q.OK() {
					addErr(cur, row, "furnace", q.Diagnostic, q.Raw)
					bad = true
				}
			}
			if !bad {
				out.Furnace = append(out.Furnace, step)
			}
		case SecGas:
			if len(fields) < 4 {
				addErr(cur, row, "row", "need at,gas,flow,action", line)
				out.SectionOK[cur] = false
				continue
			}
			at := parseQuantity(DimTime, fields[0])
			gas := strings.TrimSpace(fields[1])
			flow := parseQuantity(DimFlow, fields[2])
			action := strings.TrimSpace(fields[3])
			ev := RawGasEvent{Row: row, CommitID: commitID, At: at, Gas: gas, Flow: flow, Action: action}
			bad := false
			if gas == "" {
				addErr(cur, row, "gas", "empty gas", fields[1])
				bad = true
			}
			if !at.OK() {
				addErr(cur, row, "at", at.Diagnostic, at.Raw)
				bad = true
			}
			if !flow.OK() {
				addErr(cur, row, "flow", flow.Diagnostic, flow.Raw)
				bad = true
			}
			if !bad {
				out.Gas = append(out.Gas, ev)
			}
		case SecBlank:
			if len(fields) < 2 {
				addErr(cur, row, "row", "need t,blank_mass", line)
				out.SectionOK[cur] = false
				continue
			}
			tq := parseQuantity(DimTime, fields[0])
			bq := parseQuantity(DimMass, fields[1])
			p := RawBlankPoint{Row: row, CommitID: commitID, Time: tq, BlankMass: bq}
			bad := false
			for _, q := range []Quantity{tq, bq} {
				if !q.OK() {
					addErr(cur, row, "blank", q.Diagnostic, q.Raw)
					bad = true
				}
			}
			if !bad {
				out.Blank = append(out.Blank, p)
			}
		case SecDrift:
			if out.Drift != nil {
				addErr(cur, row, "drift", "drift section allows one row", line)
				out.SectionOK[cur] = false
				continue
			}
			rq := parseQuantity(DimMass, strings.TrimSuffix(fields[0], "/min"))
			// Drift rate carries an explicit rate dimension; parse specially.
			rq = parseRate(fields[0])
			basis := ""
			if len(fields) > 1 {
				basis = strings.Join(fields[1:], " ")
			}
			if !rq.OK() {
				addErr(cur, row, "rate", rq.Diagnostic, rq.Raw)
				out.SectionOK[cur] = false
				continue
			}
			out.Drift = &RawDrift{CommitID: commitID, Rate: rq, Basis: strings.TrimSpace(basis)}
		case SecInitialMass:
			if out.InitialMass != nil {
				addErr(cur, row, "initial_mass", "initial_mass section allows one row", line)
				out.SectionOK[cur] = false
				continue
			}
			mq := parseQuantity(DimMass, fields[0])
			if !mq.OK() {
				addErr(cur, row, "mass", mq.Diagnostic, mq.Raw)
				out.SectionOK[cur] = false
				continue
			}
			im := &RawInitialMass{CommitID: commitID, Mass: mq}
			if len(fields) > 1 {
				im.Balance = strings.TrimSpace(fields[1])
			}
			if len(fields) > 2 {
				im.Note = strings.TrimSpace(strings.Join(fields[2:], " "))
			}
			out.InitialMass = im
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	if !formatOK {
		addErr("", 0, "format", "missing '# TGA-ASCII/1' marker", "")
	}
	return out, nil
}

// parseRate handles "<mass>/<time>" rate cells, normalising to mg/min.
func parseRate(cell string) Quantity {
	cell = strings.TrimSpace(cell)
	parts := strings.Split(cell, "/")
	if len(parts) != 2 {
		return Bad(DimMass, cell, "rate must be mass/time")
	}
	mq := parseQuantity(DimMass, parts[0])
	tq := parseQuantity(DimTime, parts[1])
	if !mq.OK() {
		return mq
	}
	if !tq.OK() {
		return Bad(DimMass, cell, tq.Diagnostic)
	}
	mv, _ := mq.NormFloat()
	tv, _ := tq.NormFloat()
	if tv == 0 {
		return Bad(DimMass, cell, "zero time unit")
	}
	v := mv / tv
	return Q(DimMass, v, cell, "mg/min")
}

func splitCSV(line string) []string {
	var out []string
	cur := strings.Builder{}
	for _, r := range line {
		switch r {
		case ',', ';', '\t':
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	out = append(out, strings.TrimSpace(cur.String()))
	return out
}

func sanitizeHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func headerCompatible(s Section, hdr []string) bool {
	want := map[Section]string{
		SecSamples: "t", SecFurnace: "start", SecGas: "at",
		SecBlank: "t", SecDrift: "rate", SecInitialMass: "mass",
	}
	w, ok := want[s]
	if !ok {
		return false
	}
	return strings.Contains(strings.ToLower(hdr[0]), w)
}

var _ = fmt.Sprintf
