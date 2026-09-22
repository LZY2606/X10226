// Package importer parses the domain-specific .tga text format. Original
// tokens and lines are always preserved; normalization happens only in the
// derivation layer, and unconvertible values stay in place with diagnostics.
package importer

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"strings"

	"tga-review/internal/domain"
)

type Parser struct{}

func New() *Parser { return &Parser{} }

// Parse turns one submission into an ImportBatch. Records carry no project id
// yet; the service stamps project/sequence information on commit.
func (p *Parser) Parse(filename, mode, raw string) *domain.ImportBatch {
	b := &domain.ImportBatch{
		Filename: filename,
		Mode:     mode,
		Raw:      raw,
		Present:  map[string]bool{},
	}
	layerCounter := map[string]int{}
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		fields := strings.Fields(trimmed)
		kind, err := classify(fields)
		if err != nil {
			b.Diagnostics = append(b.Diagnostics, domain.Diagnostic{
				Severity: domain.SevError, Code: "unknown_section",
				LineNo: lineNo, Message: err.Error(), RawText: trimmed,
			})
			continue
		}
		layerCounter[kind]++
		if kind == "params" {
			// validated structurally below
		}
		rec := domain.SourceRecord{
			Kind: kind, LayerIndex: layerCounter[kind], LineNo: lineNo,
			RawText: trimmed, Fields: fields, Revision: 1,
			Status: "accepted",
		}
		rec.ID = stableID(kind, rec.LayerIndex, lineNo, trimmed)
		b.Records = append(b.Records, rec)
		b.Present[kind] = true
	}

	// Semantic checks per record; unconvertible values stay in place.
	for idx := range b.Records {
		rec := &b.Records[idx]
		if d := checkRecord(rec); d != nil {
			if strings.HasPrefix(d.Code, "unconvertible") {
				d.Severity = domain.SevWarning
				rec.Status = "unconvertible"
			} else {
				rec.Status = "rejected"
			}
			rec.Diagnostic = d.Message
			b.Diagnostics = append(b.Diagnostics, *d)
		}
	}

	b.Status = "accepted"
	if len(b.Diagnostics) > 0 {
		for _, d := range b.Diagnostics {
			if d.Severity == domain.SevError {
				b.Status = "quarantined"
			}
		}
	}
	if mode == "full" {
		for _, req := range []string{domain.SrcPoints, domain.SrcInitial, domain.SrcFurnace} {
			if !b.Present[req] {
				b.Status = "quarantined"
				b.Diagnostics = append(b.Diagnostics, domain.Diagnostic{
					Severity: domain.SevError,
					Code:     "missing_required_layer",
					Message:  fmt.Sprintf("required layer %q is absent", req),
				})
			}
		}
	}
	return b
}

func classify(f []string) (string, error) {
	switch f[0] {
	case "point":
		return domain.SrcPoints, nil
	case "initial_mass":
		return domain.SrcInitial, nil
	case "drift":
		return domain.SrcDrift, nil
	case "furnace":
		return domain.SrcFurnace, nil
	case "gas":
		return domain.SrcGas, nil
	case "blank":
		return domain.SrcBlank, nil
	case "params":
		return "params", nil
	}
	return "", fmt.Errorf("unrecognized directive %q", f[0])
}

func checkRecord(rec *domain.SourceRecord) *domain.Diagnostic {
	f := rec.Fields
	mkErr := func(code, msg string) *domain.Diagnostic {
		return &domain.Diagnostic{
			Severity: domain.SevError, Code: code, LineNo: rec.LineNo,
			Message: msg, RawText: rec.RawText,
		}
	}
	switch rec.Kind {
	case domain.SrcPoints:
		if len(f) != 7 {
			return mkErr("bad_arity", "point needs: point <t> <tu> <T> <Tu> <m> <mu>")
		}
		if _, _, e := NormalizeTime(f[1], f[2]); e != nil {
			return mkErr("unconvertible_time", e.Error())
		}
		if _, _, e := NormalizeTemp(f[3], f[4]); e != nil {
			return mkErr("unconvertible_temp", e.Error())
		}
		if _, _, e := NormalizeMass(f[5], f[6]); e != nil {
			return mkErr("unconvertible_mass", e.Error())
		}
	case domain.SrcInitial:
		if len(f) != 3 {
			return mkErr("bad_arity", "initial_mass needs: initial_mass <m> <mu>")
		}
		if _, _, e := NormalizeMass(f[1], f[2]); e != nil {
			return mkErr("unconvertible_mass", e.Error())
		}
	case domain.SrcDrift:
		if len(f) < 2 {
			return mkErr("bad_arity", "drift needs: drift none | drift linear <rate> <unit>")
		}
		if f[1] == "none" {
			return nil
		}
		if f[1] != "linear" || len(f) != 4 {
			return mkErr("bad_drift", "drift needs: drift linear <rate> mg/h")
		}
		rate, unit := f[2], strings.ToLower(f[3])
		if !isNumber(rate) {
			return mkErr("unconvertible_drift", "drift rate must be numeric")
		}
		switch unit {
		case "mg/h", "mg/min":
		default:
			return mkErr("unconvertible_drift_unit", "drift unit must be mg/h or mg/min")
		}
	case domain.SrcFurnace:
		if len(f) < 2 {
			return mkErr("bad_arity", "furnace needs ramp/hold clause")
		}
		switch f[1] {
		case "hold":
			if len(f) != 8 {
				return mkErr("bad_arity", "furnace hold <from> <tu> <to> <tu> <tempC> C")
			}
			if _, _, e := NormalizeTime(f[2], f[3]); e != nil {
				return mkErr("unconvertible_time", e.Error())
			}
			if _, _, e := NormalizeTime(f[4], f[5]); e != nil {
				return mkErr("unconvertible_time", e.Error())
			}
			if !isNumber(f[6]) || strings.ToLower(f[7]) != "c" {
				return mkErr("unconvertible_temp", "hold temperature must be numeric C")
			}
		case "ramp":
			if len(f) != 9 {
				return mkErr("bad_arity", "furnace ramp <from> <tu> <to> <tu> <T0> <T1> C")
			}
			if _, _, e := NormalizeTime(f[2], f[3]); e != nil {
				return mkErr("unconvertible_time", e.Error())
			}
			if _, _, e := NormalizeTime(f[4], f[5]); e != nil {
				return mkErr("unconvertible_time", e.Error())
			}
			if !isNumber(f[6]) || !isNumber(f[7]) {
				return mkErr("unconvertible_temp", "ramp temperatures must be numeric")
			}
			if strings.ToLower(f[8]) != "c" {
				return mkErr("unconvertible_temp_unit", "furnace temperatures must be in C")
			}
		default:
			return mkErr("bad_furnace", "furnace clause must be ramp or hold")
		}
	case domain.SrcGas:
		// gas switch <at> <tu> <from> <to> <flow> mL/min
		if len(f) != 9 || f[1] != "switch" {
			return mkErr("bad_arity", "gas switch <at> <tu> <from> <to> <flow> mL/min")
		}
		if _, _, e := NormalizeTime(f[2], f[3]); e != nil {
			return mkErr("unconvertible_time", e.Error())
		}
		if !isNumber(f[6]) {
			return mkErr("unconvertible_flow", "gas flow must be numeric")
		}
		if strings.ToLower(f[7]+f[8]) != "ml/min" {
			return mkErr("unconvertible_flow_unit", "gas flow unit must be mL/min")
		}
	case "params":
		if len(f) != 7 || f[1] != "delay" || f[4] != "lag" {
			return mkErr("bad_arity", "params delay <d> <tu> lag <l> C")
		}
		if _, _, e := NormalizeTime(f[2], f[3]); e != nil {
			return mkErr("unconvertible_time", e.Error())
		}
		if !isNumber(f[5]) || strings.ToLower(f[6]) != "c" {
			return mkErr("unconvertible_temp", "lag must be numeric C")
		}
	case domain.SrcBlank:
		if len(f) != 5 {
			return mkErr("bad_arity", "blank <t> <tu> <m> <mu>")
		}
		if _, _, e := NormalizeTime(f[1], f[2]); e != nil {
			return mkErr("unconvertible_time", e.Error())
		}
		if _, _, e := NormalizeMass(f[3], f[4]); e != nil {
			return mkErr("unconvertible_mass", e.Error())
		}
	}
	return nil
}

func isNumber(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '+' && r != 'e' && r != 'E' {
			return false
		}
	}
	_, err := parseFloat(s)
	return err == nil
}

func stableID(kind string, layer, line int, raw string) string {
	h := sha1.Sum([]byte(raw))
	return fmt.Sprintf("rec-%s-l%d-n%d-%s", kind, layer, line, hex.EncodeToString(h[:4]))
}
