package tga

import (
	"fmt"
	"math"
)

// CandidateParams control stage candidate detection.
type CandidateParams struct {
	MinRateMgMin    float64 `json:"min_rate_mg_min"`   // peak detection threshold on |dm/dt|
	OnsetRatio      float64 `json:"onset_ratio"`       // fraction of peak rate defining onset/endset
	OverlapRatio    float64 `json:"overlap_ratio"`     // valley/peak ratio above which peaks overlap
	NoiseBandMg     float64 `json:"noise_band_mg"`     // non-monotonic noise band
	GapThresholdMin float64 `json:"gap_threshold_min"` // sampling gap threshold
	InitialMassMg   float64 `json:"initial_mass_mg"`   // for mass-loss percentages
}

// Stage is one decomposition stage candidate. Stages may overlap in
// temperature; their mass losses are NOT normalized to sum to 100%.
type Stage struct {
	Label       string  `json:"label"`
	TStartC     float64 `json:"t_start_C"`
	TPeakC      float64 `json:"t_peak_C"`
	TEndC       float64 `json:"t_end_C"`
	MassLossPct float64 `json:"mass_loss_pct"`
	Overlapped  bool    `json:"overlapped"`
}

// CandidateSet is one legal decomposition of the DTG trace.
type CandidateSet struct {
	ID     string  `json:"id"`
	Kind   string  `json:"kind"` // "split" or "merged"
	Stages []Stage `json:"stages"`
}

// OverlapJudgment records that stages may overlap and sums are not forced.
type OverlapJudgment struct {
	Rule        string  `json:"rule"` // "stages.overlap_allowed_sum_not_forced"
	PeakA_TempC float64 `json:"peak_a_temp_C"`
	PeakB_TempC float64 `json:"peak_b_temp_C"`
	ValleyRate  float64 `json:"valley_rate_mg_min"`
}

// AmbiguityJudgment records that no unique decomposition exists and all
// legal candidates were retained.
type AmbiguityJudgment struct {
	Rule         string   `json:"rule"` // "candidates.ambiguous_all_retained"
	CandidateIDs []string `json:"candidate_ids"`
}

// Candidates is the derived result of stage detection.
type Candidates struct {
	Sets      []CandidateSet    `json:"sets"`
	Ambiguous bool              `json:"ambiguous"`
	Overlaps  []OverlapJudgment `json:"overlaps,omitempty"`
}

type peak struct {
	idx   int
	temp  float64
	rate  float64 // positive loss rate mg/min
	left  int     // onset index
	right int     // endset index
}

// FindCandidates detects DTG peaks per segment and builds candidate sets.
// Overlapping peak groups produce both "split" and "merged" decompositions;
// all are returned and flagged ambiguous.
func FindCandidates(s *Series, d *DTG, p CandidateParams) (*Candidates, []OverlapJudgment, *AmbiguityJudgment) {
	res := &Candidates{}
	var overlaps []OverlapJudgment
	var allStages []Stage
	ambiguous := false

	for si, sg := range d.Segments {
		peaks := detectPeaks(s, d, sg, p)
		if len(peaks) == 0 {
			continue
		}
		// group peaks into overlap clusters
		groups := [][]peak{}
		cur := []peak{peaks[0]}
		for i := 1; i < len(peaks); i++ {
			vr := valleyRate(d, peaks[i-1].idx, peaks[i].idx)
			thr := p.OverlapRatio * math.Min(peaks[i-1].rate, peaks[i].rate)
			if vr > thr {
				cur = append(cur, peaks[i])
				overlaps = append(overlaps, OverlapJudgment{
					Rule:        "stages.overlap_allowed_sum_not_forced",
					PeakA_TempC: peaks[i-1].temp,
					PeakB_TempC: peaks[i].temp,
					ValleyRate:  vr,
				})
			} else {
				groups = append(groups, cur)
				cur = []peak{peaks[i]}
			}
		}
		groups = append(groups, cur)

		segSets := []CandidateSet{{Kind: "split"}, {Kind: "merged"}}
		n := 0
		for _, g := range groups {
			for _, pk := range g {
				n++
				st := Stage{
					Label:       fmt.Sprintf("S%d", n),
					TStartC:     s.Temp[pk.left],
					TPeakC:      pk.temp,
					TEndC:       s.Temp[pk.right],
					MassLossPct: massLoss(s, pk.left, pk.right, p.InitialMassMg),
					Overlapped:  len(g) > 1,
				}
				segSets[0].Stages = append(segSets[0].Stages, st)
				allStages = append(allStages, st)
			}
			if len(g) > 1 {
				lo, hi := g[0].left, g[len(g)-1].right
				peakT, peakR := 0.0, 0.0
				for _, pk := range g {
					if pk.rate > peakR {
						peakR, peakT = pk.rate, pk.temp
					}
				}
				segSets[1].Stages = append(segSets[1].Stages, Stage{
					Label:       fmt.Sprintf("M%d", len(segSets[1].Stages)+1),
					TStartC:     s.Temp[lo],
					TPeakC:      peakT,
					TEndC:       s.Temp[hi],
					MassLossPct: massLoss(s, lo, hi, p.InitialMassMg),
					Overlapped:  true,
				})
			} else {
				segSets[1].Stages = append(segSets[1].Stages, segSets[0].Stages[len(segSets[0].Stages)-1])
			}
		}
		hasOverlap := false
		for _, g := range groups {
			if len(g) > 1 {
				hasOverlap = true
			}
		}
		if hasOverlap {
			ambiguous = true
			for i := range segSets {
				segSets[i].ID = fmt.Sprintf("seg%d-%s-%s", si, segSets[i].Kind, Fingerprint(segSets[i].Stages))
				res.Sets = append(res.Sets, segSets[i])
			}
		} else {
			segSets[0].ID = fmt.Sprintf("seg%d-split-%s", si, Fingerprint(segSets[0].Stages))
			res.Sets = append(res.Sets, segSets[0])
		}
	}
	res.Ambiguous = ambiguous
	var amb *AmbiguityJudgment
	if ambiguous {
		amb = &AmbiguityJudgment{Rule: "candidates.ambiguous_all_retained"}
		for _, set := range res.Sets {
			amb.CandidateIDs = append(amb.CandidateIDs, set.ID)
		}
	}
	return res, overlaps, amb
}

func detectPeaks(s *Series, d *DTG, sg Segment, p CandidateParams) []peak {
	var peaks []peak
	for i := sg.Start + 1; i < sg.End-1; i++ {
		r := -d.RateMgMin[i]
		if r < p.MinRateMgMin {
			continue
		}
		if -d.RateMgMin[i-1] <= r && -d.RateMgMin[i+1] <= r &&
			(-d.RateMgMin[i-1] < r || -d.RateMgMin[i+1] < r) {
			pk := peak{idx: i, temp: s.Temp[i], rate: r}
			// onset: walk left until rate drops below OnsetRatio*peak
			pk.left = sg.Start
			for j := i; j > sg.Start; j-- {
				if -d.RateMgMin[j] < p.OnsetRatio*r {
					pk.left = j
					break
				}
			}
			pk.right = sg.End - 1
			for j := i; j < sg.End-1; j++ {
				if -d.RateMgMin[j] < p.OnsetRatio*r {
					pk.right = j
					break
				}
			}
			peaks = append(peaks, pk)
		}
	}
	return peaks
}

func valleyRate(d *DTG, a, b int) float64 {
	v := math.Inf(1)
	for i := a; i <= b && i < len(d.RateMgMin); i++ {
		if -d.RateMgMin[i] < v {
			v = -d.RateMgMin[i]
		}
	}
	return v
}

func massLoss(s *Series, lo, hi int, initialMg float64) float64 {
	if initialMg <= 0 || hi <= lo {
		return 0
	}
	return (s.Mass[lo] - s.Mass[hi]) / initialMg * 100
}

// ---- residue ---------------------------------------------------------------

// ChainStep is one expandable step of a residue calculation chain.
type ChainStep struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Unit  string `json:"unit"`
	Ref   string `json:"ref"` // stable identity of the contributing source/derived
}

// Residue is the final residue percentage with its full calculation chain.
type Residue struct {
	Percent       float64     `json:"percent"`
	FinalMassMg   float64     `json:"final_mass_mg"`
	InitialMassMg float64     `json:"initial_mass_mg"`
	Chain         []ChainStep `json:"chain"`
}

// ComputeResidue builds the residue value and its auditable chain.
// initialRaw is the raw representation as submitted (e.g. "12.483,mg").
func ComputeResidue(corrected *Series, initialMg float64, initialRaw, initialRef, correctedRef string) *Residue {
	final := corrected.Mass[len(corrected.Mass)-1]
	r := &Residue{
		Percent:       final / initialMg * 100,
		FinalMassMg:   final,
		InitialMassMg: initialMg,
	}
	r.Chain = []ChainStep{
		{Label: "样品初重（原始表示）", Value: initialRaw, Unit: "as-submitted", Ref: initialRef},
		{Label: "样品初重（规范化）", Value: fmt.Sprintf("%.4f", initialMg), Unit: "mg", Ref: initialRef},
		{Label: "漂移+空白校正后末点质量", Value: fmt.Sprintf("%.4f", final), Unit: "mg", Ref: correctedRef},
		{Label: "残留率 = 末点质量 / 初重 × 100", Value: fmt.Sprintf("%.3f", r.Percent), Unit: "%", Ref: correctedRef},
	}
	return r
}
