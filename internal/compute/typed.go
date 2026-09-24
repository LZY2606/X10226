package compute

import (
	"fmt"

	"github.com/thermal/tga/internal/domain"
)

// Typed holds parsed views of the stratified raw layers. Invalid cells stay on
// the point (Valid=false + Diag); they are excluded from numeric series but are
// never discarded.
type Typed struct {
	Samples       []domain.SamplePoint
	Drift         []domain.DriftPoint
	Furnace       []domain.FurnaceSegment
	Valves        []domain.ValveEvent
	Blank         []domain.BlankPoint
	Initial       *domain.InitialMass
	SampleDiags   []domain.Diagnostic
	MissingLayers []domain.Layer
}

func buildTyped(raw domain.RawSet) Typed {
	var t Typed
	put := func(l domain.Layer) *domain.RawLayer { return raw.Latest(l) }

	if rl := put(domain.LayerSamples); rl != nil {
		t.Samples = parseSamples(rl)
	} else {
		t.MissingLayers = append(t.MissingLayers, domain.LayerSamples)
	}
	if rl := put(domain.LayerDrift); rl != nil {
		t.Drift = parseDrift(rl)
	} else {
		t.MissingLayers = append(t.MissingLayers, domain.LayerDrift)
	}
	if rl := put(domain.LayerFurnace); rl != nil {
		t.Furnace = parseFurnace(rl)
	} else {
		t.MissingLayers = append(t.MissingLayers, domain.LayerFurnace)
	}
	if rl := put(domain.LayerGasValve); rl != nil {
		t.Valves = parseValves(rl)
	} else {
		t.MissingLayers = append(t.MissingLayers, domain.LayerGasValve)
	}
	if rl := put(domain.LayerBlank); rl != nil {
		t.Blank = parseBlank(rl)
	} else {
		t.MissingLayers = append(t.MissingLayers, domain.LayerBlank)
	}
	if rl := put(domain.LayerMass0); rl != nil {
		t.Initial = parseMass0(rl)
	} else {
		t.MissingLayers = append(t.MissingLayers, domain.LayerMass0)
	}
	return t
}

func col(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}

func cell(rows [][]string, r, c int) string {
	if c < 0 || c >= len(rows[r]) {
		return ""
	}
	return rows[r][c]
}

func parseSamples(rl *domain.RawLayer) []domain.SamplePoint {
	ti, mi, ei := col(rl.Header, "time"), col(rl.Header, "mass"), col(rl.Header, "temp")
	out := make([]domain.SamplePoint, 0, len(rl.Rows))
	for r := range rl.Rows {
		sp := domain.SamplePoint{Index: r}
		sp.Time = domain.ParseMeasurement(cell(rl.Rows, r, ti), rl.Units.Time, domain.DimTime)
		sp.Temp = domain.ParseMeasurement(cell(rl.Rows, r, ei), rl.Units.Temp, domain.DimTemp)
		sp.Mass = domain.ParseMeasurement(cell(rl.Rows, r, mi), rl.Units.Mass, domain.DimMass)
		if !sp.Time.Valid {
			sp.Diag = joinDiag(sp.Diag, "time: "+sp.Time.Diag)
		}
		if !sp.Temp.Valid {
			sp.Diag = joinDiag(sp.Diag, "temp: "+sp.Temp.Diag)
		}
		if !sp.Mass.Valid {
			sp.Diag = joinDiag(sp.Diag, "mass: "+sp.Mass.Diag)
		}
		out = append(out, sp)
	}
	return out
}

func parseDrift(rl *domain.RawLayer) []domain.DriftPoint {
	ti, mi := col(rl.Header, "time"), col(rl.Header, "mass")
	out := make([]domain.DriftPoint, 0, len(rl.Rows))
	for r := range rl.Rows {
		out = append(out, domain.DriftPoint{
			Index: r,
			Time:  domain.ParseMeasurement(cell(rl.Rows, r, ti), rl.Units.Time, domain.DimTime),
			Mass:  domain.ParseMeasurement(cell(rl.Rows, r, mi), rl.Units.Mass, domain.DimMass),
		})
	}
	return out
}

func parseBlank(rl *domain.RawLayer) []domain.BlankPoint {
	ti, mi := col(rl.Header, "time"), col(rl.Header, "mass")
	out := make([]domain.BlankPoint, 0, len(rl.Rows))
	for r := range rl.Rows {
		out = append(out, domain.BlankPoint{
			Index: r,
			Time:  domain.ParseMeasurement(cell(rl.Rows, r, ti), rl.Units.Time, domain.DimTime),
			Mass:  domain.ParseMeasurement(cell(rl.Rows, r, mi), rl.Units.Mass, domain.DimMass),
		})
	}
	return out
}

func parseFurnace(rl *domain.RawLayer) []domain.FurnaceSegment {
	si, ei, sti, tti, ki := col(rl.Header, "start"), col(rl.Header, "end"), col(rl.Header, "start_temp"), col(rl.Header, "target_temp"), col(rl.Header, "kind")
	out := make([]domain.FurnaceSegment, 0, len(rl.Rows))
	for r := range rl.Rows {
		out = append(out, domain.FurnaceSegment{
			Index:      r,
			Start:      domain.ParseMeasurement(cell(rl.Rows, r, si), rl.Units.Time, domain.DimTime),
			End:        domain.ParseMeasurement(cell(rl.Rows, r, ei), rl.Units.Time, domain.DimTime),
			StartTemp:  domain.ParseMeasurement(cell(rl.Rows, r, sti), rl.Units.Temp, domain.DimTemp),
			TargetTemp: domain.ParseMeasurement(cell(rl.Rows, r, tti), rl.Units.Temp, domain.DimTemp),
			Kind:       cell(rl.Rows, r, ki),
		})
	}
	return out
}

func parseValves(rl *domain.RawLayer) []domain.ValveEvent {
	ti, fi, toi := col(rl.Header, "time"), col(rl.Header, "from"), col(rl.Header, "to")
	out := make([]domain.ValveEvent, 0, len(rl.Rows))
	for r := range rl.Rows {
		out = append(out, domain.ValveEvent{
			Index: r,
			Time:  domain.ParseMeasurement(cell(rl.Rows, r, ti), rl.Units.Time, domain.DimTime),
			From:  cell(rl.Rows, r, fi),
			To:    cell(rl.Rows, r, toi),
		})
	}
	return out
}

func parseMass0(rl *domain.RawLayer) *domain.InitialMass {
	ti, mi := col(rl.Header, "time"), col(rl.Header, "mass")
	if len(rl.Rows) == 0 {
		return nil
	}
	r := len(rl.Rows) - 1 // latest revision wins within the layer
	return &domain.InitialMass{
		Revision: rl.Version,
		Time:     domain.ParseMeasurement(cell(rl.Rows, r, ti), rl.Units.Time, domain.DimTime),
		Mass:     domain.ParseMeasurement(cell(rl.Rows, r, mi), rl.Units.Mass, domain.DimMass),
		Reason:   rl.Note,
	}
}

func joinDiag(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

var _ = fmt.Sprintf
