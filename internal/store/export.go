package store

import (
	"context"

	"tga-review/internal/domain"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Bundle is the full project archive. Identity ids are content addressed and
// sequence counters are embedded, so deleting the database and importing this
// file restores ids, ordering and provenance exactly.
type Bundle struct {
	Schema     string         `json:"schema"`
	Version    int            `json:"version"`
	Project    Project        `json:"project"`
	Counters   map[string]int `json:"counters"`
	Events     []domainEvent  `json:"events"`
	Sources    []domain.Source `json:"sources"`
	Sessions   []domain.Session `json:"sessions"`
	Params     []paramRow     `json:"params"`
	Nodes      []domain.Node  `json:"nodes"`
	Decisions  []domain.Decision `json:"decisions"`
	Conflicts  []domain.Conflict `json:"conflicts"`
	Archive    []domain.ArchiveEntry `json:"archive"`
	Checksums  map[string]string `json:"checksums"`
}

type domainEvent = eventAlias
type eventAlias struct {
	Seq       int            `json:"seq"`
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt string         `json:"created_at"`
}

type paramRow struct {
	Version   int    `json:"version"`
	FP        string `json:"fingerprint"`
	ParentFP  string `json:"parent_fp"`
	JSON      string `json:"json"`
	CreatedAt string `json:"created_at"`
}

func checksum(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ExportBundle reads a complete, consistent snapshot of one project.
func (s *Store) ExportBundle(ctx context.Context, projectID string) (*Bundle, error) {
	p, err := s.GetProject(ctx, projectID)
	if err != nil || p == nil {
		return nil, err
	}
	evs, err := s.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	srcs, err := s.AllSources(ctx, projectID)
	if err != nil {
		return nil, err
	}
	ses, err := s.Sessions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.AllNodes(ctx, projectID)
	if err != nil {
		return nil, err
	}
	decs, err := s.AllDecisions(ctx, projectID)
	if err != nil {
		return nil, err
	}
	confs, err := s.AllConflicts(ctx, projectID)
	if err != nil {
		return nil, err
	}
	arch, err := s.ArchiveEntries(ctx, projectID)
	if err != nil {
		return nil, err
	}
	counters := map[string]int{}
	rows, err := s.db.QueryContext(ctx, "SELECT name,value FROM counters WHERE project_id=?", projectID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		var v int
		if err := rows.Scan(&n, &v); err != nil {
			rows.Close()
			return nil, err
		}
		counters[n] = v
	}
	rows.Close()

	prows := []paramRow{}
	prowsRows, err := s.db.QueryContext(ctx,
		"SELECT version,fingerprint,COALESCE(parent_fp,''),json,created_at FROM params_versions WHERE project_id=? ORDER BY version", projectID)
	if err != nil {
		return nil, err
	}
	for prowsRows.Next() {
		var pr paramRow
		if err := prowsRows.Scan(&pr.Version, &pr.FP, &pr.ParentFP, &pr.JSON, &pr.CreatedAt); err != nil {
			prowsRows.Close()
			return nil, err
		}
		prows = append(prows, pr)
	}
	prowsRows.Close()

	evOut := make([]eventAlias, len(evs))
	for i, e := range evs {
		evOut[i] = eventAlias{Seq: e.Seq, ID: e.ID, Type: e.Type, Payload: e.Payload, CreatedAt: e.CreatedAt}
	}
	b := &Bundle{
		Schema:    "tga-review/project",
		Version:   1,
		Project:   *p,
		Counters:  counters,
		Events:    evOut,
		Sources:   srcs,
		Sessions:  ses,
		Params:    prows,
		Nodes:     nodes,
		Decisions: decs,
		Conflicts: confs,
		Archive:   arch,
	}
	b.Checksums = map[string]string{
		"events":    checksum(b.Events),
		"sources":   checksum(b.Sources),
		"sessions":  checksum(b.Sessions),
		"params":    checksum(b.Params),
		"nodes":     checksum(b.Nodes),
		"decisions": checksum(b.Decisions),
		"conflicts": checksum(b.Conflicts),
		"archive":   checksum(b.Archive),
	}
	return b, nil
}

// VerifyChecksums confirms the bundle was not altered.
func (b *Bundle) VerifyChecksums() error {
	checks := map[string]any{
		"events":    b.Events,
		"sources":   b.Sources,
		"sessions":  b.Sessions,
		"params":    b.Params,
		"nodes":     b.Nodes,
		"decisions": b.Decisions,
		"conflicts": b.Conflicts,
		"archive":   b.Archive,
	}
	for k, v := range checks {
		want, ok := b.Checksums[k]
		if !ok {
			return errf("bundle missing checksum for %s", k)
		}
		if got := checksum(v); got != want {
			return errf("bundle checksum mismatch for %s", k)
		}
	}
	return nil
}
