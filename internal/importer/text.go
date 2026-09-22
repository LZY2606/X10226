package importer

import (
	"fmt"
	"strings"

	"tga-review/internal/domain"
)

type section struct {
	layer string
	lines []string
}

// parseText reads the line-oriented bundle:
//
//	# @layer samples
//	t[s] mass[mg] temp[degC]
//	0 12.50 30
//
// Each section is built independently. A structurally invalid layer is
// rejected while sibling layers still land (half failure).
func parseText(project, source string, payload []byte) (*Result, error) {
	res := &Result{Project: project, SourceRef: source}
	var sections []section
	var cur *section
	for rawIdx, rawLine := range strings.Split(string(payload), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "##") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			tokens := strings.Fields(line)
			if len(tokens) >= 3 && tokens[0] == "#" && tokens[1] == "@layer" {
				if !knownLayer(tokens[2]) {
					return nil, fmt.Errorf("line %d: unknown layer %q", rawIdx+1, tokens[2])
				}
				sections = append(sections, section{layer: tokens[2]})
				cur = &sections[len(sections)-1]
				continue
			}
			continue
		}
		if cur == nil {
			return nil, fmt.Errorf("line %d: data before any # @layer section: %q", rawIdx+1, line)
		}
		cur.lines = append(cur.lines, line)
	}
	seen := map[string]bool{}
	for _, sec := range sections {
		if seen[sec.layer] {
			rejectLayer(res, sec.layer, "duplicate layer section ignored; first valid section kept")
			continue
		}
		seen[sec.layer] = true
		if err := buildSection(res, project, source, sec.layer, sec.lines); err != nil {
			rejectLayer(res, sec.layer, err.Error())
			clearLayer(res, sec.layer)
		}
	}
	finalizeAccepted(res)
	return res, nil
}

func knownLayer(l string) bool {
	for _, x := range domain.Layers {
		if x == l {
			return true
		}
	}
	return false
}

func rejectLayer(res *Result, layer, reason string) {
	for i := range res.Rejected {
		if res.Rejected[i].Layer == layer {
			return
		}
	}
	res.Rejected = append(res.Rejected, domain.RejectedLayer{Layer: layer, Reason: reason})
}

func clearLayer(res *Result, layer string) {
	switch layer {
	case domain.LayerSamples:
		res.Data.Samples = nil
	case domain.LayerDrift:
		res.Data.Drift = nil
	case domain.LayerProgram:
		res.Data.Program = nil
	case domain.LayerGasEvents:
		res.Data.Gas = nil
	case domain.LayerBlank:
		res.Data.Blank = nil
	case domain.LayerInitial:
		res.Data.Initial = nil
	}
}

func finalizeAccepted(res *Result) {
	d := res.Data
	if len(d.Samples) > 0 {
		res.AcceptedLayers = append(res.AcceptedLayers, domain.LayerSamples)
	}
	if len(d.Drift) > 0 {
		res.AcceptedLayers = append(res.AcceptedLayers, domain.LayerDrift)
	}
	if len(d.Program) > 0 {
		res.AcceptedLayers = append(res.AcceptedLayers, domain.LayerProgram)
	}
	if len(d.Gas) > 0 {
		res.AcceptedLayers = append(res.AcceptedLayers, domain.LayerGasEvents)
	}
	if len(d.Blank) > 0 {
		res.AcceptedLayers = append(res.AcceptedLayers, domain.LayerBlank)
	}
	if len(d.Initial) > 0 {
		res.AcceptedLayers = append(res.AcceptedLayers, domain.LayerInitial)
	}
}
