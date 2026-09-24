package domain

import "sort"

type Decision struct {
	ManualDecision
	ConflictWith string
	ResolvedBy   string
	Undone       bool
	UndoneBy     string
}

type State struct {
	ProjectID string
	Seq       int64
	Raw       []Record
	Batches   []Batch
	Params    []Params
	Decisions []Decision
	Events    []Event
}

func (s *State) CurrentParams() Params {
	if len(s.Params) == 0 {
		return DefaultParams()
	}
	return s.Params[len(s.Params)-1]
}

func (s *State) ActiveRecords() []Record {
	byOriginal := map[string]int{}
	var out []Record
	for _, rec := range s.Raw {
		if !rec.Valid {
			continue
		}
		if rec.Supersedes != "" {
			if idx, ok := byOriginal[rec.Supersedes]; ok {
				out[idx] = rec
				byOriginal[rec.ID] = idx
				continue
			}
		}
		byOriginal[rec.ID] = len(out)
		out = append(out, rec)
	}
	return out
}

func (s *State) ActiveDecisions() []Decision {
	return append([]Decision(nil), s.Decisions...)
}

func (s *State) SourceSetHash() string {
	recs := s.ActiveRecords()
	ids := make([]string, 0, len(recs))
	for _, rec := range recs {
		ids = append(ids, rec.ID+"="+stableRecordText(rec))
	}
	return StableHash(stringsJoin(ids, "\n"))
}

func stringsJoin(vals []string, sep string) string {
	out := ""
	for i, v := range vals {
		if i > 0 {
			out += sep
		}
		out += v
	}
	return out
}

func stableRecordText(rec Record) string {
	keys := make([]string, 0, len(rec.Extras))
	for k := range rec.Extras {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	extra := ""
	for _, k := range keys {
		extra += k + "=" + rec.Extras[k] + ";"
	}
	return stringsJoin([]string{
		rec.Layer, rec.Time.Raw, rec.Time.Unit, rec.Temperature.Raw, rec.Temperature.Unit,
		rec.Mass.Raw, rec.Mass.Unit, rec.InitialMass.Raw, rec.InitialMass.Unit,
		rec.Drift.Raw, rec.Drift.Unit, rec.Gas, rec.Segment, rec.TargetTemp.Raw, rec.RampRate.Raw,
		rec.Note, rec.RawText, extra, rec.Supersedes,
	}, "|")
}

func AnalysisID(projectID string, params Params, sourceHash string) string {
	return StableHash(projectID + "|" + AlgorithmVersion + "|" + params.Fingerprint + "|" + sourceHash)
}
