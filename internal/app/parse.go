package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	SourceSamplePoint       = "sample_point"
	SourceBalanceCorrection = "balance_correction"
	SourceFurnaceProgram    = "furnace_program"
	SourceGasValve          = "gas_valve_event"
	SourceBlankCurve        = "blank_curve"
	SourceInitialMass       = "initial_mass"
	SourceParameterRevision = "parameter_revision"
)

type RawRecordPayload struct {
	BatchID        string            `json:"batch_id"`
	RecordID       string            `json:"record_id"`
	LogicalID      string            `json:"logical_id"`
	Version        int               `json:"version"`
	ParentVersion  *int              `json:"parent_version,omitempty"`
	SourceType     string            `json:"source_type"`
	MaterialID     string            `json:"material_id"`
	OriginalText   string            `json:"original_text"`
	Normalized     json.RawMessage   `json:"normalized"`
	Status         string            `json:"status"`
	Diagnostics    []Diagnostic      `json:"diagnostics,omitempty"`
	SourceSequence int               `json:"source_sequence"`
}

type ImportSummaryPayload struct {
	BatchID    string       `json:"batch_id"`
	Status     string       `json:"status"`
	Accepted   int          `json:"accepted"`
	Rejected   int          `json:"rejected"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type ParsedImport struct {
	Records []RawRecordPayload
	Report  ImportReport
}

type valueWithUnit struct {
	Raw      json.RawMessage `json:"raw"`
	Value    *float64        `json:"value,omitempty"`
	Unit     string          `json:"unit,omitempty"`
	Basis    string          `json:"basis,omitempty"`
	Diagnosis *Diagnostic    `json:"diagnosis,omitempty"`
}

func (a *App) ImportText(ctx context.Context, projectID, actor, text string) (ImportReport, error) {
	parsed := ParseImport(projectID, text)
	now := a.clock()
	batchID := ""
	for _, record := range parsed.Records {
		if batchID == "" {
			batchID = record.BatchID
		}
		payload, _ := json.Marshal(record)
		err := a.appendEvent(ctx, projectID, "raw_record_submitted", actor, now, payload, func(tx *interfaceTx) error { return nil })
		_ = err
	}
	return ImportReport{}, nil
}

type interfaceTx = struct{}

func ParseImport(projectID, text string) ParsedImport {
	batchSum := sha256.Sum256([]byte(projectID + "\n" + text))
	batchID := "batch_" + hex.EncodeToString(batchSum[:])[:12]
	var rows []json.RawMessage
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal([]byte(trimmed), &rows); err != nil {
			return rejectBatch(batchID, Diagnostic{Severity: "error", Code: "IMPORT_JSON_ARRAY_INVALID", Message: err.Error(), Raw: trimmed})
		}
	} else {
		dec := json.NewDecoder(bytes.NewReader([]byte(text)))
		for dec.More() {
			var row json.RawMessage
			if err := dec.Decode(&row); err != nil {
				return rejectBatch(batchID, Diagnostic{Severity: "error", Code: "IMPORT_JSONL_INVALID", Message: err.Error(), Raw: lineAround(text, int(dec.InputOffset()))})
			}
			rows = append(rows, row)
		}
	}
	result := ParsedImport{Report: ImportReport{Status: "accepted", LastSeq: -1}}
	seenLogical := map[string]int{}
	for i, row := range rows {
		var generic map[string]json.RawMessage
		if err := json.Unmarshal(row, &generic); err != nil {
			reject(&result, i, row, Diagnostic{Severity: "error", Code: "RECORD_NOT_OBJECT", Message: err.Error()})
			continue
		}
		sourceType := stringValue(generic["source_type"])
		if !validSourceType(sourceType) {
			reject(&result, i, row, Diagnostic{Severity: "error", Code: "UNKNOWN_SOURCE_TYPE", Message: "source_type must be one of the six immutable TGA layers or parameter_revision"})
			continue
		}
		logicalID := stringValue(generic["logical_id"])
		if logicalID == "" {
			logicalID = fmt.Sprintf("%s:%d", sourceType, i+1)
		}
		version := 1
		if raw, ok := generic["version"]; ok {
			if err := json.Unmarshal(raw, &version); err != nil || version < 1 {
				reject(&result, i, row, Diagnostic{Severity: "error", Code: "INVALID_VERSION", Message: "version must be a positive integer"})
				continue
			}
		}
		if previous, ok := seenLogical[logicalID]; ok && version <= previous {
			reject(&result, i, row, Diagnostic{Severity: "error", Code: "VERSION_ORDER_INVALID", Message: fmt.Sprintf("%s version %d must be greater than %d", logicalID, version, previous)})
			continue
		}
		seenLogical[logicalID] = version
		materialID := stringValue(generic["material_id"])
		if materialID == "" {
			materialID = projectID
		}
		var parent *int
		if raw, ok := generic["parent_version"]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, &parent); err != nil || parent == nil || *parent >= version {
				reject(&result, i, row, Diagnostic{Severity: "error", Code: "INVALID_PARENT_VERSION", Message: "parent_version must reference an earlier imported version"})
				continue
			}
		}
		if version > 1 && parent == nil {
			reject(&result, i, row, Diagnostic{Severity: "error", Code: "REVISION_PARENT_REQUIRED", Message: "versions greater than one require parent_version"})
			continue
		}
		normalized, diags := normalizeRecord(sourceType, generic)
		status := "accepted"
		for _, diagnostic := range diags {
			if diagnostic.Severity == "error" {
				status = "accepted_with_unconverted_value"
			}
		}
		record := RawRecordPayload{
			BatchID:        batchID,
			RecordID:       stableID("raw", projectID, logicalID, version),
			LogicalID:      logicalID,
			Version:        version,
			ParentVersion:  parent,
			SourceType:     sourceType,
			MaterialID:     materialID,
			OriginalText:   string(row),
			Normalized:     normalized,
			Status:         status,
			Diagnostics:    diags,
			SourceSequence: i + 1,
		}
		result.Records = append(result.Records, record)
		result.Report.Accepted++
		result.Report.AcceptedRecordIDs = append(result.Report.AcceptedRecordIDs, record.RecordID)
		result.Report.Diagnostics = append(result.Report.Diagnostics, diags...)
	}
	if result.Report.Rejected > 0 {
		result.Report.Status = "partial_failure"
	} else if hasErrorDiagnostic(result.Report.Diagnostics) {
		result.Report.Status = "accepted_with_diagnostics"
	}
	return result
}

func rejectBatch(batchID string, diagnostic Diagnostic) ParsedImport {
	diagnostic.Severity = "error"
	return ParsedImport{Report: ImportReport{Status: "failed", Rejected: 1, LastSeq: -1, Diagnostics: []Diagnostic{diagnostic}}}
}

func reject(result *ParsedImport, index int, row json.RawMessage, diagnostic Diagnostic) {
	diagnostic.Severity = "error"
	diagnostic.Index = index + 1
	diagnostic.Raw = string(row)
	result.Report.Rejected++
	result.Report.Status = "partial_failure"
	result.Report.Diagnostics = append(result.Report.Diagnostics, diagnostic)
}

func validSourceType(sourceType string) bool {
	switch sourceType {
	case SourceSamplePoint, SourceBalanceCorrection, SourceFurnaceProgram, SourceGasValve, SourceBlankCurve, SourceInitialMass, SourceParameterRevision:
		return true
	}
	return false
}

func normalizeRecord(sourceType string, generic map[string]json.RawMessage) (json.RawMessage, []Diagnostic) {
	out := map[string]any{}
	var diags []Diagnostic
	switch sourceType {
	case SourceSamplePoint:
		out["time"] = normalizeValue(generic["time"], "s", "furnace_elapsed")
		out["temperature"] = normalizeValue(generic["temperature"], "C", "sample_program_temperature")
		out["mass"] = normalizeValue(generic["mass"], "mg", "balance_observed")
		if out["time"].(valueWithUnit).Value == nil || out["mass"].(valueWithUnit).Value == nil {
			diags = append(diags, Diagnostic{Severity: "error", Code: "UNCONVERTIBLE_SAMPLE_POINT", Message: "time and mass are required and stay at their original position"})
		}
	case SourceBalanceCorrection:
		out["time"] = normalizeValue(generic["time"], "s", "furnace_elapsed")
		out["drift"] = normalizeValue(generic["drift"], "mg", "balance_baseline")
	case SourceFurnaceProgram:
		out["start_time"] = normalizeValue(generic["start_time"], "s", "furnace_elapsed")
		out["end_time"] = normalizeValue(generic["end_time"], "s", "furnace_elapsed")
		out["start_temperature"] = normalizeValue(generic["start_temperature"], "C", "program_setpoint")
		out["end_temperature"] = normalizeValue(generic["end_temperature"], "C", "program_setpoint")
	case SourceGasValve:
		out["time"] = normalizeValue(generic["time"], "s", "valve_controller")
		out["from_gas"] = stringValue(generic["from_gas"])
		out["to_gas"] = stringValue(generic["to_gas"])
	case SourceBlankCurve:
		out["time"] = normalizeValue(generic["time"], "s", "furnace_elapsed")
		out["blank_mass"] = normalizeValue(generic["blank_mass"], "mg", "empty_pan_balance")
	case SourceInitialMass:
		out["mass"] = normalizeValue(generic["mass"], "mg", "sample_initial_mass")
		out["measured_at"] = stringValue(generic["measured_at"])
	case SourceParameterRevision:
		out["parameters"] = copyRaw(generic["parameters"])
		out["reason"] = stringValue(generic["reason"])
	}
	for key, value := range generic {
		if _, exists := out[key]; !exists {
			out[key] = copyRaw(value)
		}
	}
	encoded, _ := json.Marshal(out)
	return encoded, attachSource(diags, sourceType)
}

func normalizeValue(raw json.RawMessage, defaultUnit, basis string) valueWithUnit {
	result := valueWithUnit{Raw: copyRaw(raw), Basis: basis}
	if len(raw) == 0 {
		result.Diagnosis = &Diagnostic{Severity: "error", Code: "MISSING_VALUE", Message: "value is missing and was not replaced for plotting"}
		return result
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err == nil {
		if valueRaw, ok := object["value"]; ok {
			value, err := numberValue(valueRaw)
			if err != nil {
				result.Diagnosis = &Diagnostic{Severity: "error", Code: "UNCONVERTIBLE_UNIT_VALUE", Message: err.Error()}
			} else {
				result.Value = &value
			}
		}
		if unitRaw, ok := object["unit"]; ok {
			result.Unit = stringValue(unitRaw)
		}
		if basisRaw, ok := object["basis"]; ok {
			result.Basis = stringValue(basisRaw)
		}
		if result.Unit == "" {
			result.Unit = defaultUnit
		}
		if result.Value != nil && result.Unit != defaultUnit {
			result.Diagnosis = &Diagnostic{Severity: "error", Code: "UNIT_NOT_NORMALIZED", Message: fmt.Sprintf("unit %q retained; derived layer expects %q", result.Unit, defaultUnit)}
			result.Value = nil
		}
		return result
	}
	value, err := numberValue(raw)
	if err != nil {
		result.Diagnosis = &Diagnostic{Severity: "error", Code: "UNCONVERTIBLE_NUMBER", Message: err.Error()}
		return result
	}
	result.Value = &value
	result.Unit = defaultUnit
	return result
}

func numberValue(raw json.RawMessage) (float64, error) {
	var number float64
	if err := json.Unmarshal(raw, &number); err != nil {
		var text string
		if err2 := json.Unmarshal(raw, &text); err2 == nil {
			parsed, err3 := strconv.ParseFloat(text, 64)
			if err3 == nil {
				return parsed, nil
			}
		}
		return 0, err
	}
	return number, nil
}

func stringValue(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return strings.Trim(string(raw), "\"")
}

func copyRaw(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return json.RawMessage("null")
	}
	return append(json.RawMessage(nil), raw...)
}

func attachSource(diags []Diagnostic, source string) []Diagnostic {
	for i := range diags {
		if diags[i].Source == "" {
			diags[i].Source = source
		}
	}
	return diags
}

func hasErrorDiagnostic(diags []Diagnostic) bool {
	for _, diagnostic := range diags {
		if diagnostic.Severity == "error" {
			return true
		}
	}
	return false
}

func lineAround(text string, offset int) string {
	if offset < 0 || offset > len(text) {
		return text
	}
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	end := strings.IndexByte(text[offset:], '\n')
	if end < 0 {
		return strings.TrimSpace(text[start:])
	}
	return strings.TrimSpace(text[start : offset+end])
}
