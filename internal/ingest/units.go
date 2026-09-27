package ingest

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// Canonical bases used only in the derived layer:
//
//	time        -> minutes ("min")
//	temperature -> kelvin ("K"), with Celsius retained in basis note
//	mass        -> milligrams ("mg")
//
// Unknown units are NOT dropped: the Quantity keeps its raw text and gains a
// diagnostic, so plotting can never silently discard the measurement.
var unitFactors = map[string]map[string]float64{
	"time": {
		"min": 1, "minute": 1, "minutes": 1,
		"s": 1.0 / 60.0, "sec": 1.0 / 60.0, "seconds": 1.0 / 60.0,
		"h": 60, "hr": 60, "hour": 60, "hours": 60,
	},
	"mass": {
		"mg": 1, "milligram": 1, "milligrams": 1,
		"g": 1000, "gram": 1000, "grams": 1000,
		"ug": 0.001, "µg": 0.001,
	},
}

// temperature units need affine conversion, so handle separately.
func isCelsius(u string) bool {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "c", "°c", "degc", "celsius":
		return true
	}
	return false
}
func isKelvin(u string) bool {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "k", "kelvin":
		return true
	}
	return false
}

func normalise(q *domain.Quantity, kind string, canonicalUnit string) string {
	q.Unit = strings.TrimSpace(q.Unit)
	q.Raw = strings.TrimSpace(q.Raw)
	v, err := strconv.ParseFloat(q.Raw, 64)
	if err != nil {
		q.Diagnostic = fmt.Sprintf("unparsable numeric literal %q in unit %q", q.Raw, q.Unit)
		return q.Diagnostic
	}
	if kind == "temperature" {
		switch {
		case isKelvin(q.Unit):
			q.Canonical = v
			q.Normalised = true
		case isCelsius(q.Unit):
			q.Canonical = v + 273.15
			q.Normalised = true
			q.Basis = joinBasis(q.Basis, "origin:C")
		default:
			q.Diagnostic = fmt.Sprintf("unknown temperature unit %q", q.Unit)
		}
		if q.Normalised {
			q.Unit = canonicalUnit
		}
		return q.Diagnostic
	}
	factors := unitFactors[kind]
	if f, ok := factors[strings.ToLower(q.Unit)]; ok {
		q.Canonical = v * f
		q.Normalised = true
		q.Unit = canonicalUnit
		return ""
	}
	q.Diagnostic = fmt.Sprintf("unknown %s unit %q", kind, q.Unit)
	return q.Diagnostic
}

func joinBasis(a, b string) string {
	if a == "" {
		return b
	}
	return a + ";" + b
}

func normaliseSamples(pts []domain.SamplePoint) (int, []string) {
	var diags []string
	for i := range pts {
		pts[i].Index = i
		if d := normalise(&pts[i].Time, "time", "min"); d != "" {
			diags = append(diags, fmt.Sprintf("samples[%d].time: %s", i, d))
		}
		if d := normalise(&pts[i].Temperature, "temperature", "K"); d != "" {
			diags = append(diags, fmt.Sprintf("samples[%d].temperature: %s", i, d))
		}
		if d := normalise(&pts[i].Mass, "mass", "mg"); d != "" {
			diags = append(diags, fmt.Sprintf("samples[%d].mass: %s", i, d))
		}
	}
	// structural checks (monotonic time). bad points are diagnosed, not removed.
	for i := 1; i < len(pts); i++ {
		a, b := pts[i-1], pts[i]
		if a.Time.Normalised && b.Time.Normalised && b.Time.Canonical < a.Time.Canonical-math.Pow10(-9) {
			diags = append(diags, fmt.Sprintf("samples[%d]: non-monotonic time %v -> %v", i, a.Time.Canonical, b.Time.Canonical))
		}
	}
	return len(pts), diags
}

func normaliseDrift(d *domain.DriftModel) []string {
	var diags []string
	switch d.Kind {
	case "linear":
		if d.SlopePerMin.Raw != "" {
			// slope expressed in mass units per minute per stated mass unit
			if x := normalise(&d.SlopePerMin, "mass", "mg"); x != "" {
				diags = append(diags, "drift.slope_per_min: "+x)
			}
		}
		if d.Intercept.Raw != "" {
			if x := normalise(&d.Intercept, "mass", "mg"); x != "" {
				diags = append(diags, "drift.intercept: "+x)
			}
		}
		if d.AnchorTime.Raw != "" {
			if x := normalise(&d.AnchorTime, "time", "min"); x != "" {
				diags = append(diags, "drift.anchor_time: "+x)
			}
		}
	case "points", "none", "":
	default:
		diags = append(diags, fmt.Sprintf("drift: unknown kind %q", d.Kind))
	}
	for i := range d.Points {
		if x := normalise(&d.Points[i].Time, "time", "min"); x != "" {
			diags = append(diags, fmt.Sprintf("drift.points[%d].time: %s", i, x))
		}
		if x := normalise(&d.Points[i].Mass, "mass", "mg"); x != "" {
			diags = append(diags, fmt.Sprintf("drift.points[%d].mass: %s", i, x))
		}
	}
	return diags
}

func normaliseFurnace(segs []domain.FurnaceSegment) (int, []string) {
	var diags []string
	for i := range segs {
		segs[i].Index = i
		switch segs[i].Kind {
		case "ramp", "isothermal":
		default:
			diags = append(diags, fmt.Sprintf("furnace[%d]: unknown kind %q", i, segs[i].Kind))
		}
		for _, q := range []struct {
			q    *domain.Quantity
			kind string
			unit string
			what string
		}{
			{&segs[i].StartTime, "time", "min", "start_time"},
			{&segs[i].EndTime, "time", "min", "end_time"},
			{&segs[i].T0, "temperature", "K", "t0"},
			{&segs[i].T1, "temperature", "K", "t1"},
		} {
			if d := normalise(q.q, q.kind, q.unit); d != "" {
				diags = append(diags, fmt.Sprintf("furnace[%d].%s: %s", i, q.what, d))
			}
		}
	}
	return len(segs), diags
}

func normaliseGas(evs []domain.GasEvent) (int, []string) {
	var diags []string
	for i := range evs {
		evs[i].Index = i
		if d := normalise(&evs[i].Time, "time", "min"); d != "" {
			diags = append(diags, fmt.Sprintf("gas[%d].time: %s", i, d))
		}
		if strings.TrimSpace(evs[i].To) == "" {
			diags = append(diags, fmt.Sprintf("gas[%d]: missing target gas", i))
		}
		if evs[i].Flow.Raw != "" {
			// flow unit declared generically; only annotate, never fabricate
			evs[i].Flow.Basis = joinBasis(evs[i].Flow.Basis, "as-imported")
		}
	}
	return len(evs), diags
}

func normaliseBlank(pts []domain.BlankPoint) (int, []string) {
	var diags []string
	for i := range pts {
		pts[i].Index = i
		if d := normalise(&pts[i].Time, "time", "min"); d != "" {
			diags = append(diags, fmt.Sprintf("blank[%d].time: %s", i, d))
		}
		if d := normalise(&pts[i].Temperature, "temperature", "K"); d != "" {
			diags = append(diags, fmt.Sprintf("blank[%d].temperature: %s", i, d))
		}
		if d := normalise(&pts[i].Mass, "mass", "mg"); d != "" {
			diags = append(diags, fmt.Sprintf("blank[%d].mass: %s", i, d))
		}
	}
	return len(pts), diags
}

func normaliseInitial(m *domain.InitialMassStatement) []string {
	var diags []string
	if d := normalise(&m.Mass, "mass", "mg"); d != "" {
		diags = append(diags, "initial_mass.mass: "+d)
	}
	if m.Mass.Canonical <= 0 {
		diags = append(diags, "initial_mass.mass: must be positive")
	}
	return diags
}
