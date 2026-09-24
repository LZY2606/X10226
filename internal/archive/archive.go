// Package archive implements the offline "tga-lineage v1" case package:
// export/delete/reimport preserves stable ids, ordering and source relations.
package archive

import (
	"bytes"
	"encoding/json"
	"fmt"

	"tgaline/internal/domain"
)

const FormatVersion = "tga-lineage-v1"

// Archive is the self-contained, machine-readable lineage bundle.
type Archive struct {
	Format  string         `json:"format"`
	Project ProjectBlock   `json:"project"`
	Events  []*domain.Event `json:"events"`
	Layers  []*domain.Layer `json:"layers"`
	Nodes   []*domain.Node  `json:"nodes"`
	SHA     string         `json:"sha"`
}

type ProjectBlock struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// Source is the read boundary satisfied by internal/store.
type Source interface {
	LoadEvents(projectID string) ([]*domain.Event, error)
	AllLayers() ([]*domain.Layer, error)
	AllNodes() ([]*domain.Node, error)
}

// Sink is the write boundary satisfied by internal/store.
type Sink interface {
	CreateProject(id, name, createdAt, metaJSON string) error
	InsertEventRaw(ev *domain.Event, payloadTaggedJSON []byte) error
	InsertLayerRaw(l *domain.Layer) error
	InsertNodeRaw(n *domain.Node) error
}

// Export collects one project's events plus the immutable layers and nodes
// they reference. Layers/nodes are global content-addressed artifacts; all are
// included so a fresh database can replay without recomputation.
func Export(pb ProjectBlock, st Source) (*Archive, error) {
	events, err := st.LoadEvents(pb.ID)
	if err != nil {
		return nil, err
	}
	layers, err := st.AllLayers()
	if err != nil {
		return nil, err
	}
	nodes, err := st.AllNodes()
	if err != nil {
		return nil, err
	}
	// narrow layers to this project; nodes are shared but referenced by id
	var pl []*domain.Layer
	for _, l := range layers {
		if l.ProjectID == pb.ID {
			pl = append(pl, l)
		}
	}
	a := &Archive{
		Format: FormatVersion, Project: pb,
		Events: events, Layers: pl, Nodes: nodes,
	}
	sha, err := contentSHA(a)
	if err != nil {
		return nil, err
	}
	a.SHA = sha
	return a, nil
}

func contentSHA(a *Archive) (string, error) {
	copy := *a
	copy.SHA = ""
	h, err := domain.Hash(copy)
	if err != nil {
		return "", err
	}
	return "sha256:" + h, nil
}

func Marshal(a *Archive) ([]byte, error) {
	return json.MarshalIndent(a, "", "  ")
}

func Unmarshal(b []byte) (*Archive, error) {
	var a Archive
	if err := json.Unmarshal(b, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

// Verify recomputes the bundle hash and the event hash chain, and checks that
// every source reference resolves. It does not trust embedded ids blindly.
func (a *Archive) Verify() error {
	if a.Format != FormatVersion {
		return fmt.Errorf("archive: unexpected format %q", a.Format)
	}
	got, err := contentSHA(a)
	if err != nil {
		return err
	}
	if got != a.SHA {
		return fmt.Errorf("archive: outer sha mismatch (%s vs %s)", got, a.SHA)
	}
	// event ordering + hash chain
	layerIDs := map[string]bool{}
	for _, l := range a.Layers {
		if layerIDs[l.ID] {
			return fmt.Errorf("archive: duplicate layer %s", l.ID)
		}
		layerIDs[l.ID] = true
	}
	nodeIDs := map[string]bool{}
	for _, n := range a.Nodes {
		if nodeIDs[n.ID] {
			return fmt.Errorf("archive: duplicate node %s", n.ID)
		}
		nodeIDs[n.ID] = true
	}
	var parent string
	for i, ev := range a.Events {
		if ev.Seq != int64(i+1) {
			return fmt.Errorf("archive: event seq gap at %d", i+1)
		}
		if ev.ParentID != parent {
			return fmt.Errorf("archive: broken parent chain at seq %d", ev.Seq)
		}
		want, err := domain.Hash([]any{"event", ev.Seq, string(ev.Type), ev.At, ev.Actor, ev.ParentID, ev.Payload})
		if err != nil {
			return err
		}
		if ev.ID != "ev"+want[:24] {
			return fmt.Errorf("archive: event id mismatch at seq %d", ev.Seq)
		}
		parent = ev.ID
		// layer refs resolve
		if ev.Type == domain.EvLayerCommitted {
			if pl, ok := ev.Payload.(domain.LayerCommitPayload); ok {
				if !layerIDs[pl.LayerID] {
					return fmt.Errorf("archive: event %d references missing layer %s", ev.Seq, pl.LayerID)
				}
			}
		}
	}
	// node sources resolve
	for _, n := range a.Nodes {
		for _, ref := range n.DirectSources {
			switch ref.Kind {
			case "layer":
				if !layerIDs[ref.ID] {
					return fmt.Errorf("archive: node %s references missing layer %s", n.ID, ref.ID)
				}
			case "node":
				if !nodeIDs[ref.ID] {
					return fmt.Errorf("archive: node %s references missing node %s", n.ID, ref.ID)
				}
			case "params", "event":
				// fingerprints / event ids are not material nodes here
			default:
				return fmt.Errorf("archive: node %s unknown source kind %q", n.ID, ref.Kind)
			}
		}
		// recompute node id
		wantID := domain.ShortID("n_"+string(n.Kind)+"_",
			[]any{string(n.Kind), n.AlgoVersion, n.ParamsFP, n.DirectSources, jsonRaw(n.Body)})
		if wantID != n.ID {
			return fmt.Errorf("archive: node %s fails content-address check", n.ID)
		}
	}
	return nil
}

// jsonRaw re-decodes body JSON so it hashes identically to the in-memory form.
func jsonRaw(body string) any {
	var v any
	dec := json.NewDecoder(bytes.NewReader([]byte(body)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return body
	}
	return v
}
