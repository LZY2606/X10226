package core

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type ParsedBundle struct {
	Import     RawImport
	Records    []SourceRecord
	Parameters *Parameters
}

func ParseBundle(bundle SourceBundle) (ParsedBundle, error) {
	raw, _ := json.Marshal(bundle)
	sum := sha256.Sum256(raw)
	out := ParsedBundle{}
	imp := RawImport{
		BatchID:   bundle.BatchID,
		ParentID:  bundle.ParentID,
		Label:     bundle.Label,
		Actor:     bundle.Actor,
		RawSHA256: hex.EncodeToString(sum[:]),
		Sections:  bundle.Sections,
	}
	for _, section := range bundle.Sections {
		records, params, diags, ok := parseSection(bundle.BatchID, section, imp.ParentID)
		imp.Diagnostics = append(imp.Diagnostics, diags...)
		if ok {
			imp.AcceptedKinds = append(imp.AcceptedKinds, section.Kind)
			out.Records = append(out.Records, records...)
			if params != nil {
				out.Parameters = params
			}
		} else {
			imp.RejectedKinds = append(imp.RejectedKinds, section.Kind)
		}
	}
	imp.Partial = len(imp.RejectedKinds) > 0
	out.Import = imp
	return out, nil
}

func parseSection(batchID string, section SourceSection, parentID string) ([]SourceRecord, *Parameters, []Diagnostic, bool) {
	rows, diags := readRows(section)
	if len(rows) == 0 {
		diags = append(diags, Diagnostic{Code: "FORMAT_EMPTY", Severity: "error", Message: section.Kind + " has no data rows", Subject: section.Filename})
		return nil, nil, diags, false
	}
	var records []SourceRecord
	var params *Parameters
	switch section.Kind {
	case KindSamples:
		records = parseSamples(batchID, rows, &diags)
	case KindDrift:
		records = parseDrift(batchID, rows, parentID, &diags)
	case KindFurnace:
		records = parseFurnace(batchID, rows, &diags)
	case KindValves:
		records = parseValves(batchID, rows, &diags)
	case KindBlank:
		records = parseBlank(batchID, rows, &diags)
	case KindInitialMass:
		records = parseInitialMass(batchID, rows, parentID, &diags)
	case KindParameters:
		params = parseParameters(batchID, rows, parentID, &diags)
	default:
		diags = append(diags, Diagnostic{Code: "FORMAT_UNKNOWN_KIND", Severity: "error", Message: "unknown section kind: " + section.Kind, Subject: section.Filename})
	}
	ok := section.Kind != KindParameters && len(records) > 0 || section.Kind == KindParameters && params != nil
	return records, params, diags, ok
}

func readRows(section SourceSection) ([][]string, []Diagnostic) {
	r := csv.NewReader(strings.NewReader(section.Content))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	all, err := r.ReadAll()
	if err != nil {
		return nil, []Diagnostic{{Code: "FORMAT_CSV", Severity: "error", Message: err.Error(), Subject: section.Filename}}
	}
	if len(all) < 2 {
		return nil, nil
	}
	return all[1:], nil
}

func parseSamples(batchID string, rows [][]string, diags *[]Diagnostic) []SourceRecord {
	var points []SamplePoint
	for i, row := range rows {
		raw := strings.Join(row, ",")
		if len(row) != 3 {
			*diags = append(*diags, Diagnostic{Code: "FORMAT_SAMPLE_COLUMNS", Severity: "error", Message: "expected time_s,temperature_c,mass_mg", Subject: fmt.Sprintf("%s:%d", batchID, i+2)})
			continue
		}
		p := SamplePoint{SourceID: batchID, Seq: i + 1, Time: qty(row[0], "s", "furnace_time"), Temperature: qty(row[1], "degC", "sample_program_temperature"), Mass: qty(row[2], "mg", "balance_mass"), RawRow: raw}
		if !p.Time.OK || !p.Temperature.OK || !p.Mass.OK {
			p.RawRow = raw
		}
		points = append(points, p)
	}
	return wrapRecords(batchID, KindSamples, points)
}

func parseDrift(batchID string, rows [][]string, parentID string, diags *[]Diagnostic) []SourceRecord {
	if len(rows) == 0 || len(rows[0]) < 6 {
		*diags = append(*diags, Diagnostic{Code: "FORMAT_DRIFT_COLUMNS", Severity: "error", Message: "expected correction_id,start_s,end_s,rate_mg_per_min,blank_curve_id,model"})
		return nil
	}
	row := rows[0]
	id := clean(row[0])
	d := DriftCorrection{SourceID: batchID, ParentID: parentID, CorrectionID: id, Model: clean(row[5]), Start: qty(row[1], "s", "drift_window"), End: qty(row[2], "s", "drift_window"), Rate: qty(row[3], "mg/min", "drift_rate"), BlankCurveID: clean(row[4]), Algorithm: "linear_baseline_v1"}
	d.Fingerprint = CanonicalFingerprint(id, row[1], row[2], row[3], row[4], row[5])
	return wrapRecords(batchID, KindDrift, []DriftCorrection{d})
}

func parseFurnace(batchID string, rows [][]string, diags *[]Diagnostic) []SourceRecord {
	var out []FurnacePoint
	for i, row := range rows {
		if len(row) != 3 {
			*diags = append(*diags, Diagnostic{Code: "FORMAT_FURNACE_COLUMNS", Severity: "error", Message: "expected time_s,setpoint_c,measured_c", Subject: fmt.Sprintf("%s:%d", batchID, i+2)})
			continue
		}
		out = append(out, FurnacePoint{SourceID: batchID, Seq: i + 1, Time: qty(row[0], "s", "furnace_clock"), Setpoint: qty(row[1], "degC", "furnace_setpoint"), Measured: qty(row[2], "degC", "sample_temperature")})
	}
	return wrapRecords(batchID, KindFurnace, out)
}

func parseValves(batchID string, rows [][]string, diags *[]Diagnostic) []SourceRecord {
	var out []GasValveEvent
	for i, row := range rows {
		if len(row) < 7 {
			*diags = append(*diags, Diagnostic{Code: "FORMAT_VALVE_COLUMNS", Severity: "error", Message: "expected event_id,time_s,from,to,delay_s,lag_s,note", Subject: fmt.Sprintf("%s:%d", batchID, i+2)})
			continue
		}
		id := clean(row[0])
		delay := qty(row[4], "s", "gas_transport_delay")
		lag := qty(row[5], "s", "sample_temperature_lag")
		timeQ := qty(row[1], "s", "valve_clock")
		reaction := Quantity{Unit: "s", Coordinate: "estimated_sample_reaction", OK: timeQ.OK && delay.OK && lag.OK}
		if reaction.OK {
			reaction.Value = timeQ.Value + delay.Value + lag.Value
			reaction.Original = strconv.FormatFloat(reaction.Value, 'f', 3, 64)
		}
		out = append(out, GasValveEvent{SourceID: batchID, EventID: id, Time: timeQ, FromGas: clean(row[2]), ToGas: clean(row[3]), Delay: delay, ThermalLag: lag, ReactionStart: reaction, Note: clean(orAt(row, 6))})
	}
	return wrapRecords(batchID, KindValves, out)
}

func parseBlank(batchID string, rows [][]string, diags *[]Diagnostic) []SourceRecord {
	var out []BlankPoint
	curveID := "blank-" + batchID
	for i, row := range rows {
		if len(row) != 3 {
			*diags = append(*diags, Diagnostic{Code: "FORMAT_BLANK_COLUMNS", Severity: "error", Message: "expected blank_curve_id,time_s,baseline_mass_mg", Subject: fmt.Sprintf("%s:%d", batchID, i+2)})
			continue
		}
		curveID = clean(row[0])
		out = append(out, BlankPoint{SourceID: batchID, BlankCurveID: curveID, Seq: i + 1, Time: qty(row[1], "s", "blank_furnace_time"), BaselineMass: qty(row[2], "mg", "blank_balance_mass")})
	}
	return wrapRecords(batchID, KindBlank, out)
}

func parseInitialMass(batchID string, rows [][]string, diags *[]Diagnostic) []SourceRecord {
	if len(rows) == 0 || len(rows[0]) < 3 {
		*diags = append(*diags, Diagnostic{Code: "FORMAT_INITIAL_MASS_COLUMNS", Severity: "error", Message: "expected revision_id,mass_mg,basis"})
		return nil
	}
	row := rows[0]
	m := InitialMass{SourceID: batchID, ParentID: parentID, RevisionID: clean(row[0]), Mass: qty(row[1], "mg", "initial_sample_mass"), Basis: clean(row[2])}
	if len(row) > 3 {
		m.Note = clean(row[3])
	}
	return wrapRecords(batchID, KindInitialMass, []InitialMass{m})
}

func parseParameters(batchID string, rows [][]string, parentID string, diags *[]Diagnostic) *Parameters {
	if len(rows) == 0 || len(rows[0]) < 5 {
		*diags = append(*diags, Diagnostic{Code: "FORMAT_PARAMETERS_COLUMNS", Severity: "error", Message: "expected smoothing_window_s,dtg_threshold,overlap_ratio,minimum_stage_s,algorithm_version"})
		return nil
	}
	row := rows[0]
	p := Parameters{SourceID: batchID, ParentID: parentID, SmoothingWindowSeconds: num(row[0]), DTGThresholdMgPerMin: num(row[1]), OverlapShoulderRatio: num(row[2]), MinimumStageSeconds: num(row[3]), AlgorithmVersion: clean(row[4])}
	p.Fingerprint = CanonicalFingerprint(row[0], row[1], row[2], row[3], row[4])
	return &p
}

func wrapRecord(batchID, kind, recordID, parentID string, value any) SourceRecord {
	data, _ := json.Marshal(value)
	return SourceRecord{BatchID: batchID, Kind: kind, RecordID: recordID, ParentID: parentID, Data: data}
}

func wrapRecords[T any](batchID, kind string, values []T) []SourceRecord {
	out := make([]SourceRecord, 0, len(values))
	for _, value := range values {
		id := PrefixedID(kind, batchID, fmt.Sprintf("%#v", value))
		out = append(out, wrapRecord(batchID, kind, id, "", value))
	}
	return out
}

func qty(raw, unit, coordinate string) Quantity {
	text := clean(raw)
	value, err := strconv.ParseFloat(text, 64)
	q := Quantity{Original: raw, Unit: unit, Coordinate: coordinate}
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) {
		q.OK = false
		q.DiagnosticID = "UNCONVERTIBLE_VALUE"
		return q
	}
	q.Value = value
	q.OK = true
	return q
}

func num(raw string) float64 {
	v, _ := strconv.ParseFloat(clean(raw), 64)
	return v
}

func clean(v string) string { return strings.TrimSpace(v) }
func orAt(row []string, i int) string {
	if i < len(row) {
		return row[i]
	}
	return ""
}
