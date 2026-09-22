// Package importer accepts small copyable samples and explicit format
// errors. Valid files are layered into immutable source versions; failures
// are recorded as diagnostics and never silently dropped. A bundle with
// some bad files imports partially and records an import.partial_failed
// event.
package importer

import (
	"fmt"
	"sort"
	"strings"

	"tgaview/internal/core"
	"tgaview/internal/store"
	"tgaview/internal/tga"
)

// Bundle is a named set of raw files (filename -> raw content).
type Bundle struct {
	Name  string
	Files map[string]string
}

// knownKinds maps source kind to a validator for its raw format.
var knownKinds = []string{
	core.SrcSamplePoints,
	core.SrcDriftCorrection,
	core.SrcFurnaceProgram,
	core.SrcGasValveEvents,
	core.SrcBlankCurve,
	core.SrcInitialMass,
}

// kindForFile maps a file name like "sample_points.csv" to a source kind.
func kindForFile(name string) (string, bool) {
	base := strings.TrimSuffix(strings.TrimSpace(name), ".csv")
	for _, k := range knownKinds {
		if base == k {
			return k, true
		}
	}
	return "", false
}

// validate parses the raw content according to its kind, returning a
// precise error on format problems.
func validate(kind, raw string) error {
	switch kind {
	case core.SrcSamplePoints:
		_, _, _, err := tga.ParseCSV(raw, kind, map[string]string{"time": "time", "temp": "temp", "mass": "mass"})
		return err
	case core.SrcBlankCurve:
		_, _, _, err := tga.ParseCSV(raw, kind, map[string]string{"temp": "temp", "mass": "mass"})
		return err
	case core.SrcFurnaceProgram:
		_, _, _, err := tga.ParseCSV(raw, kind, map[string]string{"time": "time", "setpoint": "temp"})
		return err
	case core.SrcDriftCorrection:
		_, _, err := core.ParseDriftRate(raw)
		return err
	case core.SrcInitialMass:
		_, _, err := core.ParseInitialMass(raw)
		return err
	case core.SrcGasValveEvents:
		_, err := core.ParseValveEvents(raw)
		return err
	}
	return fmt.Errorf("unknown source kind %q", kind)
}

// ImportBundle validates each file and appends source events. Files whose
// kind already exists become new versions with parent links. Returns the
// recorded import payload. A partial import is not an error: it is
// recorded as an import.partial_failed event with per-file diagnostics.
func ImportBundle(st *store.Store, b Bundle, runID string) (*core.ImportPayload, error) {
	state, err := core.RebuildState(st, 0)
	if err != nil {
		return nil, err
	}
	payload := &core.ImportPayload{RunID: runID, Status: "ok"}
	names := make([]string, 0, len(b.Files))
	for n := range b.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := b.Files[name]
		kind, ok := kindForFile(name)
		if !ok {
			payload.Failures = append(payload.Failures, core.ImportFailure{
				File: name, Message: "文件名不对应任何已知来源类型，未导入",
			})
			continue
		}
		if err := validate(kind, raw); err != nil {
			payload.Failures = append(payload.Failures, core.ImportFailure{
				File: name, Message: err.Error(),
			})
			continue
		}
		payload.Files = append(payload.Files, name)
		version := 1
		parent := 0
		if src := state.Sources[kind]; src != nil {
			parent = src.Latest().Version
			version = parent + 1
		}
		kindEv := core.EvSourceSubmitted
		if parent > 0 {
			kindEv = core.EvSourceRevised
		}
		if _, err := st.Append(kindEv, kind, core.SourcePayload{
			SourceID: kind, Version: version, Parent: parent,
			Kind: kind, Raw: raw, Format: "csv+units",
			Note: fmt.Sprintf("imported from bundle %s", b.Name),
		}); err != nil {
			return nil, err
		}
	}
	if len(payload.Failures) > 0 {
		payload.Status = "partial"
		if _, err := st.Append(core.EvImportPartial, runID, payload); err != nil {
			return nil, err
		}
	} else {
		if _, err := st.Append(core.EvImportCompleted, runID, payload); err != nil {
			return nil, err
		}
	}
	return payload, nil
}
