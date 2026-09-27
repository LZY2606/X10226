package parse

import (
	"encoding/json"
	"fmt"
	"strings"

	"tga/internal/domain"
)

// LayerDocument is the JSON envelope for one or all six layers.
type LayerDocument struct {
	Layer   domain.Layer      `json:"layer"`
	Units   map[string]string `json:"units,omitempty"`
	Columns []string          `json:"columns,omitempty"`
	Points  []map[string]any  `json:"points,omitempty"`
	Drift   *DriftDoc         `json:"drift,omitempty"`
	Events  []map[string]any  `json:"events,omitempty"`
	Program []map[string]any  `json:"program,omitempty"`
	Mass    *map[string]any   `json:"mass,omitempty"`
}

// DriftDoc is the JSON form of a balance drift specification.
type DriftDoc struct {
	Model     string `json:"model"`
	Rate      string `json:"rate"`
	Intercept string `json:"intercept"`
	AnchorAt  string `json:"anchor_at"`
}

// ParseInput parses one submitted text for the declared/guessed layer.
// Format errors are returned as error-level diagnostics inside the layer;
// a completely unrecognizable envelope yields (nil, error) so the importer
// can mark the file as rejected while still recording its raw bytes.
func ParseInput(mediaType, text string, declared domain.Layer) (*domain.ParsedLayer, []domain.Diag) {
	mt := strings.ToLower(mediaType)
	switch {
	case strings.Contains(mt, "json") || strings.HasPrefix(strings.TrimSpace(text), "{"):
		return parseJSON(text, declared)
	case strings.Contains(mt, "csv") || strings.Contains(mt, "text") || looksCSV(text):
		return parseCSV(text, declared)
	default:
		return nil, []domain.Diag{{Severity: "error", Code: "unknown_media_type",
			Message: "cannot detect format from media type " + mediaType}}
	}
}

func looksCSV(text string) bool {
	first := strings.SplitN(text, "\n", 2)[0]
	return strings.Contains(first, ",") && !strings.Contains(first, "{")
}

func parseJSON(text string, declared domain.Layer) (*domain.ParsedLayer, []domain.Diag) {
	var doc LayerDocument
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		return nil, []domain.Diag{{Severity: "error", Code: "invalid_json",
			Message: "JSON parse error: " + err.Error()}}
	}
	layer := doc.Layer
	if layer == "" {
		layer = declared
	}
	if !layer.Valid() {
		return nil, []domain.Diag{{Severity: "error", Code: "missing_layer",
			Message: "document has no recognizable 'layer'"}}
	}
	out := &domain.ParsedLayer{Layer: layer}
	switch layer {
	case domain.LayerRaw:
		parsePointRows(out, doc.Points, []string{"time", "temp", "mass"})
	case domain.LayerBlank:
		parseBlankRows(out, doc.Points)
	case domain.LayerDrift:
		parseDriftDoc(out, doc.Drift)
	case domain.LayerFurnace:
		parseFurnaceDoc(out, doc.Program)
	case domain.LayerGas:
		parseGasDoc(out, doc.Events)
	case domain.LayerInitMass:
		if doc.Mass == nil {
			out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(layer),
				Code: "missing_mass", Message: "initial_mass document needs 'mass'"})
		} else {
			parseInitMassDoc(out, *doc.Mass)
		}
	}
	return out, out.Diags
}

func atStr(m map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return fmt.Sprintf("%v", v), true
		}
	}
	return "", false
}

func parsePointRows(out *domain.ParsedLayer, rows []map[string]any, fields []string) {
	if len(rows) == 0 {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Code: "empty_points", Message: "points array is empty"})
		return
	}
	for i, r := range rows {
		row := i + 1
		p := domain.RawPoint{Row: row, Valid: true}
		if s, ok := atStr(r, "time", "t", "furnace_time"); ok {
			p.Time = parseValue(s, "time", func(msg string) {
				p.Valid = false
				p.Diag = msg
				out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
					Row: row, Field: "time", Code: "bad_value", Message: msg, Raw: s})
			})
		} else {
			p.Valid = false
			out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
				Row: row, Field: "time", Code: "missing_field", Message: "missing time"})
		}
		if s, ok := atStr(r, "temp", "temperature", "sample_temp"); ok {
			p.Temp = parseValue(s, "temp", func(msg string) {
				p.Valid = false
				p.Diag = msg
				out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
					Row: row, Field: "temp", Code: "bad_value", Message: msg, Raw: s})
			})
		}
		if s, ok := atStr(r, "mass", "m", "weight"); ok {
			p.Mass = parseValue(s, "mass", func(msg string) {
				p.Valid = false
				p.Diag = msg
				out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
					Row: row, Field: "mass", Code: "bad_value", Message: msg, Raw: s})
			})
		} else {
			p.Valid = false
			out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
				Row: row, Field: "mass", Code: "missing_field", Message: "missing mass"})
		}
		out.Raw = append(out.Raw, p)
	}
}

func parseBlankRows(out *domain.ParsedLayer, rows []map[string]any) {
	if len(rows) == 0 {
		out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
			Code: "empty_points", Message: "blank points array is empty"})
		return
	}
	for i, r := range rows {
		bp := domain.BlankPoint{Row: i + 1}
		if s, ok := atStr(r, "time", "t"); ok {
			bp.Time = parseValue(s, "time", func(msg string) {
				out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
					Row: i + 1, Field: "time", Code: "bad_value", Message: msg, Raw: s})
			})
		}
		if s, ok := atStr(r, "mass", "m"); ok {
			bp.Mass = parseValue(s, "mass", func(msg string) {
				out.Diags = append(out.Diags, domain.Diag{Severity: "error", Layer: string(out.Layer),
					Row: i + 1, Field: "mass", Code: "bad_value", Message: msg, Raw: s})
			})
		}
		if s, ok := atStr(r, "temp", "temperature"); ok {
			bp.Temp = parseValue(s, "temp", nil)
		}
		out.Blank = append(out.Blank, bp)
	}
}
