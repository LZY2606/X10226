package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"tgasys/internal/domain"
)

// DefaultParameters returns the version-1 algorithm parameter set.
func DefaultParameters() *domain.Parameters {
	p := &domain.Parameters{
		AlgoVersion: "tga-decomp-v1",
		AtmDelayS:   30,
		TempLagTauS: 45,
		DriftModel:  "linear",
		BlankSubtract: true,
		DTGWindowS:  60,
		PeakPromPct: 0.05,
		MergeGapS:   120,
		OverlapSplit: "auto",
		GapInterp:   false,
		NoiseMonotonic: false,
	}
	p.Fingerprint = fingerprint(p)
	return p
}

// ReadParameters reads key=value lines from a parameters section.
func ReadParameters(raw string) (*domain.Parameters, []domain.Issue) {
	p := DefaultParameters()
	var issues []domain.Issue
	for _, line := range nonCommentLines(raw) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		i := strings.Index(line, "=")
		if i < 0 {
			issues = append(issues, domain.Issue{Code: "params.bad_line", Severity: "warning", Message: line})
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		if err := assign(p, k, v); err != nil {
			issues = append(issues, domain.Issue{Code: "params.bad_value", Severity: "error", Message: err.Error()})
		}
	}
	p.Fingerprint = fingerprint(p)
	return p, issues
}

func assign(p *domain.Parameters, k, v string) error {
	switch k {
	case "atm_delay_s":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		p.AtmDelayS = f
	case "temp_lag_tau_s":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		p.TempLagTauS = f
	case "drift_model":
		p.DriftModel = v
	case "blank_subtract":
		p.BlankSubtract = v == "true" || v == "1"
	case "dtg_window_s":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		p.DTGWindowS = f
	case "peak_prominence_pct":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		p.PeakPromPct = f
	case "merge_gap_s":
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return err
		}
		p.MergeGapS = f
	case "overlap_split":
		p.OverlapSplit = v
	case "gap_interpolate":
		// Deliberately honored but the engine refuses true across data gaps.
		p.GapInterp = v == "true" || v == "1"
	case "noise_enforce_mono":
		p.NoiseMonotonic = v == "true" || v == "1"
	default:
		return fmt.Errorf("unknown parameter %q", k)
	}
	return nil
}

func fingerprint(p *domain.Parameters) string {
	keys := []string{
		"algo", p.AlgoVersion,
		"atm_delay", strconv.FormatFloat(p.AtmDelayS, 'f', 3, 64),
		"lag_tau", strconv.FormatFloat(p.TempLagTauS, 'f', 3, 64),
		"drift", p.DriftModel,
		"blank", strconv.FormatBool(p.BlankSubtract),
		"dtg_w", strconv.FormatFloat(p.DTGWindowS, 'f', 3, 64),
		"prom", strconv.FormatFloat(p.PeakPromPct, 'g', 6, 64),
		"merge", strconv.FormatFloat(p.MergeGapS, 'f', 3, 64),
		"split", p.OverlapSplit,
		"interp", strconv.FormatBool(p.GapInterp),
		"mono", strconv.FormatBool(p.NoiseMonotonic),
	}
	sort.Strings(keys)
	h := sha256.Sum256([]byte(strings.Join(keys, "|")))
	return "fp_" + hex.EncodeToString(h[:8])
}
