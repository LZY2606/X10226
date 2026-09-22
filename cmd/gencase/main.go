// Command gencase regenerates the repository's offline TGA case package:
// baseline bundle, single-source mutations and machine-readable expected
// judgements. Output is deterministic (fixed RNG seed, fixed clock).
package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
)

// splitmix64 is a tiny deterministic PRNG (fixed seed, no global rand).
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }
func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
func (r *rng) float64_() float64 { return float64(r.next()>>11) / float64(uint64(1)<<53) }
func (r *rng) normal() float64 {
	// Box-Muller
	u1, u2 := math.Max(r.float64_(), 1e-12), r.float64_()
	return math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
}

const (
	m0        = 10.0
	driftRate = 0.012 // mg/min slow upward drift measured by the balance
	blankBase = 0.04  // mg buoyancy/blank amplitude
	tau       = 18.0  // thermal lag seconds
)

type pk struct {
	t     float64
	amp   float64 // mg lost
	sigma float64
}

var peaks = []pk{
	{210, 0.12, 12}, // small shoulder (moisture), not a separate mode
	{320, 0.85, 20}, // overlapping pair (320/400, 2 sigma apart):
	{400, 0.85, 20}, // baseline must already offer merged + split legal candidates
	{610, 0.7, 18},
	{770, 0.9, 26},
}

// trueMass(t) integrates gaussian DTG mass losses; residual ≈ 3.3%.
func trueMass(t float64) float64 {
	m := m0
	for _, p := range peaks {
		z := (t - p.t) / p.sigma
		m -= p.amp * normCDF(z)
	}
	return m
}
func normCDF(z float64) float64 { return 0.5 * (1 + math.Erf(z/math.Sqrt2)) }

// furnaceTemp is the programmed furnace temperature (two ramps + holds).
func furnaceTemp(t float64) float64 {
	switch {
	case t < 300: // 50 -> 300C over 300s
		return 50 + (250.0 * t / 300.0)
	case t < 420: // hold 300C
		return 300
	case t < 840: // 300 -> 1000C over 420s
		return 300 + (700.0 * (t - 420) / 420.0)
	default:
		return 1000
	}
}

type pt struct {
	time, temp, mass float64
}

func generatePoints() []pt {
	r := newRNG(20260922)
	var out []pt
	for t := 0.0; t <= 960.0; t += 6 {
		// acquisition gap: skip 486..546
		if t > 480 && t < 552 {
			continue
		}
		m := trueMass(t)
		m += driftRate * t / 60.0          // balance drift
		m += blankBase * math.Sin(t/300.0) // blank/buoyancy curve
		m += 0.0008 * r.normal()           // fixed-seed white noise
		// deterministic non-monotonic blips (raw retained, flagged at derived layer)
		if int(t) == 288 {
			m += 0.0022
		}
		if int(t) == 708 {
			m += 0.0018
		}
		out = append(out, pt{t, furnaceTemp(t), m})
	}
	return out
}

func main() {
	outDir := "cases"
	if len(os.Args) > 1 {
		outDir = os.Args[1]
	}
	points := generatePoints()
	writeBaseline(outDir, points)
	writeMutations(outDir)
	writeJudgements(outDir)
	writeReplay(outDir)
}

func writeFile(path string, v any) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		panic(err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(path, b, 0o644); err != nil {
		panic(err)
	}
}
