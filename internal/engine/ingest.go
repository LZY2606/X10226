package engine

import (
	"fmt"

	"tgareview/internal/domain"
)

// ingestRow converts one accepted source row into its layered domain object.
// Raw text stays on the source-row event; here values are normalized.
func ingestRow(s *State, row domain.SourceRowPayload, eventID string) error {
	batch := row.BatchID
	get := func(name string) string {
		for _, f := range row.Fields {
			if f.Name == name {
				return f.Raw
			}
		}
		return ""
	}
	id := func(kind string, no int) string {
		return domain.ShortID(kind, batch, fmt.Sprint(no))
	}
	switch row.Layer {
	case domain.LayerRaw:
		s.Raw = append(s.Raw, domain.RawPoint{
			ID: id("raw", row.RowNo), BatchID: batch, RowNo: row.RowNo,
			TimeS:  computeQ(get("t"), domain.UnitS),
			TempC:  computeQ(get("temp"), domain.UnitC),
			MassMg: computeQ(get("mass"), domain.UnitMg),
		})
	case domain.LayerDrift:
		s.Drifts = append(s.Drifts, domain.DriftRow{
			ID: id("drift", row.RowNo), BatchID: batch,
			StartS:       computeQ(get("start"), domain.UnitS),
			RateMgPerMin: computeQ(get("rate"), domain.UnitMgPerMin),
			Active:       true,
		})
		s.revisionActive(&s.Drifts)
	case domain.LayerBlank:
		s.Blanks = append(s.Blanks, domain.BlankPoint{
			ID: id("blank", row.RowNo), BatchID: batch,
			TimeS:  computeQ(get("t"), domain.UnitS),
			MassMg: computeQ(get("mass"), domain.UnitMg),
		})
	case domain.LayerProgram:
		s.Program = append(s.Program, domain.ProgramStep{
			ID: id("program", row.RowNo), BatchID: batch,
			StartS:      computeQ(get("start"), domain.UnitS),
			EndS:        computeQ(get("end"), domain.UnitS),
			TargetC:     computeQ(get("target"), domain.UnitC),
			RampCperMin: computeQ(get("ramp"), domain.UnitMgPerMin), // label only
		})
		s.Program[len(s.Program)-1].RampCperMin.Unit = domain.Unit("C/min")
	case domain.LayerGas:
		s.Gas = append(s.Gas, domain.GasEvent{
			ID: id("gas", row.RowNo), BatchID: batch,
			ValveAtS: computeQ(get("at"), domain.UnitS),
			FromGas:  get("from"), ToGas: get("to"),
		})
	case domain.LayerWeight:
		s.Weights = append(s.Weights, domain.WeightRow{
			ID: id("weight", row.RowNo), BatchID: batch,
			Mg:     computeQ(get("mg"), domain.UnitMg),
			Active: true,
		})
		s.revisionActive(&s.Weights)
	case domain.LayerParams:
		// parameter rows are normalized by the service into params.set
	}
	return nil
}

// revisionActive deactivates earlier rows of the same revision family and sets
// Supersedes on the newest, expressing the parent-version relationship.
func (s *State) revisionActive(slice any) {
	switch v := slice.(type) {
	case *[]domain.WeightRow:
		if len(*v) >= 2 {
			newest := &(*v)[len(*v)-1]
			prevID := ""
			for i := range *v {
				if i != len(*v)-1 {
					(*v)[i].Active = false
					prevID = (*v)[i].ID
				}
			}
			newest.Supersedes = prevID
		}
	case *[]domain.DriftRow:
		if len(*v) >= 2 {
			newest := &(*v)[len(*v)-1]
			prevID := ""
			for i := range *v {
				if i != len(*v)-1 {
					(*v)[i].Active = false
					prevID = (*v)[i].ID
				}
			}
			newest.Supersedes = prevID
		}
	}
}

func computeQ(raw string, unit domain.Unit) domain.Q {
	return computeQValue(raw, unit)
}
