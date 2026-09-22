package app

import (
	"encoding/json"

	"tga-review/internal/domain"
)

// View is the synchronized review payload: current artifacts, stale older
// versions, provenance and the event history.
type View struct {
	Project      domain.Project        `json:"project"`
	Artifacts    []domain.Artifact     `json:"artifacts"` // latest per family at seq
	Stale        []domain.Artifact     `json:"stale"`     // older versions, viewable
	AsOfSeq      int64                 `json:"as_of_seq"`
	LatestSeq    int64                 `json:"latest_seq"`
	Historical   bool                  `json:"historical"`
	Events       []domain.Event        `json:"events"`
	Records      []domain.SourceRecord `json:"records"`
	Diagnostics  []domain.Diagnostic   `json:"diagnostics,omitempty"`
	Quarantined  []domain.SourceRecord `json:"quarantined,omitempty"`
	ConflictOpen []string              `json:"conflict_open,omitempty"`
}

// CurrentView computes the state at the latest event seq.
func (s *Service) CurrentView(projectID string) (*View, error) {
	events, err := s.Store.ListEvents(projectID)
	if err != nil {
		return nil, err
	}
	var latest int64
	for _, e := range events {
		latest = e.Seq
	}
	return s.ViewAt(projectID, latest)
}

// ViewAt restores the visible conclusion at any historical event seq without
// overwriting history with newer computation.
func (s *Service) ViewAt(projectID string, seq int64) (*View, error) {
	if err := s.deriveProjectAt(projectID, seq); err != nil {
		return nil, err
	}
	p, err := s.Store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	events, err := s.Store.ListEvents(projectID)
	if err != nil {
		return nil, err
	}
	recs, err := s.Store.ListRecords(projectID)
	if err != nil {
		return nil, err
	}
	arts, err := s.Store.ListArtifacts(projectID)
	if err != nil {
		return nil, err
	}
	var latest int64
	for _, e := range events {
		latest = e.Seq
	}
	effectiveEvents := map[int64]bool{}
	for _, e := range events {
		effectiveEvents[e.Seq] = !isUndone(events, e.Seq, seq) && (seq == 0 || e.Seq <= seq)
	}

	current := map[string]domain.Artifact{}
	var stale []domain.Artifact
	// candidate-set family may contain decision versions; pick the newest with
	// created_seq <= seq, tie broken by id determinism
	for _, a := range arts {
		if a.CreatedSeq > seq && seq > 0 {
			continue
		}
		// artifacts derived after a source layer changed at seq X carry the new
		// source ids; they are simply absent when we query the older snapshot.
		exist, ok := current[a.Family]
		if !ok || a.CreatedSeq > exist.CreatedSeq ||
			(a.CreatedSeq == exist.CreatedSeq && a.ID > exist.ID) {
			if ok {
				stale = append(stale, exist)
			}
			current[a.Family] = a
		} else {
			stale = append(stale, a)
		}
	}
	v := &View{
		Project: p, Artifacts: make([]domain.Artifact, 0, len(current)),
		Stale: stale, AsOfSeq: seq, LatestSeq: latest, Historical: seq > 0 && seq < latest,
		Events: events, Records: recs,
	}
	for _, a := range current {
		v.Artifacts = append(v.Artifacts, a)
	}
	// quarantine list from records belonging to quarantined import events
	qSeqs := map[int64]bool{}
	for _, e := range events {
		if e.Type == domain.EvImportQuarantine {
			qSeqs[e.Seq] = true
		}
	}
	for _, r := range recs {
		if qSeqs[r.ImportSeq] || r.Status == "rejected" || r.Status == "unconvertible" {
			v.Quarantined = append(v.Quarantined, r)
		}
	}
	// open conflicts from the current candidate set
	if cs := candidateSetOf(current); cs != nil {
		acceptedByRegion := map[string]int{}
		for _, reg := range cs.Regions {
			for _, pr := range reg.Proposals {
				if pr.Status == domain.StatusAccepted || pr.Status == domain.StatusConflict {
					acceptedByRegion[reg.ID]++
				}
			}
			if acceptedByRegion[reg.ID] > 1 {
				v.ConflictOpen = append(v.ConflictOpen, reg.ID)
			}
		}
	}
	return v, nil
}

func candidateSetOf(m map[string]domain.Artifact) *domain.CandidateSet {
	for _, a := range m {
		if a.Kind == domain.ArtCandSet {
			var cs domain.CandidateSet
			_ = json.Unmarshal(a.Payload, &cs)
			return &cs
		}
	}
	return nil
}

// Trace returns source records, upstream artifacts and deciding events for one
// displayed value (artifact id or proposal id).
type Trace struct {
	Target   string                `json:"target"`
	Sources  []domain.SourceRecord `json:"sources"`
	Upstream []domain.Artifact     `json:"upstream"`
	Events   []domain.Event        `json:"events"`
}

func (s *Service) Trace(projectID, target string) (*Trace, error) {
	recs, _ := s.Store.ListRecords(projectID)
	arts, _ := s.Store.ListArtifacts(projectID)
	events, _ := s.Store.ListEvents(projectID)
	byID := map[string]domain.Artifact{}
	for _, a := range arts {
		byID[a.ID] = a
	}
	recByID := map[string]domain.SourceRecord{}
	for _, r := range recs {
		recByID[r.ID] = r
	}
	t := &Trace{Target: target}
	visited := map[string]bool{}
	queue := []string{target}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		visited[id] = true
		if a, ok := byID[id]; ok {
			t.Upstream = append(t.Upstream, a)
			queue = append(queue, a.DependsOn...)
			for _, sid := range a.SourceIDs {
				if r, ok := recByID[sid]; ok {
					t.Sources = append(t.Sources, r)
				}
			}
		}
	}
	for _, e := range events {
		if contains(string(e.Payload), target) {
			t.Events = append(t.Events, e)
		}
	}
	return t, nil
}

func contains(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
