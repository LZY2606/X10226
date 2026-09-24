package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

func DefaultParams() Params {
	return Params{GasDelayS: 18, ThermalLagS: 12, DriftOffsetStartUg: 12, DriftOffsetEndUg: 58, PeakThresholdPctMin: 0.12, OverlapWindowMin: 1.4, GapLimitSec: 45}
}

func Compute(input InputBundle) Result {
	params := normalizeParams(input.Params)
	judgments := make([]Judgment, 0)
	for _, source := range input.Sources {
		if source.Status == "failed" {
			judgments = append(judgments, Judgment{Code: "import_half_failure", Severity: "error", Message: source.Error, ObjectKeys: []string{"raw:" + source.ID}, SourceIDs: []string{source.ID}, RuleVersion: AlgorithmCandidates})
		}
	}

	raw, validSamples := parseSamples(input.CurrentByKind[KindSamples], &judgments)
	drift := parseDrift(input.CurrentByKind[KindDrift], params, &judgments)
	furnace := parseFurnace(input.CurrentByKind[KindFurnace], &judgments)
	gas := parseGas(input.CurrentByKind[KindGas], params, &judgments)
	blank := parseBlank(input.CurrentByKind[KindBlank], validSamples, &judgments)
	initial := parseInitialMass(input.CurrentByKind[KindInitialMass], &judgments)
	corrected := correctSamples(validSamples, drift, blank, initial, params, &judgments)
	candidates, finalResidue := detectCandidates(corrected, gas, furnace, initial, params, &judgments)

	sourceIDs := make([]string, 0, len(input.CurrentByKind))
	for _, source := range input.CurrentByKind {
		if source.Status != "failed" {
			sourceIDs = append(sourceIDs, source.ID)
		}
	}
	sort.Strings(sourceIDs)
	hash := parameterFingerprint(params, sourceIDs)
	version := sourceVersionSet(sourceIDs)

	nodes := []Node{
		rawNode(raw, input.CurrentByKind[KindSamples], hash, version),
		correctedNode(corrected, input.CurrentByKind, hash, version),
		furnaceNode(furnace, input.CurrentByKind[KindFurnace], hash, version),
		gasNode(gas, input.CurrentByKind[KindGas], params, hash, version),
		candidateNode(candidates, input.CurrentByKind, hash, version, judgments),
		residueNode(finalResidue, candidates, initial, corrected, input.CurrentByKind, hash, version),
	}
	return Result{Nodes: nodes, Judgments: judgments}
}
