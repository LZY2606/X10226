// Package cases embeds the offline domain case pack
// "失重谱系：TGA 阶段分解、气氛切换与残留物证据链". Nothing here is a generic
// spreadsheet: every fixture is authored as TGA layer material with units and
// coordinate bases, plus a machine-readable replay of boundary situations.
package cases

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed data/*
var dataFS embed.FS

// Manifest describes one case pack.
type Manifest struct {
	ID           string                  `json:"id"`
	Title        string                  `json:"title"`
	Seed         int64                   `json:"seed"`
	VirtualStart string                  `json:"virtualStart"`
	Files        []string                `json:"files"`
	Replay       []ReplayStep            `json:"replay"`
	Expectations map[string]Expectation  `json:"expectations"`
}

// ReplayStep is one machine-readable review action in the narrative.
type ReplayStep struct {
	N        int    `json:"n"`
	Kind     string `json:"kind"`
	Layer    string `json:"layer,omitempty"`
	File     string `json:"file,omitempty"`
	Label    string `json:"label,omitempty"`
	Decision string `json:"decision,omitempty"`
	Target   string `json:"target,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Expect   string `json:"expect,omitempty"`
}

// Expectation states the bounded blast radius / outcome of a step.
type Expectation struct {
	AffectsOnly []string `json:"affectsOnly,omitempty"`
	Status      string   `json:"status,omitempty"`
	Note        string   `json:"note,omitempty"`
}

type Pack struct {
	Manifest Manifest
	V1Input  string
	Files    map[string]string
}

func List() []Manifest {
	entries, err := dataFS.ReadDir("data")
	if err != nil {
		return nil
	}
	var out []Manifest
	for _, e := range entries {
		if e.IsDir() {
			if c, err := loadManifest(e.Name()); err == nil {
				out = append(out, c)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func Load(id string) (Pack, error) {
	m, err := loadManifest(id)
	if err != nil {
		return Pack{}, err
	}
	p := Pack{Manifest: m, Files: map[string]string{}}
	for _, f := range m.Files {
		b, err := dataFS.ReadFile("data/" + id + "/" + f)
		if err != nil {
			return Pack{}, fmt.Errorf("missing case file %s: %w", f, err)
		}
		p.Files[f] = string(b)
	}
	if v1, ok := p.Files["v1_bundle.txt"]; ok {
		p.V1Input = v1
	}
	return p, nil
}

func loadManifest(id string) (Manifest, error) {
	b, err := dataFS.ReadFile("data/" + id + "/manifest.json")
	if err != nil {
		return Manifest{}, fmt.Errorf("case %q: %w", id, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}
