package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"sort"
	"strings"

	"tga/internal/app"
	"tga/internal/model"
)

type caseEntry struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

var errBadScenario = errors.New("unknown scenario file")

func (s *Server) listCasepack(w http.ResponseWriter, r *http.Request) {
	var entries []caseEntry
	err := fs.WalkDir(s.CasePack, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, _ := d.Info()
		entries = append(entries, caseEntry{Name: path, Size: info.Size()})
		return nil
	})
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	replay, _ := s.CasePack.ReadFile("replay.txt")
	writeJSON(w, 200, map[string]any{"files": entries, "replay": string(replay)})
}

func (s *Server) getCasepack(w http.ResponseWriter, r *http.Request) {
	data, err := s.CasePack.ReadFile(r.PathValue("name"))
	if err != nil {
		writeErr(w, 404, "case file not found")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(data)
}

func (s *Server) loadCasepack(w http.ResponseWriter, r *http.Request) {
	id, which := r.PathValue("id"), r.PathValue("which")
	switch which {
	case "baseline":
		if err := s.applyBaseline(r.Context(), id); err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"loaded": "baseline"})
	case "scenario":
		var req struct {
			File string `json:"file"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.File == "" {
			writeErr(w, 400, `body must be {"file":"scenarios/01_drift_blank.txt"}`)
			return
		}
		name, err := s.applyScenario(r.Context(), id, req.File)
		if err != nil {
			writeErr(w, 400, err.Error())
			return
		}
		writeJSON(w, 201, map[string]any{"loaded": name})
	default:
		writeErr(w, 400, "which must be baseline or scenario")
	}
}

func (s *Server) applyBaseline(ctx context.Context, id string) error {
	files := map[model.Layer]string{
		model.LayerSamples: "casepack/baseline/mass_samples.txt",
		model.LayerBlank:   "casepack/baseline/blank_curve.txt",
		model.LayerProgram: "casepack/baseline/furnace_program.txt",
		model.LayerGas:     "casepack/baseline/gas_valve_events.txt",
		model.LayerM0:      "casepack/baseline/initial_mass.txt",
	}
	for _, layer := range model.AllLayers() {
		data, err := s.CasePack.ReadFile(files[layer])
		if err != nil {
			return err
		}
		if _, _, err := s.App.ImportLayer(ctx, id, app.ImportInput{
			Layer: layer, Raw: string(data), Source: "casepack:" + files[layer],
		}); err != nil {
			return err
		}
	}
	params, err := s.CasePack.ReadFile("casepack/baseline/params.txt")
	if err != nil {
		return err
	}
	vals, err := app.ParseParamsText(string(params))
	if err != nil {
		return err
	}
	_, err = s.App.ReviseParams(ctx, id, vals)
	return err
}

func (s *Server) applyScenario(ctx context.Context, id, file string) (string, error) {
	if !strings.HasPrefix(file, "scenarios/") || !strings.HasSuffix(file, ".txt") {
		return "", errBadScenario
	}
	data, err := s.CasePack.ReadFile("casepack/" + file)
	if err != nil {
		return "", err
	}
	name := strings.TrimSuffix(strings.TrimPrefix(file, "scenarios/"), ".txt")
	switch {
	case strings.Contains(name, "drift_blank"):
		_, _, err = s.App.ImportLayer(ctx, id, app.ImportInput{Layer: model.LayerBlank, Raw: string(data), Source: "casepack:" + file})
	case strings.Contains(name, "overlap"), strings.Contains(name, "nonmonotone"),
		strings.Contains(name, "gap_samples"), strings.Contains(name, "partial_samples"):
		_, _, err = s.App.ImportLayer(ctx, id, app.ImportInput{Layer: model.LayerSamples, Raw: string(data), Source: "casepack:" + file})
	case strings.Contains(name, "revised_mass"):
		_, _, err = s.App.ImportLayer(ctx, id, app.ImportInput{Layer: model.LayerM0, Raw: string(data), Source: "casepack:" + file})
	case strings.Contains(name, "delayed_params"):
		var vals map[string]string
		vals, err = app.ParseParamsText(string(data))
		if err == nil {
			_, err = s.App.ReviseParams(ctx, id, vals)
		}
	default:
		err = errBadScenario
	}
	return name, err
}
