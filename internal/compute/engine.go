package compute

import (
	"fmt"
	"sort"
	"strings"

	"tga-review/internal/domain"
)

// Bundle is the set of accepted, versioned source layers fed to the engine.
// Each layer carries the stable identity it was stored with.
type Bundle struct {
	Sources     map[string]string // layer -> source ID
	Samples     []domain.SamplePoint
	Drift       *domain.DriftModel
	Furnace     []domain.FurnaceSegment
	Gas         []domain.GasEvent
	Blank       []domain.BlankPoint
	InitialMass *domain.InitialMassStatement
}

// DirectSources returns source IDs in canonical layer order.
func (b Bundle) DirectSources() []string {
	var out []string
	for _, l := range domain.AllLayers() {
		if id, ok := b.Sources[l]; ok && id != "" {
			out = append(out, id)
		}
	}
	return out
}

// Derive runs the whole deterministic pipeline.
func Derive(p Params, b Bundle) *Result {
	r := &Result{
		AlgorithmVersion: AlgorithmVersion,
		ParamFingerprint: p.Fingerprint(),
		Params:           p,
		InputHash:        bundleHash(b),
		Provenance: Provenance{
			AlgorithmVersion: AlgorithmVersion,
			ParamFingerprint: p.Fingerprint(),
			DirectSources:    b.DirectSources(),
			InputHash:        bundleHash(b),
		},
		Diagnostics: []Diagnostic{},
	}

	if b.InitialMass != nil && b.InitialMass.Mass.Normalised && b.InitialMass.Mass.Canonical > 0 {
		r.InitialMassMG = b.InitialMass.Mass.Canonical
		r.InitialMassKnown = true
	} else {
		r.Diagnostics = append(r.Diagnostics, Diagnostic{
			Scope: "initial_mass", Severity: "warning",
			Message: "no valid stated initial mass; percentages relative to first accepted point",
		})
	}

	// 1. Atmosphere reaction windows: valve time != reaction time.
	r.Atmospheres = mapAtmospheres(p, b)

	// 2. Normalise accepted sample points, mark non-monotonic noise, split gaps.
	raw := acceptedPoints(b)
	segments, gaps := splitSegments(raw, p)
	r.Gaps = gaps

	// 3. Blank + drift correction inside each segment; never across gaps.
	for si := range segments {
		seg := &segments[si]
		for i := range seg.Points {
			pt := &seg.Points[i]
			pt.MassCorrect = correctedMass(pt.T, pt.MassRaw, p, b)
		}
		markNonMonotonic(seg)
		assignTG(seg, r)
		assignDTG(seg, r)
	}
	r.Segments = segments

	// 4. DTG peaks with overlap grouping and rejected candidates retained.
	r.Peaks = detectPeaks(p, r)

	// 5. Stage candidates (may overlap; no 100% total enforced).
	r.Stages = buildStages(p, b, r)

	// 6. Residue with expandable chain per segment end.
	r.Residues = buildResidues(p, r)

	// Diagnostics for retained raw values that could not be normalised.
	collectInputDiagnostics(b, r)

	sort.SliceStable(r.Diagnostics, func(i, j int) bool {
		return r.Diagnostics[i].Scope < r.Diagnostics[j].Scope
	})
	return r
}

type rawPoint struct {
	T, Tk, Mass float64
}

func acceptedPoints(b Bundle) []rawPoint {
	var out []rawPoint
	for _, s := range b.Samples {
		if s.Time.Normalised && s.Temperature.Normalised && s.Mass.Normalised {
			out = append(out, rawPoint{s.Time.Canonical, s.Temperature.Canonical, s.Mass.Canonical})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].T < out[j].T })
	return out
}

func bundleHash(b Bundle) string {
	var sb strings.Builder
	for _, l := range domain.AllLayers() {
		fmt.Fprintf(&sb, "%s=%s;", l, b.Sources[l])
	}
	return "in" + shortHash(sb.String())[:14]
}
