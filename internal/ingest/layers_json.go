package ingest

import (
	"encoding/json"
	"fmt"

	"tga-review/internal/domain"
)

// JSON layer payloads mirror the CSV column names, e.g.
// {"points":[{"t_s":0,"mass_mg":"12.3 mg","temp_c":"30 °C"}]}.
// Values may be numbers or strings; strings preserve their original tokens.

func toRows(raw any) ([]map[string]string, error) {
	arr, ok := raw.([]any)
	if !ok {
		// accept {"points":[...]} / {"events":[...]} / {"segments":[...]}
		obj, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("json must be array or object")
		}
		for _, key := range []string{"points", "events", "segments"} {
			if v, ok := obj[key].([]any); ok {
				arr = v
				break
			}
		}
		if arr == nil {
			if _, ok := obj["mass_mg"]; ok {
				arr = []any{obj}
			} else {
				return nil, fmt.Errorf("json object missing points/events/segments")
			}
		}
	}
	rows := make([]map[string]string, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rows must be objects")
		}
		out := map[string]string{}
		for k, v := range m {
			switch t := v.(type) {
			case nil:
				out[k] = ""
			case string:
				out[k] = t
			case json.Number:
				out[k] = t.String()
			case bool:
				out[k] = fmt.Sprintf("%v", t)
			default:
				b, _ := json.Marshal(t)
				out[k] = string(b)
			}
		}
		rows = append(rows, out)
	}
	return rows, nil
}

func rowsToCSVRecords(header []string, rows []map[string]string) [][]string {
	recs := make([][]string, len(rows))
	for i, m := range rows {
		rec := make([]string, len(header))
		for j, h := range header {
			rec[j] = m[h]
		}
		recs[i] = rec
	}
	return recs
}

func jsonTable(layer domain.SourceLayer, raw any, header []string, parser func(domain.SourceLayer, []string, [][]string) Result) Result {
	rows, err := toRows(raw)
	if err != nil {
		return fatal(layer, err.Error())
	}
	return parser(layer, header, rowsToCSVRecords(header, rows))
}

func parseSamplesJSON(layer domain.SourceLayer, raw any) Result {
	return jsonTable(layer, raw, []string{"t_s", "mass_mg", "temp_c"}, parseSamplesRows)
}

func parseDriftJSON(layer domain.SourceLayer, raw any) Result {
	return jsonTable(layer, raw, []string{"t_s", "mass_mg"}, parseDriftRows)
}

func parseFurnaceJSON(layer domain.SourceLayer, raw any) Result {
	return jsonTable(layer, raw, []string{"from_c", "to_c", "rate_k_per_min", "duration_s", "gas", "kind"}, parseFurnaceRows)
}

func parseValvesJSON(layer domain.SourceLayer, raw any) Result {
	return jsonTable(layer, raw, []string{"t_s", "gas"}, parseValvesRows)
}

func parseBlankJSON(layer domain.SourceLayer, raw any) Result {
	return jsonTable(layer, raw, []string{"t_s", "mass_mg"}, parseBlankRows)
}

func parseInitMassJSON(layer domain.SourceLayer, raw any) Result {
	return jsonTable(layer, raw, []string{"mass_mg", "note"}, parseInitMassRows)
}
