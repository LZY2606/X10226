package app

import (
	"context"

	"tgareview/internal/domain"
)

// ImportReport is returned after one import attempt; hard format failures leave
// no source, while partial failures still commit valid rows.
type ImportReport struct {
	Source        *domain.Source `json:"source,omitempty"`
	Event         *domain.Event  `json:"event,omitempty"`
	Kind          string         `json:"kind"`
	Filename      string         `json:"filename"`
	Fatal         string         `json:"fatal,omitempty"`
	AcceptedRows  int            `json:"accepted_rows"`
	RejectedRows  int            `json:"rejected_rows"`
	AffectedScope []string       `json:"affected_scope"`
}

// ImportLayer parses and commits one raw layer. The original text of every row
// is preserved (including rejected ones). Local changes only invalidate
// artifacts that list the previous version as a direct input.
func (a *App) ImportLayer(ctx context.Context, projectID, kind, filename string, data []byte) (ImportReport, error) {
	rep := ImportReport{Kind: kind, Filename: filename}
	pr := domain.ParseCSV(kind, data)
	if pr.Fatal != "" && pr.Accepted == 0 {
		rep.Fatal = pr.Fatal
		_, _ = a.St.AppendEvent(ctx, projectID, domain.EvNote, "importer",
			"导入失败（未提交任何来源）："+filename+" — "+pr.Fatal,
			map[string]any{"kind": kind, "filename": filename, "fatal": pr.Fatal})
		return rep, nil
	}
	src := &domain.Source{
		ProjectID: projectID, Kind: kind, Filename: filename,
		Header: pr.Header, UnitDecl: pr.UnitDecl, Rows: pr.Rows,
		Accepted: pr.Accepted, Rejected: pr.Rejected,
	}
	ev, err := a.St.CommitSource(ctx, src)
	if err != nil {
		return rep, err
	}
	rep.Source = src
	rep.Event = &ev
	rep.AcceptedRows = pr.Accepted
	rep.RejectedRows = pr.Rejected
	rep.AffectedScope = affectedScope(kind, pr.Rejected > 0)
	return rep, nil
}

// affectedScope documents which downstream objects a local source change may
// invalidate. Stages and TG are named explicitly so the UI can flag them.
func affectedScope(kind string, partial bool) []string {
	base := map[string][]string{
		domain.SrcSamplePoints: {"art:" + domain.ArtCorrectedTG, "art:" + domain.ArtDTG, "art:" + domain.ArtCandidates, "art:" + domain.ArtResidue},
		domain.SrcDrift:        {"art:" + domain.ArtCorrectedTG, "art:" + domain.ArtCandidates, "art:" + domain.ArtResidue},
		domain.SrcFurnace:      {"art:" + domain.ArtAtmoTimeline},
		domain.SrcGasValve:     {"art:" + domain.ArtAtmoTimeline, "art:" + domain.ArtCandidates},
		domain.SrcBlank:        {"art:" + domain.ArtCorrectedTG, "art:" + domain.ArtResidue},
		domain.SrcInitialMass:  {"art:" + domain.ArtCorrectedTG, "art:" + domain.ArtCandidates, "art:" + domain.ArtResidue},
	}[kind]
	if partial {
		base = append(base, "partial-import: 合法行已提交，非法行片段保留在来源中")
	}
	return base
}
