package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"tga-lineage/internal/domain"
)

func nodeID(kind, key string, srcTags []string, fp string) string {
	tags := append([]string{}, srcTags...)
	sort.Strings(tags)
	h := sha256.Sum256([]byte(kind + "|" + key + "|" + strings.Join(tags, ",") + "|" + fp))
	return "n_" + hex.EncodeToString(h[:])[:16]
}

func srcRef(kind domain.FragmentKind, version int) string {
	return fmt.Sprintf("src:%s:v%d", kind, version)
}

func makeSeries(n int, v float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func timesOf(pts []point) []float64 {
	out := make([]float64, len(pts))
	for i, p := range pts {
		out[i] = p.t
	}
	return out
}

func seriesNode(id, kind, label, unit, basis string, pts []point, _ bool, refs []domain.NodeRef) domain.Node {
	ts := make([]float64, 0, len(pts))
	vs := make([]float64, 0, len(pts))
	for _, p := range pts {
		ts = append(ts, p.t)
		if p.ok {
			vs = append(vs, p.v)
		} else {
			vs = append(vs, math.NaN())
		}
	}
	return domain.Node{ID: id, Kind: kind, Label: label, Unit: unit, Basis: basis,
		Times: ts, Vals: vs, Direct: refs}
}

func sampleTempNode(id string, pts []point, refs []domain.NodeRef) domain.Node {
	ts := make([]float64, 0, len(pts))
	vs := make([]float64, 0, len(pts))
	for _, p := range pts {
		ts = append(ts, p.t)
		if p.tempOK {
			vs = append(vs, p.temp)
		} else {
			vs = append(vs, math.NaN())
		}
	}
	return domain.Node{ID: id, Kind: "sample_points/sample_temp", Label: "样品实测温度（反应温度基准）",
		Unit: "degC", Basis: "t_since_run_start", Times: ts, Vals: vs, Direct: refs}
}

func interpLinear(pts []point, t float64) float64 {
	if len(pts) == 0 {
		return math.NaN()
	}
	if t <= pts[0].t {
		return pts[0].v
	}
	if t >= pts[len(pts)-1].t {
		return pts[len(pts)-1].v
	}
	lo, hi := 0, len(pts)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if pts[mid].t <= t {
			lo = mid
		} else {
			hi = mid
		}
	}
	a, b := pts[lo], pts[hi]
	if b.t == a.t {
		return a.v
	}
	return a.v + (b.v-a.v)*(t-a.t)/(b.t-a.t)
}

// polyFit fits y = c0 + c1 t + ... with least squares (normal equations).
func polyFit(pts []point, order int) []float64 {
	n := order + 1
	m := make([][]float64, n)
	for i := range m {
		m[i] = make([]float64, n+1)
	}
	for _, p := range pts {
		if !p.ok {
			continue
		}
		xp := make([]float64, n)
		xp[0] = 1
		for i := 1; i < n; i++ {
			xp[i] = xp[i-1] * p.t
		}
		for i := 0; i < n; i++ {
			for j := 0; j < n; j++ {
				m[i][j] += xp[i] * xp[j]
			}
			m[i][n] += xp[i] * p.v
		}
	}
	// gaussian elimination
	for col := 0; col < n; col++ {
		piv := col
		for r := col + 1; r < n; r++ {
			if math.Abs(m[r][col]) > math.Abs(m[piv][col]) {
				piv = r
			}
		}
		m[col], m[piv] = m[piv], m[col]
		if math.Abs(m[col][col]) < 1e-12 {
			continue
		}
		for r := col + 1; r < n; r++ {
			f := m[r][col] / m[col][col]
			for c := col; c <= n; c++ {
				m[r][c] -= f * m[col][c]
			}
		}
	}
	coef := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		s := m[r][n]
		for c := r + 1; c < n; c++ {
			s -= m[r][c] * coef[c]
		}
		if math.Abs(m[r][r]) < 1e-12 {
			coef[r] = 0
		} else {
			coef[r] = s / m[r][r]
		}
	}
	return coef
}

func polyEval(coef []float64, t float64) float64 {
	v := 0.0
	for i := len(coef) - 1; i >= 0; i-- {
		v = v*t + coef[i]
	}
	return v
}

// smoothWithin applies a moving average inside each valid segment only; the
// window never crosses a gap boundary.
func smoothWithin(vals, ts []float64, ok []bool, segs []segment, windowS float64) []float64 {
	out := makeSeries(len(vals), math.NaN())
	for _, s := range segs {
		for i := s.start; i <= s.end; i++ {
			if !ok[i] {
				continue
			}
			lo, hi := i, i
			for lo > s.start && ts[lo-1] >= ts[i]-windowS/2 {
				lo--
			}
			for hi < s.end && ts[hi+1] <= ts[i]+windowS/2 {
				hi++
			}
			sum, cnt := 0.0, 0
			for j := lo; j <= hi; j++ {
				if ok[j] && !math.IsNaN(vals[j]) {
					sum += vals[j]
					cnt++
				}
			}
			if cnt > 0 {
				out[i] = sum / float64(cnt)
			}
		}
	}
	return out
}

// dtg computes -dm/dt in mg/min by central differences within segments.
func dtg(ts, vals []float64, segs []segment) ([]float64, []float64, []float64) {
	out := makeSeries(len(vals), math.NaN())
	for _, s := range segs {
		for i := s.start + 1; i < s.end; i++ {
			dt := ts[i+1] - ts[i-1]
			if dt <= 0 || math.IsNaN(vals[i-1]) || math.IsNaN(vals[i+1]) {
				continue
			}
			out[i] = -(vals[i+1] - vals[i-1]) / dt * 60
		}
	}
	tt := make([]float64, len(ts))
	copy(tt, ts)
	return out, tt, out
}

// findNonMonotonic flags local positive drifts (mass gain) inside segments.
func findNonMonotonic(ts, vals []float64, segs []segment) []float64 {
	out := makeSeries(len(vals), math.NaN())
	for _, s := range segs {
		for i := s.start + 1; i <= s.end; i++ {
			if math.IsNaN(vals[i]) || math.IsNaN(vals[i-1]) {
				continue
			}
			if vals[i] > vals[i-1] {
				out[i] = vals[i]
			}
		}
	}
	return out
}

func sampleTempAt(pts []point, t float64) float64 {
	// linear interpolation over valid temp points
	prev, next := -1, -1
	for i, p := range pts {
		if !p.tempOK {
			continue
		}
		if p.t <= t {
			prev = i
		}
		if p.t >= t && next == -1 {
			next = i
		}
	}
	if prev == -1 && next == -1 {
		return math.NaN()
	}
	if prev == -1 {
		return pts[next].temp
	}
	if next == -1 {
		return pts[prev].temp
	}
	if pts[next].t == pts[prev].t {
		return pts[prev].temp
	}
	return pts[prev].temp + (pts[next].temp-pts[prev].temp)*(t-pts[prev].t)/(pts[next].t-pts[prev].t)
}

func furnaceSetpointAt(pts []point, t float64) float64 {
	if len(pts) == 0 {
		return math.NaN()
	}
	return interpLinear(pts, t)
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:16]
}
