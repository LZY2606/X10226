// Package importx parses operator-submitted text into the six source layers.
// Raw text is stored verbatim first; parsing never mutates it. Fatal format
// errors mark the submission as not accepted (half-failed import) while the
// original bytes stay available for inspection. Values that cannot be
// normalized stay in place with diagnostics.
package importx

import (
	"fmt"
	"strconv"
	"strings"

	"tgasys/internal/domain/sources"
	"tgasys/internal/domain/units"
)

// doc is a parsed text document: !headers, a column header row and data rows.
type doc struct {
	headers map[string]string
	cols    []string
	rows    [][]string
	rawRows []string
	diags   []sources.Diag
}

func splitDoc(text string) (*doc, error) {
	d := &doc{headers: map[string]string{}}
	lines := strings.Split(text, "\n")
	headerSeen := false
	for i, ln := range lines {
		ln = strings.TrimRight(ln, "\r")
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "!") {
			kv := strings.SplitN(strings.TrimPrefix(trim, "!"), ":", 2)
			if len(kv) != 2 {
				d.diags = append(d.diags, sources.Diag{Scope: fmt.Sprintf("line:%d", i+1), Severity: "warn", Message: "malformed !header, ignored", RawLine: ln})
				continue
			}
			d.headers[strings.TrimSpace(kv[0])] = strings.TrimSpace(kv[1])
			continue
		}
		fields := splitCSVLine(trim)
		if !headerSeen {
			d.cols = fields
			headerSeen = true
			continue
		}
		d.rows = append(d.rows, fields)
		d.rawRows = append(d.rawRows, ln)
	}
	if !headerSeen {
		return nil, fmt.Errorf("no column header row found")
	}
	return d, nil
}

func splitCSVLine(s string) []string {
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func (d *doc) colIdx(name string) int {
	for i, c := range d.cols {
		if c == name {
			return i
		}
	}
	return -1
}

func (d *doc) unit(field string) string {
	u := d.headers["units"]
	for _, kv := range strings.Fields(u) {
		p := strings.SplitN(kv, "=", 2)
		if len(p) == 2 && p[0] == field {
			return p[1]
		}
	}
	return ""
}

func (d *doc) requireUnit(field string) error {
	if d.unit(field) == "" {
		return fmt.Errorf("missing required unit declaration for %q in !units header", field)
	}
	return nil
}

// get returns the raw token of column `name` in row r, or "" if absent.
func (d *doc) get(r []string, name string) string {
	i := d.colIdx(name)
	if i < 0 || i >= len(r) {
		return ""
	}
	return r[i]
}

func (d *doc) qty(r []string, field, basis string, rowNo int, diags *[]sources.Diag) units.Quantity {
	raw := d.get(r, field)
	q, err := units.Normalize(raw, d.unit(field), basis)
	if err != nil {
		*diags = append(*diags, sources.Diag{
			Scope: fmt.Sprintf("row:%d/field:%s", rowNo, field), Severity: "error",
			Message: err.Error(), RawLine: raw,
		})
	}
	return q
}

// Parse dispatches on layer. A fatal error means the submission is not
// accepted; the caller still stores the raw submission (half-failed import).
func Parse(layer sources.Layer, sub sources.Submission) (any, error) {
	switch layer {
	case sources.LayerRaw:
		return ParseRaw(sub)
	case sources.LayerDrift:
		return ParseDrift(sub)
	case sources.LayerFurnace:
		return ParseFurnace(sub)
	case sources.LayerGas:
		return ParseGas(sub)
	case sources.LayerBlank:
		return ParseBlank(sub)
	case sources.LayerInitial:
		return ParseInitial(sub)
	}
	return nil, fmt.Errorf("unknown layer %q", layer)
}

func ParseRaw(sub sources.Submission) (*sources.RawSeries, error) {
	d, err := splitDoc(sub.RawText)
	if err != nil {
		return nil, err
	}
	for _, f := range []string{"time", "mass", "temp"} {
		if d.colIdx(f) < 0 {
			return nil, fmt.Errorf("raw_points: missing required column %q", f)
		}
		if err := d.requireUnit(f); err != nil {
			return nil, err
		}
	}
	rs := &sources.RawSeries{
		SubmissionID: sub.ID,
		Instrument:   d.headers["instrument"],
		TimeBasis:    orDefault(d.headers["time_basis"], "furnace_clock"),
	}
	gapAfterRows := map[int]string{}
	for k, v := range d.headers {
		if strings.HasPrefix(k, "gap_after_row") {
			n, err := strconv.Atoi(v)
			if err == nil {
				gapAfterRows[n] = k
			}
		}
	}
	var prev *sources.SamplePoint
	for i, r := range d.rows {
		rowNo := i + 1
		p := sources.SamplePoint{Index: i, RowNo: rowNo, Usable: true}
		p.TimeRaw = d.get(r, "time")
		p.MassRaw = d.get(r, "mass")
		p.TempRaw = d.get(r, "temp")
		p.Time = d.qty(r, "time", rs.TimeBasis, rowNo, &p.Diags)
		p.Mass = d.qty(r, "mass", "pan_corrected", rowNo, &p.Diags)
		p.Temp = d.qty(r, "temp", "furnace_thermocouple", rowNo, &p.Diags)
		for _, dg := range p.Diags {
			if dg.Severity == "error" {
				p.Usable = false
			}
		}
		if prev != nil && p.Usable && prev.Usable {
			if p.Time.Value <= prev.Time.Value {
				p.Diags = append(p.Diags, sources.Diag{Scope: fmt.Sprintf("row:%d", rowNo), Severity: "warn",
					Message: "non-monotonic time axis (noise); point kept, excluded from interval math"})
			}
		}
		rs.Points = append(rs.Points, p)
		prev = &rs.Points[len(rs.Points)-1]
	}
	// Declared acquisition gaps: no interpolation across them downstream.
	for row := range gapAfterRows {
		if row >= 1 && row < len(rs.Points) {
			a, b := rs.Points[row-1], rs.Points[row]
			rs.Gaps = append(rs.Gaps, sources.Gap{
				FromIndex: a.Index, ToIndex: b.Index,
				FromTimeS: a.Time.Value, ToTimeS: b.Time.Value,
				Reason: "declared acquisition gap",
			})
			rs.Points[row-1].GapAfter = true
		}
	}
	rs.Diags = d.diags
	return rs, nil
}

func ParseDrift(sub sources.Submission) (*sources.DriftModel, error) {
	d, err := splitDoc(sub.RawText)
	if err != nil {
		return nil, err
	}
	if d.colIdx("time") < 0 || d.colIdx("drift") < 0 {
		return nil, fmt.Errorf("balance_drift: requires columns time,drift")
	}
	if err := d.requireUnit("time"); err != nil {
		return nil, err
	}
	if err := d.requireUnit("drift"); err != nil {
		return nil, err
	}
	dm := &sources.DriftModel{SubmissionID: sub.ID, Method: orDefault(d.headers["method"], "blank_linear_anchors_v1")}
	for i, r := range d.rows {
		rowNo := i + 1
		t := d.qty(r, "time", "furnace_clock", rowNo, &dm.Diags)
		v := d.qty(r, "drift", "balance_baseline", rowNo, &dm.Diags)
		dm.Anchors = append(dm.Anchors, sources.XY{T: t.Value, V: v.Value})
	}
	return dm, nil
}

func ParseFurnace(sub sources.Submission) (*sources.FurnaceProgram, error) {
	d, err := splitDoc(sub.RawText)
	if err != nil {
		return nil, err
	}
	for _, c := range []string{"kind", "start_time", "end_time", "start_temp", "end_temp"} {
		if d.colIdx(c) < 0 {
			return nil, fmt.Errorf("furnace_program: missing column %q", c)
		}
	}
	fp := &sources.FurnaceProgram{SubmissionID: sub.ID}
	for i, r := range d.rows {
		rowNo := i + 1
		seg := sources.FurnaceSegment{Index: i, Kind: d.get(r, "kind")}
		if seg.Kind != "ramp" && seg.Kind != "hold" {
			return nil, fmt.Errorf("furnace_program row %d: kind must be ramp|hold, got %q", rowNo, seg.Kind)
		}
		seg.StartT = d.qty(r, "start_time", "furnace_clock", rowNo, &fp.Diags)
		seg.EndT = d.qty(r, "end_time", "furnace_clock", rowNo, &fp.Diags)
		seg.StartTemp = d.qty(r, "start_temp", "furnace_setpoint", rowNo, &fp.Diags)
		seg.EndTemp = d.qty(r, "end_temp", "furnace_setpoint", rowNo, &fp.Diags)
		seg.Rate = d.qty(r, "rate", "furnace_setpoint", rowNo, &fp.Diags)
		fp.Segments = append(fp.Segments, seg)
	}
	return fp, nil
}

func ParseGas(sub sources.Submission) (*sources.GasTimeline, error) {
	d, err := splitDoc(sub.RawText)
	if err != nil {
		return nil, err
	}
	for _, c := range []string{"time", "to_gas"} {
		if d.colIdx(c) < 0 {
			return nil, fmt.Errorf("gas_valve: missing column %q", c)
		}
	}
	if err := d.requireUnit("time"); err != nil {
		return nil, err
	}
	gt := &sources.GasTimeline{SubmissionID: sub.ID}
	for i, r := range d.rows {
		rowNo := i + 1
		gv := sources.GasValve{Index: i, FromGas: d.get(r, "from_gas"), ToGas: d.get(r, "to_gas"), Note: d.get(r, "note")}
		gv.Time = d.qty(r, "time", "furnace_clock", rowNo, &gt.Diags)
		gt.Valves = append(gt.Valves, gv)
	}
	return gt, nil
}

func ParseBlank(sub sources.Submission) (*sources.BlankCurve, error) {
	d, err := splitDoc(sub.RawText)
	if err != nil {
		return nil, err
	}
	for _, c := range []string{"time", "mass"} {
		if d.colIdx(c) < 0 {
			return nil, fmt.Errorf("blank_curve: missing column %q", c)
		}
	}
	bc := &sources.BlankCurve{SubmissionID: sub.ID, PanID: d.headers["pan_id"]}
	for i, r := range d.rows {
		rowNo := i + 1
		bp := sources.BlankPoint{Index: i}
		bp.Time = d.qty(r, "time", "furnace_clock", rowNo, &bc.Diags)
		bp.Mass = d.qty(r, "mass", "pan_corrected", rowNo, &bc.Diags)
		bp.Temp = d.qty(r, "temp", "furnace_thermocouple", rowNo, &bc.Diags)
		bc.Points = append(bc.Points, bp)
	}
	return bc, nil
}

func ParseInitial(sub sources.Submission) (*sources.InitialMass, error) {
	d, err := splitDoc(sub.RawText)
	if err != nil {
		return nil, err
	}
	if d.colIdx("mass") < 0 {
		return nil, fmt.Errorf("initial_mass: missing column mass")
	}
	if err := d.requireUnit("mass"); err != nil {
		return nil, err
	}
	if len(d.rows) != 1 {
		return nil, fmt.Errorf("initial_mass: exactly one data row required, got %d", len(d.rows))
	}
	im := &sources.InitialMass{
		SubmissionID: sub.ID,
		Basis:        orDefault(d.headers["basis"], "tare_settled"),
		WeighedAt:    d.headers["weighed_at"],
		Note:         d.headers["note"],
	}
	im.Mass = d.qty(d.rows[0], "mass", im.Basis, 1, &im.Diags)
	return im, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
