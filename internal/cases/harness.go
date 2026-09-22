package cases

import (
	"fmt"

	"tga-review/internal/domain"
	"tga-review/internal/ingest"
	"tga-review/internal/store"
)

// VariantDiag collects surfaced import diagnostics for verification.
type VariantDiag struct {
	Layer string
	Line  int
	Code  string
	Msg   string
}

// ImportVariant creates a project from one packed variant, importing every
// stratum in canonical order and tagging non-base variants with the mutation
// name so the counterexample lineage is machine-readable.
func ImportVariant(s *store.Store, root, variant, projectName string) (string, []VariantDiag, error) {
	files, err := LoadDir(VariantDir(root, variant))
	if err != nil {
		return "", nil, err
	}
	material := "分子筛负载金属氧化物（离线样例/" + variant + "）"
	proj, err := s.CreateProject(projectName, material, "失重谱系案例包："+variant)
	if err != nil {
		return "", nil, err
	}
	var diags []VariantDiag
	for _, f := range files {
		pr := ingest.Parse(f)
		if pr.Fatal != nil {
			return proj.ID, diags, fmt.Errorf("%s: %w", f.Name, pr.Fatal)
		}
		sub := pr.Sub
		if variant != "base" {
			sub.Mutation = variant
		}
		got, _, err := s.AddSubmission(proj.ID, sub, true)
		if err != nil {
			return proj.ID, diags, err
		}
		for _, d := range got.Diags {
			diags = append(diags, VariantDiag{Layer: d.Layer, Line: d.Line, Code: d.Code, Msg: d.Message})
		}
	}
	return proj.ID, diags, nil
}

// MustDefaultParams is a convenience for tests.
func MustDefaultParams() domain.Params { return domain.DefaultParams() }
