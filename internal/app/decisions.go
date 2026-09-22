package app

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"tga-review/internal/domain"
)

// Decide records an accept/reject/adjust decision as an event and re-derives.
func (s *Service) Decide(projectID string, d domain.DecisionPayload) (domain.Event, error) {
	ev, err := s.Store.AppendEvent(projectID, domain.EvDecision, s.now(), defaultActor, d)
	if err != nil {
		return domain.Event{}, err
	}
	if err := s.deriveFor(ev.Seq); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// Undo records an undo event targeting an earlier seq and re-derives. The
// undone event is never deleted; returning to it restores that historical view.
func (s *Service) Undo(projectID string, targetSeq int64, note string) (domain.Event, error) {
	ev, err := s.Store.AppendEvent(projectID, domain.EvUndo, s.now(), defaultActor,
		map[string]any{"target_seq": targetSeq, "note": note})
	if err != nil {
		return domain.Event{}, err
	}
	if err := s.deriveFor(ev.Seq); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// ResolveConflict chooses between mutually exclusive accepted proposals.
func (s *Service) ResolveConflict(projectID string, regionID, chosenID string, c domain.ConflictPayload) (domain.Event, error) {
	c.RegionID = regionID
	c.Resolution = "chosen"
	c.ChosenID = chosenID
	ev, err := s.Store.AppendEvent(projectID, domain.EvConflictResolved, s.now(), defaultActor, c)
	if err != nil {
		return domain.Event{}, err
	}
	if err := s.deriveFor(ev.Seq); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// applyDecisions returns the candidate-set artifacts after replaying decisions
// up to maxSeq. Each effective accept/reject/adjust adds one immutable
// candidate-set version; the as-of view selects the newest at that seq.
func (s *Service) applyDecisions(projectID string, arts []domain.Artifact,
	events []domain.Event, maxSeq int64) ([]domain.Artifact, error) {

	baseSet, baseIdx := latestCandidateSet(arts)
	if baseIdx < 0 {
		return arts, nil
	}
	conflictsOpen := map[string]bool{}
	resolutions := map[string]domain.ConflictPayload{}
	var decs []struct {
		seq int64
		d   domain.DecisionPayload
	}
	for _, e := range events {
		if e.Seq > maxSeq {
			break
		}
		if isUndone(events, e.Seq, maxSeq) {
			continue
		}
		switch e.Type {
		case domain.EvDecision:
			var d domain.DecisionPayload
			if err := json.Unmarshal(e.Payload, &d); err != nil {
				return nil, err
			}
			decs = append(decs, struct {
				seq int64
				d   domain.DecisionPayload
			}{e.Seq, d})
		case domain.EvConflict:
			var c domain.ConflictPayload
			_ = json.Unmarshal(e.Payload, &c)
			conflictsOpen[c.RegionID] = true
		case domain.EvConflictResolved:
			var c domain.ConflictPayload
			_ = json.Unmarshal(e.Payload, &c)
			resolutions[c.RegionID] = c
			delete(conflictsOpen, c.RegionID)
		}
	}
	if len(decs) == 0 {
		return arts, nil
	}

	current := cloneCandidateSet(baseSet.Payload)
	var out []domain.Artifact
	out = append(out, arts...)
	for _, dd := range decs {
		d := dd.d
		changed := false
		for ri := range current.Regions {
			for pi := range current.Regions[ri].Proposals {
				p := &current.Regions[ri].Proposals[pi]
				if p.ID != d.ProposalID {
					continue
				}
				switch d.Action {
				case "accept":
					if p.Status == domain.StatusAccepted {
						continue
					}
					// accepting one proposal in a region rejects siblings, and
					// two accepted exclusive variants in sequence open a conflict
					hasAccepted := false
					for _, q := range current.Regions[ri].Proposals {
						if q.Status == domain.StatusAccepted && q.ID != p.ID {
							hasAccepted = true
						}
					}
					if hasAccepted {
						conflictsOpen[current.Regions[ri].ID] = true
						p.Status = domain.StatusConflict
					} else {
						p.Status = domain.StatusAccepted
					}
					p.DecisionSeq = dd.seq
				case "reject":
					p.Status = domain.StatusRejected
					p.DecisionSeq = dd.seq
				case "adjust":
					if d.Adjusted != nil {
						adj := *d.Adjusted
						adj.ID = adjustedID(p.ID, dd.seq)
						adj.ParentID = p.ID
						adj.Manual = true
						adj.Status = domain.StatusAccepted
						adj.DecisionSeq = dd.seq
						current.Regions[ri].Proposals = append(current.Regions[ri].Proposals, adj)
						p.Status = domain.StatusAdjusted
					}
				}
				changed = true
			}
		}
		if !changed {
			continue
		}
		payload, _ := json.Marshal(current)
		versionArt := domain.Artifact{
			ID: versionedCandidateID(baseSet.ID, dd.seq), ProjectID: projectID,
			Kind: domain.ArtCandSet, Family: "candidate-set",
			Label:       fmt.Sprintf("Candidate set after decision @%d", dd.seq),
			AlgoVersion: baseSet.AlgoVersion, ParamFP: baseSet.ParamFP,
			SourceIDs: baseSet.SourceIDs, DependsOn: baseSet.DependsOn,
			CreatedSeq: dd.seq, Payload: payload,
		}
		out = append(out, versionArt)
	}
	return out, nil
}

func latestCandidateSet(arts []domain.Artifact) (domain.Artifact, int) {
	idx := -1
	var best domain.Artifact
	for i, a := range arts {
		if a.Kind == domain.ArtCandSet && a.CreatedSeq > 0 {
			if idx < 0 || a.CreatedSeq > best.CreatedSeq {
				best, idx = a, i
			}
		}
	}
	// fallback: the engine-produced set has CreatedSeq 0
	for i, a := range arts {
		if a.Kind == domain.ArtCandSet && idx < 0 {
			return a, i
		}
	}
	return best, idx
}

func cloneCandidateSet(raw json.RawMessage) *domain.CandidateSet {
	var cs domain.CandidateSet
	_ = json.Unmarshal(raw, &cs)
	return &cs
}

func versionedCandidateID(base string, seq int64) string {
	h := sha1.Sum([]byte(base + fmt.Sprintf("|dec-%d", seq)))
	return "art-candidate-set-dec-" + hex.EncodeToString(h[:])[:12]
}

func adjustedID(parent string, seq int64) string {
	h := sha1.Sum([]byte(parent + fmt.Sprintf("|adj-%d", seq)))
	return "prop-manual-" + hex.EncodeToString(h[:])[:12]
}

func sortedArtifactFamilies(arts []domain.Artifact) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range arts {
		if !seen[a.Family] {
			seen[a.Family] = true
			out = append(out, a.Family)
		}
	}
	sort.Strings(out)
	return out
}
