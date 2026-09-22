package engine

import (
	"fmt"
	"sort"
	"strconv"

	"tga-review/internal/domain"
	"tga-review/internal/importer"
)

const (
	AlgoVersion = "tga-derive-1.0.0"
	// GapThresholdS: sample spacing larger than gapFactor x median spacing is
	// a sampling gap. Gaps split segments and are never interpolated across.
	GapFactor = 5.0
)

type result struct {
	artifacts []domain.Artifact
	sourceIDs []string
}

// derive runs every layer for the snapshot. Stable artifact ids make repeat
// derivation idempotent; the service keeps only unknown ids.
func Derive(snap *Snapshot, importSeq int64) ([]domain.Artifact, error) {
	var arts []domain.Artifact
	ids := func(kind string) []string {
		var out []string
		for _, rs := range snap.Records {
			for _, r := range rs {
				if r.Kind == kind {
					out = append(out, r.ID)
				}
			}
		}
		sort.Strings(out)
		return out
	}

	vs, vArt := validateSeries(snap, ids(domain.SrcPoints))
	arts = append(arts, vArt)

	segArt, segs := buildSegments(snap, vArt.ID, vs)
	arts = append(arts, segArt)

	corArt, cor := buildCorrected(snap, vArt.ID, vs)
	arts = append(arts, corArt)

	dtgArt, dtg := buildDTG(snap, corArt.ID, cor, segs)
	arts = append(arts, dtgArt)

	furnArt, furn := buildFurnace(snap, ids(domain.SrcFurnace))
	arts = append(arts, furnArt)

	gasArt, gasTL := buildGasTimeline(snap, ids(domain.SrcGas))
	arts = append(arts, gasArt)

	setArt := buildProposals(snap, corArt.ID, dtgArt.ID, furnArt.ID, gasArt.ID, cor, dtg, segs, furn, gasTL)
	arts = append(arts, setArt)

	resArt := buildResidual(snap, vArt.ID, corArt.ID, setArt.ID, vs, cor, setArt)
	arts = append(arts, resArt)

	balArt := buildBalance(snap, resArt.ID, setArt.ID, cor, resArt)
	arts = append(arts, balArt)

	for i := range arts {
		if arts[i].CreatedSeq == 0 {
			arts[i].CreatedSeq = importSeq
		}
	}
	return arts, nil
}

func validateSeries(snap *Snapshot, srcIDs []string) (*domain.ValidatedSeries, domain.Artifact) {
	recs := snap.Records[domain.SrcPoints]
	vs := &domain.ValidatedSeries{}
	for _, r := range recs {
		f := r.Fields
		t, tu, _ := importer.NormalizeTime(f[1], f[2])
		T, Tu, _ := importer.NormalizeTemp(f[3], f[4])
		s := domain.Sample{
			Index: len(vs.Samples), LineNo: r.LineNo, TimeS: t, TempC: T,
			RawTime: f[1] + " " + f[2], RawTemp: f[3] + " " + f[4],
			RawMass: f[5] + " " + f[6],
		}
		if m, mu, err := importer.NormalizeMass(f[5], f[6]); err == nil {
			s.MassMg = m
			vs.MassUnit = mu
		} else {
			s.Excluded = true
			s.Reason = "unconvertible_mass:" + err.Error()
		}
		vs.TimeUnit, vs.TempUnit = tu, Tu
		vs.Samples = append(vs.Samples, s)
	}
	art := mkArtifact(snap, domain.ArtValidated, "validated-series", "Raw TG points (SI normalized frame)", srcIDs, nil, vs)
	return vs, art
}

func buildFurnace(snap *Snapshot, srcIDs []string) (domain.Artifact, *domain.FurnaceProgram) {
	fp := &domain.FurnaceProgram{}
	for _, r := range snap.Records[domain.SrcFurnace] {
		f := r.Fields
		fromS, _, _ := importer.NormalizeTime(f[2], f[3])
		toS, _, _ := importer.NormalizeTime(f[4], f[5])
		st := domain.FurnaceStep{Index: len(fp.Steps), FromS: fromS, ToS: toS, RawText: r.RawText}
		if f[1] == "ramp" {
			t0, _ := strconv.ParseFloat(f[6], 64)
			t1, _ := strconv.ParseFloat(f[7], 64)
			st.StartC, st.EndC = t0, t1
			if toS > fromS {
				st.RateCPerMin = (t1 - t0) / ((toS - fromS) / 60.0)
			}
		} else {
			tc, _ := strconv.ParseFloat(f[6], 64)
			st.StartC, st.EndC = tc, tc
			st.Hold = true
		}
		fp.Steps = append(fp.Steps, st)
	}
	sort.Slice(fp.Steps, func(i, j int) bool { return fp.Steps[i].FromS < fp.Steps[j].FromS })
	for i := range fp.Steps {
		fp.Steps[i].Index = i
	}
	art := mkArtifact(snap, domain.ArtFurnace, "furnace-program", "Furnace temperature program", srcIDs, nil, fp)
	return art, fp
}

func buildGasTimeline(snap *Snapshot, srcIDs []string) (domain.Artifact, *domain.GasTimeline) {
	tl := &domain.GasTimeline{}
	delay := snap.Params.AtmosDelayS
	lag := snap.Params.TempLagC
	for _, r := range snap.Records[domain.SrcGas] {
		f := r.Fields
		atS, _, _ := importer.NormalizeTime(f[2], f[3])
		flow, _ := strconv.ParseFloat(f[6], 64)
		tl.Events = append(tl.Events, domain.GasEvent{
			Index: len(tl.Events), AtS: atS, FromGas: f[4], ToGas: f[5],
			FlowMLMin: flow, DelayS: delay, ReactionAtS: atS + delay,
			TempLagC: lag, RawText: r.RawText,
		})
	}
	sort.Slice(tl.Events, func(i, j int) bool { return tl.Events[i].AtS < tl.Events[j].AtS })
	for i := range tl.Events {
		tl.Events[i].Index = i
	}
	deps := []string{}
	art := mkArtifact(snap, domain.ArtGasLine, "gas-timeline",
		fmt.Sprintf("Gas valve events with %.0fs atmosphere delay and %.1fC lag", delay, lag),
		srcIDs, deps, tl)
	return art, tl
}
