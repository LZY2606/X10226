package engine

import (
	"tgaline/internal/domain"
)

// All derived ids are content-addressed so the same inputs+algorithm+params
// reproduce the same id after export/delete/reimport.
func nodeID(kind domain.NodeKind, v any) string {
	return domain.ShortID("n_"+string(kind)+"_", v)
}

func peakID(centerS, height float64, gid string, k int) string {
	return domain.ShortID("pk_", []any{"peak", gid, k, round4(centerS), round4(height)})
}

func groupID(minT, maxT float64, count int) string {
	return domain.ShortID("gr_", []any{"group", round4(minT), round4(maxT), count})
}

func windowID(w domain.Window) string {
	return domain.ShortID("win_", []any{"window", round4(w.StartS), round4(w.EndS)})
}

func stageID(w domain.Window, massLoss float64, salt int) string {
	return domain.ShortID("st_", []any{"stage", salt, round4(w.StartS), round4(w.EndS), round4(massLoss)})
}

func residueID(atS float64, m0 string) string {
	return domain.ShortID("rs_", []any{"residue", round4(atS), m0})
}

func reactionID(gas string, valveS, startS, endS float64) string {
	return domain.ShortID("rw_", []any{"reaction", gas, round4(valveS), round4(startS), round4(endS)})
}
