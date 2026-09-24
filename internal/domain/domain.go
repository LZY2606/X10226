// Package domain implements the pure computations of the TGA review system:
// unit-aware parsing of raw quantities, segmented series with sampling gaps,
// blank/balance-drift correction, DTG, atmosphere-delay timelines, stage
// decomposition and residue. All functions are pure; persistence and
// orchestration live in the store and app packages.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Diagnostic is attached to a raw value that could not be converted. The raw
// value stays in place in the source payload; it is never silently dropped.
type Diagnostic struct {
	Where  string `json:"where"`
	Raw    string `json:"raw"`
	Detail string `json:"detail"`
}

// Judgment is a machine-readable record of a domain rule applied to data.
type Judgment struct {
	Rule      string         `json:"rule"`
	Statement string         `json:"statement"`
	Data      map[string]any `json:"data,omitempty"`
}

// ---------- units ----------

type Dim int

const (
	DimTime Dim = iota // base: s
	DimTemp            // base: °C
	DimMass            // base: mg
	DimFlow            // base: mL/min
	DimRate            // base: °C/min
)

func (d Dim) String() string {
	switch d {
	case DimTime:
		return "time(s)"
	case DimTemp:
		return "temp(°C)"
	case DimMass:
		return "mass(mg)"
	case DimFlow:
		return "flow(mL/min)"
	case DimRate:
		return "rate(°C/min)"
	}
	return "?"
}

type unitDef struct {
	dim    Dim
	factor float64
	offset float64
}

var unitTable = map[string]unitDef{
	"s": {DimTime, 1, 0}, "sec": {DimTime, 1, 0}, "min": {DimTime, 60, 0}, "h": {DimTime, 3600, 0},
	"c": {DimTemp, 1, 0}, "°c": {DimTemp, 1, 0}, "degc": {DimTemp, 1, 0}, "k": {DimTemp, 1, -273.15},
	"mg": {DimMass, 1, 0}, "g": {DimMass, 1000, 0}, "ug": {DimMass, 0.001, 0}, "µg": {DimMass, 0.001, 0},
	"ml/min": {DimFlow, 1, 0}, "ml/min.": {DimFlow, 1, 0}, "sccm": {DimFlow, 1, 0},
	"c/min": {DimRate, 1, 0}, "k/min": {DimRate, 1, 0}, "c/s": {DimRate, 60, 0},
}

// ParseQuantity converts a raw representation like "12.483 mg" into base
// units. The raw string is preserved by callers; only the derived layer
// normalizes.
func ParseQuantity(raw string, want Dim) (float64, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return 0, fmt.Errorf("empty quantity")
	}
	fields := strings.Fields(s)
	numPart := fields[0]
	unitPart := strings.ToLower(strings.Join(fields[1:], ""))
	v, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("unparseable number %q", numPart)
	}
	if unitPart == "" {
		return v, nil // unitless: assumed already in base units
	}
	u, ok := unitTable[unitPart]
	if !ok {
		return 0, fmt.Errorf("unknown unit %q", unitPart)
	}
	if u.dim != want {
		return 0, fmt.Errorf("unit %q is %v, want %v", unitPart, u.dim, want)
	}
	return v*u.factor + u.offset, nil
}

// ---------- series with gaps ----------

// RawPoint is one sample point in its original representation.
type RawPoint struct {
	Seq  int    `json:"seq"`
	Time string `json:"time"`
	Temp string `json:"temp"`
	Mass string `json:"mass"`
}

type Point struct {
	Seq    int     `json:"seq"`
	TimeS  float64 `json:"time_s"`
	TempC  float64 `json:"temp_c"`
	MassMg float64 `json:"mass_mg"`
	Seg    int     `json:"seg"`
}

// Gap marks a sampling gap. Data on either side is never interpolated across.
type Gap struct {
	AfterSeq int     `json:"after_seq"`
	FromS    float64 `json:"from_s"`
	ToS      float64 `json:"to_s"`
}

type Series struct {
	Points     []Point      `json:"points"`
	Gaps       []Gap        `json:"gaps"`
	Diags      []Diagnostic `json:"diagnostics"`
	GapFactor  float64      `json:"gap_factor"`
	MedianDtS  float64      `json:"median_dt_s"`
}

// BuildSeries normalizes raw points. Unconvertible values produce
// diagnostics and are skipped here, but remain in the stored raw payload.
func BuildSeries(raw []RawPoint, gapFactor float64) Series {
	s := Series{GapFactor: gapFactor}
	for _, rp := range raw {
		t, err := ParseQuantity(rp.Time, DimTime)
		if err != nil {
			s.Diags = append(s.Diags, Diagnostic{Where: fmt.Sprintf("point[%d].time", rp.Seq), Raw: rp.Time, Detail: err.Error()})
			continue
		}
		tc, err := ParseQuantity(rp.Temp, DimTemp)
		if err != nil {
			s.Diags = append(s.Diags, Diagnostic{Where: fmt.Sprintf("point[%d].temp", rp.Seq), Raw: rp.Temp, Detail: err.Error()})
			continue
		}
		m, err := ParseQuantity(rp.Mass, DimMass)
		if err != nil {
			s.Diags = append(s.Diags, Diagnostic{Where: fmt.Sprintf("point[%d].mass", rp.Seq), Raw: rp.Mass, Detail: err.Error()})
			continue
		}
		s.Points = append(s.Points, Point{Seq: rp.Seq, TimeS: t, TempC: tc, MassMg: m})
	}
	sort.Slice(s.Points, func(i, j int) bool { return s.Points[i].TimeS < s.Points[j].TimeS })
	if len(s.Points) < 2 {
		return s
	}
	dts := make([]float64, 0, len(s.Points)-1)
	for i := 1; i < len(s.Points); i++ {
		dts = append(dts, s.Points[i].TimeS-s.Points[i-1].TimeS)
	}
	sort.Float64s(dts)
	s.MedianDtS = dts[len(dts)/2]
	threshold := s.MedianDtS * gapFactor
	seg := 0
	s.Points[0].Seg = 0
	for i := 1; i < len(s.Points); i++ {
		dt := s.Points[i].TimeS - s.Points[i-1].TimeS
		if dt > threshold {
			s.Gaps = append(s.Gaps, Gap{AfterSeq: s.Points[i-1].Seq, FromS: s.Points[i-1].TimeS, ToS: s.Points[i].TimeS})
			seg++
		}
		s.Points[i].Seg = seg
	}
	return s
}

// InGap reports whether time t falls strictly inside a gap.
func (s Series) InGap(t float64) bool {
	for _, g := range s.Gaps {
		if t > g.FromS && t < g.ToS {
			return true
		}
	}
	return false
}

// ---------- generic time/value series (blank curve) ----------

type RawTV struct {
	Time  string `json:"time"`
	Value string `json:"value"`
}

type TM struct {
	T float64 `json:"t_s"`
	V float64 `json:"v"`
}

type TMSeries struct {
	Points []TM         `json:"points"`
	Gaps   []Gap        `json:"gaps"`
	Diags  []Diagnostic `json:"diagnostics"`
}

func BuildTMSeries(raw []RawTV, dim Dim, gapFactor float64) TMSeries {
	out := TMSeries{}
	for _, r := range raw {
		t, err := ParseQuantity(r.Time, DimTime)
		if err != nil {
			out.Diags = append(out.Diags, Diagnostic{Where: "time", Raw: r.Time, Detail: err.Error()})
			continue
		}
		v, err := ParseQuantity(r.Value, dim)
		if err != nil {
			out.Diags = append(out.Diags, Diagnostic{Where: "value", Raw: r.Value, Detail: err.Error()})
			continue
		}
		out.Points = append(out.Points, TM{T: t, V: v})
	}
	sort.Slice(out.Points, func(i, j int) bool { return out.Points[i].T < out.Points[j].T })
	if len(out.Points) < 2 {
		return out
	}
	dts := make([]float64, 0, len(out.Points)-1)
	for i := 1; i < len(out.Points); i++ {
		dts = append(dts, out.Points[i].T-out.Points[i-1].T)
	}
	sort.Float64s(dts)
	med := dts[len(dts)/2]
	for i := 1; i < len(out.Points); i++ {
		if out.Points[i].T-out.Points[i-1].T > med*gapFactor {
			out.Gaps = append(out.Gaps, Gap{FromS: out.Points[i-1].T, ToS: out.Points[i].T})
		}
	}
	return out
}

// interpWithin linearly interpolates at t, but never across a gap and never
// outside the sampled range.
func interpWithin(pts []TM, gaps []Gap, t float64) (float64, bool) {
	if len(pts) == 0 || t < pts[0].T || t > pts[len(pts)-1].T {
		return 0, false
	}
	i := sort.Search(len(pts), func(i int) bool { return pts[i].T >= t })
	if i < len(pts) && pts[i].T == t {
		return pts[i].V, true
	}
	if i == 0 {
		return 0, false
	}
	lo, hi := pts[i-1], pts[i]
	for _, g := range gaps {
		if g.FromS == lo.T && g.ToS == hi.T {
			return 0, false // gap: no interpolation across
		}
	}
	if hi.T == lo.T {
		return lo.V, true
	}
	f := (t - lo.T) / (hi.T - lo.T)
	return lo.V + f*(hi.V-lo.V), true
}

// ---------- helpers ----------

func HashID(prefix string, parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return prefix + "_" + hex.EncodeToString(h[:])[:16]
}

func Fingerprint(algoVersion string, params any) string {
	b, _ := json.Marshal(params)
	h := sha256.Sum256([]byte(algoVersion + "|" + string(b)))
	return hex.EncodeToString(h[:])[:12]
}

func round(v, step float64) float64 { return math.Round(v/step) * step }
