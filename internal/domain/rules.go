package domain

// Machine-readable rule / situation codes. They are persisted verbatim in
// rule_evidence rows and exported inside bundles so conclusions remain
// independently interpretable without the UI.
const (
	RuleAtmosDelay         = "ATMOS_DELAY"            // valve switch != reaction time
	RuleThermalLag         = "THERMAL_LAG"            // programmed temp != sample temp
	RuleStagesMayOverlap   = "STAGES_MAY_OVERLAP"     // decomposition stages can overlap
	RuleNoForcedSum100     = "NO_FORCED_SUM_100"      // mass losses are not renormalised
	RuleGapNoInterpolation = "GAP_NO_INTERPOLATION"   // gaps are never interpolated across
	RuleAmbiguousCandidates= "AMBIGUOUS_KEEP_ALL"     // keep every legal candidate
	RuleStaleNotCurrent    = "STALE_NOT_CURRENT"      // stale run viewable, never current
	RuleDriftCorrected     = "DRIFT_CORRECTED"
	RuleBlankSubtracted    = "BLANK_SUBTRACTED"
	RuleNonMonotonicKept   = "NON_MONOTONIC_KEPT"
	RuleInitialMassRevised = "INITIAL_MASS_REVISED"

	SitAtmosDelay       = "situation.atmos_delay"
	SitBaselineDrift    = "situation.baseline_drift"
	SitOverlapPeaks     = "situation.overlap_peaks"
	SitNonMonotonic     = "situation.non_monotonic_noise"
	SitSamplingGap      = "situation.sampling_gap"
	SitInitialMassRev   = "situation.initial_mass_revision"
	SitImportHalfFail   = "situation.import_half_failure"
)

// RuleStatement returns the canonical human-readable statement behind a code;
// the code itself is the durable machine-readable identity.
func RuleStatement(code string) string {
	switch code {
	case RuleAtmosDelay:
		return "atmosphere delay: a valve switch time is not the sample reaction time"
	case RuleThermalLag:
		return "thermal lag: programmed furnace temperature is not the sample temperature"
	case RuleStagesMayOverlap:
		return "decomposition stages may overlap in time and are not forced to be contiguous"
	case RuleNoForcedSum100:
		return "stage mass losses are not renormalised and are not required to sum to 100 percent"
	case RuleGapNoInterpolation:
		return "data gaps are never interpolated across; derived results that need missing data become unknown"
	case RuleAmbiguousCandidates:
		return "when a boundary cannot be uniquely decided every legal candidate is retained"
	case RuleStaleNotCurrent:
		return "runs invalidated by an input change stay viewable but are marked stale, never current"
	case RuleDriftCorrected:
		return "balance baseline drift was estimated and subtracted in the derived layer"
	case RuleBlankSubtracted:
		return "a blank/baseline curve was subtracted from sample mass in the derived layer"
	case RuleNonMonotonicKept:
		return "non-monotonic mass points are kept with a flag rather than smoothed away"
	case RuleInitialMassRevised:
		return "initial mass was supplied in a new versioned layer; earlier runs become stale"
	default:
		return ""
	}
}
