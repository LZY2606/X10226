// Package archive exports and imports the complete event log of a project.
//
// Because project and event ids are content-hash chained and every derived id
// is a pure function of direct-source event seqs + parameter fingerprint,
// deleting the local database and importing the same archive restores
// identities, ordering and provenance relations bit-for-bit.
package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"tga/internal/model"
	"tga/internal/store"
)

// FormatVersion is the archive schema version.
const FormatVersion = 1

// File is the JSON manifest inside the zip.
type File struct {
	FormatVersion int            `json:"format_version"`
	Kind          string         `json:"kind"`
	Project       model.Project  `json:"project"`
	Events        []*model.Event `json:"events"`
}

// Export writes a tga-review archive (zip with manifest.json + README).
func Export(ctx context.Context, st *store.Store, projectID string, w io.Writer) error {
	p, err := st.GetProject(ctx, projectID)
	if err != nil {
		return err
	}
	events, err := st.Events(ctx, projectID)
	if err != nil {
		return err
	}
	f := File{FormatVersion: FormatVersion, Kind: "tga-review-archive", Project: *p, Events: events}
	manifest, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	mw, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if _, err := mw.Write(manifest); err != nil {
		return err
	}
	rw, err := zw.Create("README.txt")
	if err != nil {
		return err
	}
	fmt.Fprintf(rw, "TGA review archive\nproject: %s (%s)\nevents: %d\nformat: v%d\nImporting this file into an empty database restores all stable ids, ordering and provenance.\n",
		p.Name, p.ID, len(events), FormatVersion)
	return zw.Close()
}

// Import reads an archive and inserts it (idempotent: existing project errors
// unless allowReplay is set, in which case events are returned only).
func Import(ctx context.Context, st *store.Store, r io.Reader) (*File, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("not a tga archive (zip): %w", err)
	}
	var f File
	found := false
	for _, fz := range zr.File {
		if fz.Name == "manifest.json" {
			rc, err := fz.Open()
			if err != nil {
				return nil, err
			}
			dec := json.NewDecoder(rc)
			if err := dec.Decode(&f); err != nil {
				_ = rc.Close()
				return nil, err
			}
			_ = rc.Close()
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("archive missing manifest.json")
	}
	if f.FormatVersion != FormatVersion || f.Kind != "tga-review-archive" {
		return nil, fmt.Errorf("unsupported archive format v%q", f.Kind)
	}
	if err := validateChain(&f); err != nil {
		return nil, err
	}
	if existing, err := st.GetProject(ctx, f.Project.ID); err == nil && existing != nil {
		return nil, fmt.Errorf("project %s already exists in this database", f.Project.ID)
	}
	if err := st.InsertEvents(ctx, f.Project, f.Events); err != nil {
		return nil, err
	}
	return &f, nil
}

// validateChain checks seq order and prev-id chaining.
func validateChain(f *File) error {
	var prevID string
	for i, ev := range f.Events {
		wantSeq := int64(i + 1)
		if ev.Seq != wantSeq {
			return fmt.Errorf("event order mismatch at %d: seq=%d", i, ev.Seq)
		}
		if ev.ProjectID != f.Project.ID {
			return fmt.Errorf("event %d project id mismatch", ev.Seq)
		}
		if ev.PrevID != prevID {
			return fmt.Errorf("event %d prev-id chain broken", ev.Seq)
		}
		prevID = ev.ID
	}
	return nil
}
