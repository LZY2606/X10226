package engine

import (
	"fmt"

	"tga-review/internal/domain"
)

// Batch is one deterministic run of the full derivation chain. All nodes share a
// batch id; changing a single source produces a new batch and only nodes
// downstream of that source acquire new identities — upstream nodes keep theirs.
type Batch struct {
	ID     string
	Params domain.Params
	Nodes  []*domain.DerivedNode
}

func fp(algo string, p domain.Params) domain.Fingerprint {
	return domain.Fingerprint{Algorithm: algo, Version: domain.AlgoVersion, Params: p.CanonicalJSON()}
}

// Run executes the chain against a snapshot of accepted source versions.
// versions maps layer -> newest accepted SourceVersion.
func Run(projectID, batchID string, p domain.Params, versions map[string]*domain.SourceVersion) (*Batch, error) {
	samples := versions[domain.LayerSamples]
	if samples == nil {
		return nil, fmt.Errorf("missing required layer: samples")
	}
	b := &Batch{ID: batchID, Params: p}
	add := func(n *domain.DerivedNode, e error) (*domain.DerivedNode, error) {
		if e != nil {
			return nil, e
		}
		n.ParamIDs = []string{p.ID}
		b.Nodes = append(b.Nodes, n)
		return n, nil
	}

	norm, err := add(NormalizeSeries(projectID, batchID, fp(domain.AlgoNormalize, p), samples.ID, samples.Samples))
	if err != nil {
		return nil, err
	}
	gapNode, err := add(DetectGaps(projectID, batchID, fp(domain.AlgoGap, p), norm, p))
	if err != nil {
		return nil, err
	}
	corrected, err := add(ApplyCorrections(projectID, batchID, fp(domain.AlgoCorrect, p), norm,
		versions[domain.LayerDrift], versions[domain.LayerBlank], p))
	if err != nil {
		return nil, err
	}
	dtgNode, err := add(BuildDTG(projectID, batchID, fp(domain.AlgoDTG, p), corrected, gapNode, p))
	if err != nil {
		return nil, err
	}
	windowsNode, werr := ReactionWindowsBuild(projectID, batchID, fp(domain.AlgoWindow, p),
		versions[domain.LayerGas], versions[domain.LayerProgram], p)
	if werr == nil {
		windowsNode.ParamIDs = []string{p.ID}
		b.Nodes = append(b.Nodes, windowsNode)
	}
	peaksNode, err := add(FindPeaks(projectID, batchID, fp(domain.AlgoPeak, p), dtgNode, p))
	if err != nil {
		return nil, err
	}

	initNode, ierr := SelectInitMass(projectID, batchID, fp(domain.AlgoInitMass, p), versions[domain.LayerInitMass], p)
	if ierr != nil {
		return nil, ierr
	}
	initNode.ParamIDs = []string{p.ID}
	b.Nodes = append(b.Nodes, initNode)
	initMg, _, _, err := ChosenMass(initNode)
	if err != nil {
		return nil, err
	}

	stageNode, err := add(BuildStages(projectID, batchID, fp(domain.AlgoStage, p), corrected, dtgNode, peaksNode, windowsNode, gapNode, initMg, p))
	if err != nil {
		return nil, err
	}
	residueNode, err := add(BuildResidue(projectID, batchID, fp(domain.AlgoResidue, p), corrected, gapNode, initNode, stageNode))
	if err != nil {
		return nil, err
	}
	_ = residueNode
	return b, nil
}
