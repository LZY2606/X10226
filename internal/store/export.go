package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"tga-review/internal/domain"
)

// ExportBundle is the self-contained, identity-preserving archive. IDs, ordering
// (event seq), source parent relations and derived provenance survive an
// export/delete/import round trip verbatim.
type ExportBundle struct {
	Bundle      string         `json:"bundle"`
	Format      string         `json:"format"`
	Schema      int            `json:"schema"`
	ExportSHA   string         `json:"export_sha"`
	Project     *domain.Project `json:"project"`
	Events      []domain.Event `json:"events"`
	Params      []domain.Params `json:"params"`
	Versions    []*domain.SourceVersion `json:"versions"`
	Derived     []*domain.DerivedNode  `json:"derived"`
	Diagnostics []domain.Diagnostic    `json:"diagnostics"`
}

func (a *App) Export(ctx context.Context, projectID string) (*ExportBundle, error) {
	p, err := a.Store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("project not found")
	}
	events, err := a.Store.LoadEvents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	b := &ExportBundle{Bundle: "tga-review-export", Format: "json", Schema: 1, Project: p, Events: events}
	// params
	pids := map[string]bool{}
	for _, e := range events {
		if e.Type == domain.EvParamsRevised {
			var pr domain.ParamsRevised
			if json.Unmarshal(e.Payload, &pr) == nil {
				pids[pr.ParamsID] = true
			}
		}
	}
	for id := range pids {
		if p, err := a.Store.LoadParams(ctx, id); err == nil && p != nil {
			b.Params = append(b.Params, *p)
		}
	}
	for _, layer := range domain.LayerOrder {
		vers, err := a.Store.ListVersions(ctx, projectID, layer)
		if err != nil {
			return nil, err
		}
		b.Versions = append(b.Versions, vers...)
	}
	// all derived nodes
	st, err := a.HydratedState(ctx, projectID, 0)
	if err != nil {
		return nil, err
	}
	for _, bm := range st.Batches {
		nodes, err := a.Store.LoadDerivedByBatch(ctx, projectID, bm.ID)
		if err != nil {
			return nil, err
		}
		b.Derived = append(b.Derived, nodes...)
	}
	b.Diagnostics, _ = a.Store.ListDiagnostics(ctx, projectID)
	b.ExportSHA = bundleSHA(b)
	return b, nil
}

func bundleSHA(b *ExportBundle) string {
	type core struct {
		P  *domain.Project
		E  []domain.Event
		Pa []domain.Params
		V  []*domain.SourceVersion
		D  []*domain.DerivedNode
	}
	raw, _ := json.Marshal(core{b.Project, b.Events, b.Params, b.Versions, b.Derived})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// ImportBundle restores a bundle into a (possibly empty) database. Stable IDs,
// event sequences and source/derived relationships are preserved. It fails
// closed on schema mismatch or checksum error.
func (a *App) ImportBundle(ctx context.Context, b *ExportBundle, originNote string) (string, error) {
	if b.Bundle != "tga-review-export" || b.Schema != 1 {
		return "", fmt.Errorf("unsupported bundle: bundle=%q schema=%d", b.Bundle, b.Schema)
	}
	if got := bundleSHA(b); got != b.ExportSHA {
		return "", fmt.Errorf("export checksum mismatch: want %s got %s", b.ExportSHA, got)
	}
	pid := b.Project.ID
	if existing, err := a.Store.GetProject(ctx, pid); err != nil {
		return "", err
	} else if existing != nil {
		return "", fmt.Errorf("project %s already present; refusing to overwrite", pid)
	}
	err := a.Store.Tx(ctx, func(tx execTx) error {
		if e := a.Store.CreateProjectTx(ctx, tx, b.Project); e != nil {
			return e
		}
		// insert events verbatim with original seq/id/parent
		for i := range b.Events {
			e := b.Events[i]
			body, _ := json.Marshal(e.Payload)
			if _, e2 := tx.ExecContext(ctx,
				`INSERT INTO events(project_id,seq,id,type,at,actor,parent_id,payload,undone_by) VALUES(?,?,?,?,?,?,?,?,?)`,
				e.ProjectID, e.Seq, e.ID, e.Type, e.At, e.Actor, e.ParentID, string(body), e.UndoneBy); e2 != nil {
				return e2
			}
		}
		for i := range b.Params {
			if e := a.Store.SaveParams(ctx, tx, b.Params[i]); e != nil {
				return e
			}
		}
		for _, v := range b.Versions {
			if e := a.Store.SaveVersion(ctx, tx, v); e != nil {
				return e
			}
		}
		if e := a.Store.AddDiagnostics(ctx, tx, pid, b.Diagnostics); e != nil {
			return e
		}
		for _, n := range b.Derived {
			if e := a.Store.SaveDerived(ctx, tx, n, n.ProducedEventID); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	// append a marker event for provenance of the restore
	_ = a.Store.Tx(ctx, func(tx execTx) error {
		body, _ := json.Marshal(domain.ProjectImported{OriginProjectID: pid, ExportSHA: b.ExportSHA})
		_, e := tx.ExecContext(ctx,
			`INSERT INTO events(project_id,seq,id,type,at,actor,parent_id,payload,undone_by) VALUES(?,?,?,?,?,?,?,?,?)`,
			pid, int64(len(b.Events))+1, randID(domain.PrefixEvent), domain.EvProjectImported, a.now(), "system", "", string(body), "")
		return e
	})
	return pid, nil
}
