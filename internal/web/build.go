package web

import (
	"tga-lineage/internal/model"
	"tga-lineage/internal/project"
)

func BuildView(st *project.State) View {
	v := View{
		Project: ProjectView{
			UID: st.Info.UID, Name: st.Info.Name,
			Description: st.Info.Description, CreatedAt: st.Info.CreatedAt,
		},
		Seq: st.Seq, MaxSeq: st.Seq, Historical: false,
		Params: project.ParamsPublic(st.CurrentParamSet()),
	}
	// layer history (all versions) for provenance expansion
	for _, l := range st.AllLayers() {
		lv := LayerView{
			UID: l.UID, Kind: l.Kind, Version: itoa(l.Version),
			Parent: l.Parent, Source: l.Source, SHA256: l.SHA256,
			Status: l.Status, RecordCount: len(l.Records),
			DiagCount: len(l.Diags), Basis: l.Basis,
		}
		for _, d := range l.Diags {
			lv.Diagnostics = append(lv.Diagnostics, DiagView{
				Ref: d.Ref, Field: d.Field, Raw: d.Raw, Code: d.Code, Message: d.Message,
			})
		}
		v.Layers = append(v.Layers, lv)
	}
	// decisions
	decByUID := map[string]DecisionView{}
	for _, d := range st.Decisions {
		dv := decisionView(d)
		decByUID[d.UID] = dv
		v.Decisions = append(v.Decisions, dv)
	}
	for _, a := range st.CurrentArtifacts() {
		av := ArtifactView{
			UID: a.UID, Kind: a.Kind, Alg: a.Alg, ParamUID: a.ParamUID,
			ParamsFP: a.ParamsFP, Status: a.Status,
			Direct: a.Direct, DependsOn: a.DependsOn,
			Fresh: st.ArtifactFresh(a), PointCount: len(a.Series),
			Reasons: a.Notes,
		}
		for _, d := range st.DecisionsFor(a.UID) {
			dv := decisionView(d)
			av.Decisions = append(av.Decisions, dv)
		}
		v.Artifacts = append(v.Artifacts, av)

		for _, sg := range a.Stages {
			s := StageView{
				UID: sg.UID, Index: sg.Index, StartMin: sg.StartMin, EndMin: sg.EndMin,
				PeakMin: sg.PeakMin, PeakTempC: sg.PeakTempC, DTGPeak: sg.DTGPeak,
				StartMassMg: sg.StartMassMg, EndMassMg: sg.EndMassMg, LossMg: sg.LossMg,
				LossPct: sg.LossPct, OverlapsWith: sg.OverlapsWith,
				GasAtStart: sg.GasAtStart, GasAtPeak: sg.GasAtPeak,
				EffStartMin: sg.EffStartMin, EffEndMin: sg.EffEndMin,
				Certain: sg.Certain, Status: sg.Status, Reasons: sg.Reasons,
				GapLimited: sg.GapLimited, ArtifactUID: a.UID, Fresh: true,
			}
			if d := effectiveDecision(st, sg.UID); d != nil {
				dd := decisionView(*d)
				s.Decision = &dd
			}
			v.Stages = append(v.Stages, s)
		}
		for _, rc := range a.Residuals {
			rv := ResidualView{
				UID: rc.UID, Label: rc.Label, Certain: rc.Certain,
				ArtifactUID: a.UID, AtMin: rc.AtMin, WindowMin: rc.WindowMin,
				ValuePct: rc.ValuePct, Reasons: rc.Reasons, Fresh: true,
			}
			for _, n := range rc.Chain {
				rv.Chain = append(rv.Chain, ChainNodeView{
					UID: n.UID, Label: n.Label, Value: n.Value, Unit: n.Unit,
					Number: n.Number, Source: n.Source, Note: n.Note,
				})
			}
			if d := effectiveDecision(st, rc.UID); d != nil {
				dd := decisionView(*d)
				rv.Decision = &dd
			}
			v.Residuals = append(v.Residuals, rv)
		}
		for _, w := range a.Valves {
			v.Valves = append(v.Valves, ValveView{
				UID: w.UID, Gas: w.Gas, ValveAtMin: w.ValveAtMin,
				EffAtMin: w.EffAtMin, DelayMin: w.DelayMin, Fresh: true,
			})
		}
	}
	for _, j := range st.Judgments {
		v.Judgments = append(v.Judgments, JudgmentView{
			Code: j.Code, Severity: j.Severity, Subject: j.Subject,
			Statement: j.Statement, Rule: j.Rule, Refs: j.Refs,
		})
	}
	for _, c := range st.Counters {
		v.Counterexamples = append(v.Counterexamples, CounterView{
			UID: c.UID, Scenario: c.Scenario, Project: c.Project,
			DirectSource: c.DirectSource, Verdict: c.Verdict,
			Affected: c.Affected, Unaffected: c.Unaffected,
			ExpectedScope: c.ExpectedScope, BlastOK: c.BlastOK,
			Adopted: c.Adopted, RejectionNote: c.RejectionNote,
		})
	}
	for _, ev := range st.Events {
		v.History = append(v.History, EventView{
			Seq: ev.Seq, UID: ev.UID, Kind: ev.Kind, At: ev.At,
			Note: ev.Note, PayloadType: ev.PayloadType,
		})
	}
	tg, dtg, f, tl := RenderCharts(st)
	v.Charts = ChartsView{TG: tg, DTG: dtg, Furnace: f, Timeline: tl}
	return v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func effectiveDecision(st *project.State, uid string) *model.Decision {
	return st.EffectiveDecision(uid)
}

func decisionView(d model.Decision) DecisionView {
	return DecisionView{
		UID: d.UID, Target: d.TargetUID, Kind: d.TargetKind,
		Verdict: d.Verdict, Note: d.Note, ParamsUID: d.ParamsUID,
		ConflictsWith: d.ConflictsWith,
	}
}
