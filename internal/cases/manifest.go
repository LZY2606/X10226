package cases

import (
	"bytes"
	"encoding/json"
)

// Step is one scripted action of the review narrative.
type Step struct {
	N            int            `json:"n"`
	Action       string         `json:"action"` // import | revise_params | decide | resolve | undo | pause
	BundleID     string         `json:"bundle_id,omitempty"`
	ParamsPatch  map[string]any `json:"params_patch,omitempty"`
	Target       string         `json:"target,omitempty"`
	Verdict      string         `json:"verdict,omitempty"`
	By           string         `json:"by,omitempty"`
	Rationale    string         `json:"rationale,omitempty"`
	WinnerTarget string         `json:"winner,omitempty"`
	Expect       string         `json:"expect"`
	Note         string         `json:"note,omitempty"`
}

// Manifest describes the offline case pack.
type Manifest struct {
	Title      string     `json:"title"`
	Domain     string     `json:"domain"`
	Seed       int64      `json:"seed"`
	InstallCmd string     `json:"install_cmd"`
	DemoCmd    string     `json:"demo_cmd"`
	URL        string     `json:"url"`
	BaselineID string     `json:"baseline_id"`
	Scenarios  []Scenario `json:"scenarios"`
	Narrative  []Step     `json:"narrative"`
	Judgements []string   `json:"machine_readable_judgements"`
}

// BuildManifest assembles the committed case-pack manifest.
func BuildManifest() *Manifest {
	m := &Manifest{
		Title:      "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Domain:     "thermogravimetric-analysis",
		Seed:       Seed,
		InstallCmd: "go build ./...",
		DemoCmd:    "go test ./... -count=1; go run ./cmd/server --addr 127.0.0.1:5344",
		URL:        "http://127.0.0.1:5344",
		BaselineID: "baseline-v1",
		Scenarios:  Scenarios(),
		Judgements: []string{
			"atmosphere_delay: valve switch time != sample reaction time",
			"overlapping_stages: stages may overlap; mass losses are not forced to sum to 100%",
			"no_interpolation_across_gap: data gaps are never bridged",
			"ambiguous_decomposition: all legal candidates are retained when not uniquely decidable",
			"local_invalidation: only dependants of the changed source expire",
			"immutable_inputs: raw batches are append-only versions with parent links",
		},
		Narrative: []Step{
			{N: 1, Action: "import", BundleID: "baseline-v1",
				Expect: "six stratified layers commit; corrected TG/DTG, two peak hypotheses, stages, reaction windows and residue candidates appear",
				Note:   "reviewer verifies raw samples, drift correction, furnace program, gas valve events, blank curve and declared initial mass"},
			{N: 2, Action: "decide", Target: "stage-h0-1", Verdict: "accepted",
				By: "reviewer-li", Rationale: "moisture loss matches onset at ~200 s",
				Expect: "decision event is linked to the stage candidate"},
			{N: 3, Action: "revise_params",
				ParamsPatch: map[string]any{"atmosphere_delay_s": 55, "lag_tau_s": 16},
				Expect:      "new parameter fingerprint; only reaction windows and dependant nodes get new identities; unchanged peaks keep theirs"},
			{N: 4, Action: "import", BundleID: "ce-initial-mass",
				Expect: "initial_mass becomes v2 with parent v1; only mass-derived nodes expire",
				Note:   "single local source change after the parameter revision"},
			{N: 5, Action: "decide", Target: "residue-h0", Verdict: "accepted",
				By: "reviewer-li", Rationale: "tail plateau stable",
				Expect: "accept residue for h0"},
			{N: 6, Action: "decide", Target: "residue-h0", Verdict: "rejected",
				By: "reviewer-wang", Rationale: "tail window too short after initial mass revision",
				Expect: "a conflict between two reviewers is recorded; neither overrides until resolved"},
			{N: 7, Action: "resolve", Target: "residue-h0",
				WinnerTarget: "rejected",
				Expect:       "conflict resolution event names the winning decision and reason; the loser is marked overridden"},
			{N: 8, Action: "undo", Target: "stage-h0-1",
				Expect: "the step-2 acceptance is logically undone but the original event stays in history"},
			{N: 9, Action: "pause",
				Expect: "time travel to any old event restores that exact dependency set and visible conclusions"},
		},
	}
	return m
}

// MarshalManifest renders the manifest deterministically.
func MarshalManifest(m *Manifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
