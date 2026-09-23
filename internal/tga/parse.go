package tga

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Block is one layer section of an import bundle. The raw text is kept
// verbatim; normalization happens here, in the derived-facing parser.
type Block struct {
	Layer Layer
	Raw   string
}

// SplitBundle splits an import bundle into per-layer blocks. A block
// starts at a "# layer: <name>" header. Unknown layer names produce a
// block with Layer == "" so the caller can record a diagnostic instead
// of dropping the text.
func SplitBundle(text string) []Block {
	var blocks []Block
	var cur *Block
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") && strings.Contains(trim, "layer:") {
			idx := strings.Index(trim, "layer:") + len("layer:")
			name := strings.TrimSpace(trim[idx:])
			l, ok := ParseLayerName(name)
			if !ok {
				l = Layer("?" + name)
			}
			blocks = append(blocks, Block{Layer: l})
			cur = &blocks[len(blocks)-1]
			continue
		}
		if cur != nil {
			cur.Raw += line + "\n"
		}
	}
	return blocks
}

var numUnitRe = regexp.MustCompile(`^([+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?)\s*([a-zA-Zµ%/]*)$`)

// parseQuantity parses "12.483 mg" / "12.483mg" / "12.483" into a
// quantity normalized to one of the canonical units in want.
func parseQuantity(raw string, want map[string]float64, canonical string) Quantity {
	q := Quantity{Raw: strings.TrimSpace(raw), Unit: canonical, Value: math.NaN()}
	m := numUnitRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		q.Diag = "not a number with optional unit"
		return q
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		q.Diag = "unparseable number: " + err.Error()
		return q
	}
	unit := m[2]
	if unit == "" {
		q.Value = v // bare numbers are read in the canonical unit
		return q
	}
	f, ok := want[unit]
	if !ok {
		q.Diag = fmt.Sprintf("unknown unit %q for %s", unit, canonical)
		return q
	}
	q.Value = v * f
	return q
}

var (
	massUnits = map[string]float64{"mg": 1, "g": 1000, "ug": 1e-3, "µg": 1e-3}
	timeUnits = map[string]float64{"s": 1, "min": 60, "h": 3600}
	tempUnits = map[string]float64{"C": 1, "K": -1} // K handled specially
	flowUnits = map[string]float64{"mL/min": 1, "ml/min": 1, "sccm": 1}
)

func parseTemp(raw string) Quantity {
	q := parseQuantity(raw, tempUnits, UnitCelsius)
	if strings.HasSuffix(strings.TrimSpace(raw), "K") && !q.NaN() {
		m := numUnitRe.FindStringSubmatch(strings.TrimSpace(raw))
		v, _ := strconv.ParseFloat(m[1], 64)
		q.Value = v - 273.15
	}
	return q
}

// ParseLayer normalizes one committed version. Every failure becomes a
// diagnostic; the offending raw line stays in Version.Raw.
func ParseLayer(v *Version) *ParsedLayer {
	p := &ParsedLayer{Version: v.ID, Layer: v.Layer}
	lineNo := 0
	for _, line := range strings.Split(v.Raw, "\n") {
		lineNo++
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		fields := strings.Fields(trim)
		switch v.Layer {
		case LayerRawPoints:
			if len(fields) != 3 {
				p.diags(v.Layer, lineNo, trim, "raw_points needs: <time> <temp> <mass>")
				continue
			}
			tq := parseQuantity(fields[0], timeUnits, UnitSecond)
			tpq := parseTemp(fields[1])
			mq := parseQuantity(fields[2], massUnits, UnitMg)
			if !ok3(p, v.Layer, lineNo, trim, tq, tpq, mq) {
				continue
			}
			p.Points = append(p.Points, Point{T: tq.Value, Temp: tpq.Value, Mass: mq.Value})
		case LayerDrift, LayerBlank:
			if len(fields) != 2 {
				p.diags(v.Layer, lineNo, trim, "needs: <time> <mass-correction>")
				continue
			}
			tq := parseQuantity(fields[0], timeUnits, UnitSecond)
			mq := parseQuantity(fields[1], massUnits, UnitMg)
			if !ok2(p, v.Layer, lineNo, trim, tq, mq) {
				continue
			}
			p.Points = append(p.Points, Point{T: tq.Value, Mass: mq.Value})
		case LayerFurnace:
			if len(fields) != 2 {
				p.diags(v.Layer, lineNo, trim, "furnace_program needs: <time> <setpoint>")
				continue
			}
			tq := parseQuantity(fields[0], timeUnits, UnitSecond)
			sq := parseTemp(fields[1])
			if !ok2(p, v.Layer, lineNo, trim, tq, sq) {
				continue
			}
			p.Furnace = append(p.Furnace, FurnaceSeg{T0: tq.Value, T1: tq.Value, Setpoint: sq.Value})
		case LayerGas:
			if len(fields) < 2 {
				p.diags(v.Layer, lineNo, trim, "gas_events needs: <time> <gas> [flow] [unit]")
				continue
			}
			tq := parseQuantity(fields[0], timeUnits, UnitSecond)
			if tq.NaN() {
				p.diags(v.Layer, lineNo, trim, "bad time: "+tq.Diag)
				continue
			}
			ge := GasEvent{T: tq.Value, Gas: fields[1], FlowUnit: UnitMlMin}
			if len(fields) >= 3 {
				fq := parseQuantity(fields[2], flowUnits, UnitMlMin)
				if fq.NaN() {
					p.diags(v.Layer, lineNo, trim, "bad flow: "+fq.Diag)
					continue
				}
				ge.Flow = fq.Value
			}
			p.Gas = append(p.Gas, ge)
		case LayerInitMass:
			q := parseQuantity(trim, massUnits, UnitMg)
			if q.NaN() {
				p.diags(v.Layer, lineNo, trim, "unconvertible initial mass: "+q.Diag)
			}
			if p.InitMass == nil {
				qq := q
				p.InitMass = &qq
			} else {
				p.diags(v.Layer, lineNo, trim, "duplicate initial mass line ignored")
			}
		default:
			p.diags(v.Layer, lineNo, trim, "unknown layer")
		}
	}
	// Furnace waypoints -> segments with rates.
	if v.Layer == LayerFurnace && len(p.Furnace) > 1 {
		segs := make([]FurnaceSeg, 0, len(p.Furnace)-1)
		for i := 0; i+1 < len(p.Furnace); i++ {
			a, b := p.Furnace[i], p.Furnace[i+1]
			rate := 0.0
			if b.T0 > a.T0 {
				rate = (b.Setpoint - a.Setpoint) / (b.T0 - a.T0) * 60
			}
			segs = append(segs, FurnaceSeg{T0: a.T0, T1: b.T0, Setpoint: a.Setpoint, RateCPerMin: rate})
		}
		p.Furnace = segs
	}
	return p
}

func (p *ParsedLayer) diags(l Layer, line int, raw, reason string) {
	p.Diags = append(p.Diags, Diagnostic{Layer: l, Line: line, Raw: raw, Reason: reason})
}

func ok3(p *ParsedLayer, l Layer, line int, raw string, qs ...Quantity) bool {
	for _, q := range qs {
		if q.NaN() {
			p.diags(l, line, raw, "unconvertible value "+strconv.Quote(q.Raw)+": "+q.Diag)
			return false
		}
	}
	return true
}

func ok2(p *ParsedLayer, l Layer, line int, raw string, qs ...Quantity) bool {
	return ok3(p, l, line, raw, qs...)
}
