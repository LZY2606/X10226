package importer_test

import (
	"strings"
	"testing"

	"tga-review/internal/domain"
	"tga-review/internal/importer"
)

const goodBundle = `# @layer samples
t[s] mass[mg] temp[degC]
0 12.5 30
60 12.1 40
# @layer drift
t[s] offset[mg] kind
0 0.01 linear
# @layer program
t_start[s] t_end[s] temp_start[degC] temp_end[degC] label
0 60 30 30 equilibrate
# @layer gas
t[s] from_gas to_gas flow[sccm]
0 none N2 50
600 N2 air 50
# @layer blank
t[s] mass[mg]
0 0.02
60 0.03
# @layer initial
mass[mg] reason
12.5 "initial weighing"
`

func TestParsesAllSixLayers(t *testing.T) {
	res, err := importer.Parse("prj1", "unit-ok", []byte(goodBundle))
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range domain.Layers {
		if !contains(res.AcceptedLayers, l) {
			t.Fatalf("layer %s not accepted: %+v", l, res.AcceptedLayers)
		}
	}
	if res.Data.Samples[1].T.ConvertedValue != 60 {
		t.Fatal("time conversion")
	}
	if res.Data.Samples[0].Mass.Raw != "12.5" {
		t.Fatalf("raw representation must be retained, got %q", res.Data.Samples[0].Mass.Raw)
	}
}

func TestUnitNormalisation(t *testing.T) {
	txt := strings.Replace(goodBundle, "0 12.5 30", "0 12500 303.15", 1)
	txt = strings.Replace(txt, "t[s] mass[mg] temp[degC]", "t[min] mass[ug] temp[K]", 1)
	res, err := importer.Parse("p", "u", []byte(txt))
	if err != nil {
		t.Fatal(err)
	}
	p0 := res.Data.Samples[0]
	if p0.T.Canonical != domain.UnitSecond || p0.T.ConvertedValue != 0 {
		t.Fatalf("time canonical: %+v", p0.T)
	}
	if p0.Mass.Canonical != domain.UnitMilligram || p0.Mass.ConvertedValue != 12.5 {
		t.Fatalf("mass conversion ug->mg: %+v", p0.Mass)
	}
	if d := p0.Temp.ConvertedValue - 30; d > 0.01 || d < -0.01 {
		t.Fatalf("K->degC: %+v", p0.Temp)
	}
}

func TestHalfFailureSiblingsSurvive(t *testing.T) {
	// drop the good program section, then supply a malformed one
	head := strings.SplitN(goodBundle, "# @layer program", 2)[0]
	txt := head + `# @layer program
t_start[s] t_end[s] temp_start[degC] temp_end[degC]
0 60 thirty thirty EXTRA
# @layer gas
t[s] from_gas to_gas flow[sccm]
0 none N2 50
`
	// rebuild remaining layers (blank, initial, drift, samples already present)
	txt += `# @layer blank
t[s] mass[mg]
0 0.02
# @layer initial
mass[mg] reason
12.5 "initial weighing"
`
	res, err := importer.Parse("p", "hf", []byte(txt))
	if err != nil {
		t.Fatalf("parse must not hard-fail: %v", err)
	}
	if len(res.Rejected) != 1 || res.Rejected[0].Layer != domain.LayerProgram {
		t.Fatalf("expected exactly program rejected, got %+v", res.Rejected)
	}
	if !contains(res.AcceptedLayers, domain.LayerSamples) || !contains(res.AcceptedLayers, domain.LayerGasEvents) {
		t.Fatal("sibling layers must still land")
	}
	if len(res.Data.Program) != 0 {
		t.Fatal("rejected layer rows must be cleared")
	}
}

func TestUnconvertibleValueRetained(t *testing.T) {
	txt := strings.Replace(goodBundle, "60 12.1 40", "60 NA 40", 1)
	res, err := importer.Parse("p", "diag", []byte(txt))
	if err != nil {
		t.Fatal(err)
	}
	p := res.Data.Samples[1]
	if p.Mass.Raw != "NA" || p.Mass.Diagnostic == "" {
		t.Fatalf("bad value must remain in place with diagnostic: %+v", p.Mass)
	}
	if !contains(res.AcceptedLayers, domain.LayerSamples) {
		t.Fatal("layer with one bad value is still accepted (row-level diagnostic)")
	}
}

func TestDataBeforeSectionIsHardError(t *testing.T) {
	if _, err := importer.Parse("p", "x", []byte("1 2 3\n")); err == nil {
		t.Fatal("expected format error for data before @layer")
	}
}

func TestStableIDsOnReParse(t *testing.T) {
	a, _ := importer.Parse("p", "src", []byte(goodBundle))
	b, _ := importer.Parse("p", "src", []byte(goodBundle))
	if a.Data.Samples[0].ID != b.Data.Samples[0].ID {
		t.Fatalf("ids must be stable across reparses: %s != %s", a.Data.Samples[0].ID, b.Data.Samples[0].ID)
	}
	c, _ := importer.Parse("other", "src", []byte(goodBundle))
	if a.Data.Samples[0].ID == c.Data.Samples[0].ID {
		t.Fatal("ids must depend on project/source")
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
