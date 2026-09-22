package derive

import (
	"encoding/json"
	"math"
	"sort"

	"tga/internal/domain"
)

// AlgorithmVersion is bumped whenever the calculation semantics change.
const AlgorithmVersion = "tga-derive-1.0.0"

// Compute runs the full layered derivation from the pinned direct sources and
// parameters. It is pure: identical inputs always yield an identical node.
func Compute(projectID, paramID string, p domain.Params, in *Inputs, sourceDigests map[domain.SourceKind]string) domain.DerivedNode {
	series, rules := buildSeries(p, in)
	atmos := mapAtmosphere(p, in)
	rules = append(rules, atmosRules(p, in, atmos)...)
	peaks, peakRules := findPeaks(p, series)
	rules = append(rules, peakRules...)
	phases, resRules := decomposePhases(p, series, peaks, atmos)
	rules = append(rules, resRules...)
	residues, resRule := residueRates(p, in, series, phases)
	rules = append(rules, resRule...)

	src := map[string]int{}
	var digests []string
	kinds := []domain.SourceKind{domain.KindSamples, domain.KindDrift, domain.KindProgram, domain.KindGasEvent, domain.KindBlank, domain.KindInitialMass}
	for _, k := range kinds {
		if v, ok := in.Versions[k]; ok {
			src[string(k)] = v
			if d, ok := sourceDigests[k]; ok {
				digests = append(digests, string(k)+":"+d)
			}
		}
	}
	sort.Strings(digests)
	id := domain.DerivedID(projectID, AlgorithmVersion, paramID, src, digests)
	node := domain.DerivedNode{
		ID:               id,
		ProjectID:        projectID,
		AlgorithmVersion: AlgorithmVersion,
		ParamVersion:     paramID,
		DirectSources:    src,
		Series:           series,
		Atmos:            atmos,
		Peaks:            peaks,
		Phases:           phases,
		Residues:         residues,
		RuleEvents:       rules,
	}
	node.Digest = domain.DigestOf(stripID(node))
	return node
}

// stripID removes identity fields so the digest covers content only.
func stripID(n domain.DerivedNode) any {
	n.ID = ""
	n.ProjectID = ""
	n.Stale = false
	n.StaleReason = ""
	n.SupersededBy = ""
	return n
}

// interpolate evaluates a piecewise linear calibration at t, returning false
// if t falls outside defined segments. It is never used to bridge a sampling
// gap in the sample series itself.
func pieceLinear(times, vals []float64, t float64) (float64, bool) {
	if len(times) == 0 {
		return 0, false
	}
	if t <= times[0] {
		return vals[0], true
	}
	last := len(times) - 1
	if t >= times[last] {
		return vals[last], true
	}
	i := sort.Search(len(times), func(i int) bool { return times[i] >= t })
	if i == 0 {
		return vals[0], true
	}
	t0, t1 := times[i-1], times[i]
	if t1 == t0 {
		return vals[i-1], true
	}
	f := (t - t0) / (t1 - t0)
	return vals[i-1] + f*(vals[i]-vals[i-1]), true
}

func round(v float64, n int) float64 {
	p := math.Pow(10, float64(n))
	return math.Round(v*p) / p
}

func jsonNum(v float64) any {
	b, _ := json.Marshal(round(v, 6))
	return json.RawMessage(b)
}
