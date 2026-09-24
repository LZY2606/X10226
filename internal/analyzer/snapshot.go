package analyzer

import (
	"sort"

	"github.com/example/tga/internal/domain"
)

// Batch is one immutable committed source batch as seen by derivation.
type Batch struct {
	Seq      int
	ID       string
	Role     string
	Parent   string
	FileName string
	Sections []domain.Section
	Partial  bool
}

// RawPoint is one usable sample row from the source layer.
type RawPoint struct {
	T          float64
	SampleTemp float64
	Mass       float64
	Ref        domain.SourceRef
	HasTemp    bool
	HasMass    bool
}

// ValveEvent is one gas valve event in valve coordinates.
type ValveEvent struct {
	On  float64
	Off float64
	Gas string
	Ref domain.SourceRef
}

// FurnaceRow is one furnace program row.
type FurnaceRow struct {
	T        float64
	Setpoint float64
	Segment  string
	Ref      domain.SourceRef
}

// BlankPoint is one blank-curve point.
type BlankPoint struct {
	T    float64
	Mass float64
	Ref  domain.SourceRef
}

// Snapshot is the layered source state used as derivation input.
type Snapshot struct {
	Batches           []Batch
	InitialMass       domain.Quantity
	InitialRef        domain.SourceRef
	InitialBatchID    string
	InitialRevision   int
	Points            []RawPoint
	Valves            []ValveEvent
	Furnace           []FurnaceRow
	Blank             []BlankPoint
	Diagnostics       []domain.Diagnostic
	Unconvertibles    []domain.RawCell
	HasSampleSection  bool
	HasGasSection     bool
	HasFurnaceSection bool
	HasBlankSection   bool
}

func qValue(s *domain.Section, row int, col string) (float64, bool) {
	if row < 0 || row >= len(s.Rows) {
		return 0, false
	}
	q, ok := s.Rows[row].Fields[col]
	if !ok || q.ParseError != "" {
		return 0, false
	}
	return q.Value, true
}

func firstSection(b *Batch, kind string) *domain.Section {
	for i := range b.Sections {
		if b.Sections[i].Kind == kind {
			return &b.Sections[i]
		}
	}
	return nil
}

// BuildSnapshot layers committed batches in commit order. Later batches
// replace the section content for the kinds they carry; every value still
// carries the SourceRef of the immutable batch/row it came from.
func BuildSnapshot(batches []Batch) *Snapshot {
	snap := &Snapshot{Batches: append([]Batch(nil), batches...)}

	type layer struct {
		batch   *Batch
		section *domain.Section
	}
	latest := map[string]layer{}
	for bi := range batches {
		b := &batches[bi]
		for si := range b.Sections {
			latest[b.Sections[si].Kind] = layer{batch: b, section: &b.Sections[si]}
		}
		if b.Partial {
			snap.Diagnostics = append(snap.Diagnostics, domain.Diagnostic{
				Severity: "warning", Section: "", Code: domain.FindPartialImport,
				Message: "batch " + b.ID + " committed partially; bad rows excluded from derivation",
			})
		}
	}

	// Initial mass: newest @initial wins, parent chain captured by batch order.
	if l, ok := latest["initial"]; ok {
		s := l.section
		if len(s.Rows) > 0 {
			r := s.Rows[0]
			if q, ok := r.Fields["initial_mass_mg"]; ok {
				snap.InitialMass = q
				if q.ParseError != "" {
					snap.Unconvertibles = append(snap.Unconvertibles, domain.RawCell{
						Section: "initial", Column: "initial_mass_mg",
						Raw: q.Raw, Row: 0, Line: r.Line, Diagnosis: q.ParseError,
					})
				} else {
					snap.InitialRef = domain.SourceRef{
						EventSeq: l.batch.Seq, BatchID: l.batch.ID,
						Section: "initial", Row: 0, Column: "initial_mass_mg",
					}
					snap.InitialBatchID = l.batch.ID
				}
			}
		}
	}

	if l, ok := latest["sample"]; ok {
		snap.HasSampleSection = true
		s := l.section
		for ri, r := range s.Rows {
			tv, tok := r.Fields["time_s"]
			if !tok || tv.ParseError != "" {
				continue
			}
			mp, mok := r.Fields["sample_temp_c"]
			mq, mqok := r.Fields["mass_mg"]
			rp := RawPoint{
				T:       tv.Value,
				HasTemp: mok && mp.ParseError == "",
				HasMass: mqok && mq.ParseError == "",
				Ref: domain.SourceRef{
					EventSeq: l.batch.Seq, BatchID: l.batch.ID,
					Section: "sample", Row: ri, Column: "mass_mg",
				},
			}
			if rp.HasTemp {
				rp.SampleTemp = mp.Value
			} else if mok {
				snap.Unconvertibles = append(snap.Unconvertibles, domain.RawCell{
					Section: "sample", Column: "sample_temp_c",
					Raw: mp.Raw, Row: ri, Line: r.Line, Diagnosis: mp.ParseError,
				})
			}
			if rp.HasMass {
				rp.Mass = mq.Value
			} else if mqok {
				snap.Unconvertibles = append(snap.Unconvertibles, domain.RawCell{
					Section: "sample", Column: "mass_mg",
					Raw: mq.Raw, Row: ri, Line: r.Line, Diagnosis: mq.ParseError,
				})
			}
			snap.Points = append(snap.Points, rp)
		}
	}

	if l, ok := latest["gas"]; ok {
		snap.HasGasSection = true
		s := l.section
		for ri, r := range s.Rows {
			on, onOK := qValue(s, ri, "valve_on_s")
			off, offOK := qValue(s, ri, "valve_off_s")
			if !onOK || !offOK {
				continue
			}
			snap.Valves = append(snap.Valves, ValveEvent{
				On: on, Off: off, Gas: r.Tokens["gas"],
				Ref: domain.SourceRef{
					EventSeq: l.batch.Seq, BatchID: l.batch.ID,
					Section: "gas", Row: ri, Column: "gas",
				},
			})
		}
	}

	if l, ok := latest["furnace"]; ok {
		snap.HasFurnaceSection = true
		s := l.section
		for ri := range s.Rows {
			t, tOK := qValue(s, ri, "time_s")
			sp, spOK := qValue(s, ri, "setpoint_c")
			if !tOK || !spOK {
				continue
			}
			snap.Furnace = append(snap.Furnace, FurnaceRow{
				T: t, Setpoint: sp, Segment: s.Rows[ri].Tokens["segment"],
				Ref: domain.SourceRef{
					EventSeq: l.batch.Seq, BatchID: l.batch.ID,
					Section: "furnace", Row: ri, Column: "setpoint_c",
				},
			})
		}
	}

	if l, ok := latest["blank"]; ok {
		snap.HasBlankSection = true
		s := l.section
		for ri := range s.Rows {
			t, tOK := qValue(s, ri, "time_s")
			m, mOK := qValue(s, ri, "blank_mass_mg")
			if !tOK || !mOK {
				continue
			}
			snap.Blank = append(snap.Blank, BlankPoint{
				T: t, Mass: m,
				Ref: domain.SourceRef{
					EventSeq: l.batch.Seq, BatchID: l.batch.ID,
					Section: "blank", Row: ri, Column: "blank_mass_mg",
				},
			})
		}
	}

	sort.SliceStable(snap.Points, func(i, j int) bool { return snap.Points[i].T < snap.Points[j].T })
	sort.SliceStable(snap.Valves, func(i, j int) bool { return snap.Valves[i].On < snap.Valves[j].On })
	sort.SliceStable(snap.Furnace, func(i, j int) bool { return snap.Furnace[i].T < snap.Furnace[j].T })
	sort.SliceStable(snap.Blank, func(i, j int) bool { return snap.Blank[i].T < snap.Blank[j].T })

	return snap
}
