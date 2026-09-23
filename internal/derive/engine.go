package derive

import (
	"encoding/json"

	"tgaline/internal/domain"
)

// Inputs is the frozen set of direct sources for one derivation run.
type Inputs struct {
	Samples    *domain.Document
	Drift      *domain.Document
	Furnace    *domain.Document
	Gas        *domain.Document
	Blank      *domain.Document
	InitialMass *domain.Document
	Params     domain.Parameters
}

// Graph is the complete result of a derivation run.
type Graph struct {
	Params     domain.Parameters
	ParamsFP   string
	Artifacts  []*domain.Artifact
	Diagnostics []domain.Diagnostic
	InitialMassMg float64
	InitialMassStatus string
	InitialMassChain []domain.ChainNode
	Gaps       [][2]float64
}

func (g *Graph) byID() map[string]*domain.Artifact {
	m := map[string]*domain.Artifact{}
	for _, a := range g.Artifacts {
		m[a.ID] = a
	}
	return m
}

func paramsMap(p domain.Parameters) map[string]any {
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func prov(docs []*domain.Document, params domain.Parameters, fp, inputsHash string, deps ...string) domain.Provenance {
	pr := domain.Provenance{
		ParamsFP: fp, AlgoVersion: params.AlgorithmVersion,
		DependsOn: deps, InputsHash: inputsHash, DocVersions: map[string]int{},
	}
	for _, d := range docs {
		if d == nil {
			continue
		}
		pr.DocIDs = append(pr.DocIDs, d.ID)
		pr.DocVersions[d.ID] = d.Version
	}
	return pr
}

func newArtifact(id, projectID string, t domain.ArtifactType, status string,
	data map[string]any, pr domain.Provenance, diags []domain.Diagnostic) *domain.Artifact {
	return &domain.Artifact{
		ID: id, ProjectID: projectID, Type: t, Status: status,
		Data: data, Prov: pr, Diagnostics: diags,
	}
}

// Run executes the complete pipeline. It never panics on missing inputs; an
// unavailable input yields an unknown/partial artifact with diagnostics so
// the review UI can show exactly why a number cannot be drawn.
func Run(projectID string, in Inputs, atSeq int64) *Graph {
	g := &Graph{Params: in.Params, ParamsFP: ParamsFingerprint(paramsMap(in.Params))}
	if in.Samples == nil {
		g.Diagnostics = append(g.Diagnostics, domain.Diagnostic{
			Scope: "batch", Code: "no_samples",
			Message: "no accepted raw samples document; derivation cannot start",
			Severity: domain.SeverityError,
		})
		return g
	}
	corr := buildCorrected(projectID, in, g)
	g.Artifacts = append(g.Artifacts, corr)
	tg := buildTG(projectID, in, g, corr)
	g.Artifacts = append(g.Artifacts, tg)
	dtg := buildDTG(projectID, in, g, tg)
	g.Artifacts = append(g.Artifacts, dtg)
	gas := buildGasTimeline(projectID, in, g)
	g.Artifacts = append(g.Artifacts, gas)
	furn := buildFurnaceTimeline(projectID, in, g)
	g.Artifacts = append(g.Artifacts, furn)
	if in.Blank != nil {
		g.Artifacts = append(g.Artifacts, buildBlank(projectID, in, g))
	}
	stages := buildStages(projectID, in, g, dtg, gas)
	g.Artifacts = append(g.Artifacts, stages)
	res := buildResidue(projectID, in, g, tg, stages)
	g.Artifacts = append(g.Artifacts, res)
	for _, a := range g.Artifacts {
		a.AtEventSeq = atSeq
	}
	return g
}

func asPoints(a *domain.Artifact) []domain.Point {
	raw, _ := a.Data["points"].([]any)
	out := make([]domain.Point, 0, len(raw))
	for _, v := range raw {
		if mm, ok := v.(map[string]any); ok {
			out = append(out, pointFromMap(mm))
		}
	}
	return out
}

func pointFromMap(m map[string]any) domain.Point {
	p := domain.Point{}
	p.T, _ = m["t_s"].(float64)
	p.TempC, _ = m["temp_c"].(float64)
	p.MassMg, _ = m["mass_mg"].(float64)
	p.MassPct, _ = m["mass_pct"].(float64)
	p.DTGMgPerMin, _ = m["dtg_mg_per_min"].(float64)
	p.GapBefore, _ = m["gap_before"].(bool)
	return p
}
