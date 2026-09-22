package app

import (
	"tga-review/internal/importer"
)

// parseExisting re-parses an archived batch from its original bytes. Because
// ids are a function of project, source and payload, the regenerated rows
// match the archived batch exactly.
func parseExisting(projectID string, bp BatchPayload) (*importer.Result, error) {
	return importer.Parse(projectID, bp.Batch.SourceRef, []byte(bp.Source))
}
