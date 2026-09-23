package store

import (
	"encoding/json"

	"tgalineage/internal/model"
)

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func jsonOrEmpty(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanInput(r rowScanner) (model.RawInput, error) {
	var in model.RawInput
	var valid, seed int
	var parsed, diags string
	err := r.Scan(&in.ID, &in.ProjectID, &in.Kind, &in.Version, &in.ParentVersion,
		&in.ContentType, &in.RawText, &in.ImportBatch, &valid, &seed, &parsed, &diags,
		&in.CreatedAt)
	if err != nil {
		return in, err
	}
	in.Valid = valid != 0
	in.Seed = int64(seed)
	in.Parsed = json.RawMessage(parsed)
	if json.Valid([]byte(diags)) {
		_ = json.Unmarshal([]byte(diags), &in.Diagnostics)
	}
	return in, nil
}

func scanRun(r rowScanner) (model.Run, error) {
	var run model.Run
	var cur int
	var srcs, diags, payload string
	err := r.Scan(&run.ID, &run.ProjectID, &run.AlgoVersion, &run.ParamFingerprint,
		&srcs, &cur, &diags, &payload, &run.CreatedAt)
	if err != nil {
		return run, err
	}
	run.Current = cur != 0
	_ = json.Unmarshal([]byte(srcs), &run.Sources)
	if json.Valid([]byte(diags)) {
		_ = json.Unmarshal([]byte(diags), &run.Diagnostics)
	}
	run.Payload = json.RawMessage(payload)
	return run, nil
}

func scanCandidate(r rowScanner) (model.Candidate, error) {
	var c model.Candidate
	var detail string
	err := r.Scan(&c.ID, &c.ProjectID, &c.RunID, &c.Type, &c.AltGroup, &c.ParentID,
		&c.Status, &c.Summary, &detail, &c.CreatedAt)
	if err != nil {
		return c, err
	}
	c.Detail = json.RawMessage(detail)
	return c, nil
}

func scanEvent(r rowScanner) (model.Event, error) {
	var e model.Event
	var undone int
	var payload string
	err := r.Scan(&e.ProjectID, &e.Seq, &e.ID, &e.Type, &undone, &e.Undoes, &e.Actor,
		&payload, &e.CreatedAt)
	if err != nil {
		return e, err
	}
	e.Undone = undone != 0
	e.Payload = json.RawMessage(payload)
	return e, nil
}
