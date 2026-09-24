package engine

import "tgaline/internal/domain"

func sortStrings(s []string) {
	// simple insertion sort keeps zero-dep and deterministic
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func rowsCanon(rows []domain.LayerRow) []any {
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		var tq, Tq, mq any
		if r.TimeQ != nil {
			tq = r.TimeQ.Canonical()
		}
		if r.TQ != nil {
			Tq = r.TQ.Canonical()
		}
		if r.MassQ != nil {
			mq = r.MassQ.Canonical()
		}
		out = append(out, []any{
			r.LineNo, r.Raw, string(r.Status), r.Fields, r.Diagnostic, tq, Tq, mq, r.Extra,
		})
	}
	return out
}

func diagsCanon(ds []domain.ImportDiagnostic) []any {
	out := make([]any, 0, len(ds))
	for _, d := range ds {
		out = append(out, []any{d.LineNo, d.Level, d.Code, d.Message, d.Raw})
	}
	return out
}
