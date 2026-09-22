package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"

	"tgareview/internal/domain"
)

// osReadBaselineSamples reads the just-generated baseline sample_points rows so
// single-source mutations can copy them verbatim.
func osReadBaselineSamples(outDir string) ([]map[string]any, error) {
	var b struct {
		Layers []struct {
			Kind string          `json:"kind"`
			Data json.RawMessage `json:"data"`
		} `json:"layers"`
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "tga_case_v1", "baseline.json"))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, err
	}
	for _, l := range b.Layers {
		if domain.LayerKind(l.Kind) == domain.SamplePoints {
			var rows []map[string]any
			if err := json.Unmarshal(l.Data, &rows); err != nil {
				return nil, err
			}
			return rows, nil
		}
	}
	return nil, os.ErrNotExist
}

func asInt(v any) int {
	switch t := v.(type) {
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return -1
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case json.Number:
		f, _ := t.Float64()
		return f
	case float64:
		return t
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	}
	return 0
}
