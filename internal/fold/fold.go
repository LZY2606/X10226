// Package fold replays the append-only event log into review views, either at
// the current head or at any past event. Past views use the derived node and
// parameter versions bound to that point in history; new calculations never
// overwrite old conclusions.
package fold

import (
	"encoding/json"
	"sort"

	"tga-review/internal/domain"
)

// Decision is one resolved reviewer action on a candidate/residue.
type Decision struct {
	EventSeq   int                    `json:"event_seq"`
	EventID    string                 `json:"event_id"`
	TargetKind string                 `json:"target_kind"`
	NodeID     string                 `json:"node_id"`
	CID        string                 `json:"cid"`
	Action     string                 `json:"action"`
	Comment    string                 `json:"comment"`
	Snapshot   string                 `json:"snapshot"`
	Reviewer   string                 `json:"reviewer"`
	Active     bool                   `json:"active"`
	UndoneBy   int                    `json:"undone_by,omitempty"`
	WonIn      map[string]bool        `json:"-"`
}

// CandidateView projects one candidate with its resolved status at a point.
type CandidateView struct {
	domain.StageCandidate
	NodeID     string `json:"node_id"`
	Decision   string `json:"decision"` // accepted|rejected|adjusted|proposed|unknown
	DecEvent   int    `json:"decision_event,omitempty"`
	Stale      bool   `json:"stale"`
	Comment    string `json:"decision_comment,omitempty"`
}

// ResidueView projects one residue with its decision state.
type ResidueView struct {
	domain.Residue
	NodeID   string `json:"node_id"`
	Decision string `json:"decision"`
	DecEvent int    `json:"decision_event,omitempty"`
	Stale    bool   `json:"stale"`
	Comment  string `json:"decision_comment,omitempty"`
}

// SlotBind records which derived node version a slot pointed at and why.
type SlotBind struct {
	Slot      string `json:"slot"`
	NodeID    string `json:"node_id"`
	EventSeq  int    `json:"event_seq"`
	Reason    string `json:"reason,omitempty"`
}

// View is the complete review projection at one moment.
type View struct {
	ProjectID   string                       `json:"project_id"`
	Name        string                       `json:"name"`
	AtSeq       int                          `json:"at_seq"`
	Head        bool                         `json:"head"`
	Params      domain.Params                `json:"params"`
	ParamsID    string                       `json:"params_id"`
	HeadParamsAt int                         `json:"params_revised_at"`
	Slots       map[string]string            `json:"slots"`
	BindHistory []SlotBind                   `json:"bind_history"`
	Correct     *domain.Derived              `json:"correct"`
	DTG         *domain.Derived              `json:"dtg"`
	Stages      *domain.Derived              `json:"stages"`
	Residue     *domain.Derived              `json:"residue"`
	Candidates  []CandidateView              `json:"candidates"`
	Rejections  []CandidateView              `json:"rejections"`
	Residues    []ResidueView                `json:"residues"`
	Decisions   []Decision                   `json:"decisions"`
	RawHead     map[string]*domain.RawSource `json:"raw_head"`
	RawHistory  []*domain.RawSource          `json:"raw_history"`
	Batches     []*domain.Batch              `json:"batches"`
	Rules       []domain.Event               `json:"rules"`
	StaleSlots  map[string]bool              `json:"stale_slots"`
	Note        string                       `json:"note,omitempty"`
}

// Catalog is the read model needed to fold: events plus stored rows.
type Catalog interface {
	RawByID(projID, id string) (*domain.RawSource, error)
	RawAll(projID string) ([]*domain.RawSource, error)
	DerivedByID(projID, id string) (*domain.Derived, error)
	Batches(projID string) ([]*domain.Batch, error)
}

// Fold projects a project. atSeq<=0 means current head.
func Fold(projID, name string, evs []domain.Event, cat Catalog, atSeq int) (*View, error) {
	v := &View{
		ProjectID: projID,
		Name:      name,
		Slots:     map[string]string{},
		RawHead:   map[string]*domain.RawSource{},
		StaleSlots: map[string]bool{},
	}
	params := domain.DefaultParams()
	paramsID := ""
	// cid -> decision, node id -> set of cids seen (for stale marking)
	decByCID := map[string]*Decision{}
	// decisions in order (including inactive), for history
	type rawRefLite struct{ id string }
	rawByID := map[string]*domain.RawSource{}
	if cat != nil {
		if raws, err := cat.RawAll(projID); err == nil {
			v.RawHistory = raws
			for _, r := range raws {
				rawByID[r.ID] = r
			}
		}
		if bs, err := cat.Batches(projID); err == nil {
			v.Batches = bs
		}
	}

	headSeq := 0
	if len(evs) > 0 {
		headSeq = evs[len(evs)-1].Seq
	}
	if atSeq <= 0 || atSeq > headSeq {
		atSeq = headSeq
	}
	v.AtSeq = atSeq
	v.Head = atSeq == headSeq

	// active undos set of undone event seqs
	undone := map[int]bool{}
	for _, ev := range evs {
		if ev.Seq > atSeq {
			break
		}
		switch ev.Type {
		case domain.EvParams:
			var pl domain.ParamsPayload
			if json.Unmarshal(ev.Payload, &pl) == nil {
				params = pl.Params
				paramsID = pl.ParamsID
				v.HeadParamsAt = ev.Seq
				for _, b := range pl.Binds {
					bb := SlotBind{Slot: b.Slot, NodeID: b.NodeID, EventSeq: ev.Seq, Reason: b.Reason}
					v.Slots[b.Slot] = b.NodeID
					v.BindHistory = append(v.BindHistory, bb)
				}
			}
		case domain.EvImport:
			var pl domain.ImportPayload
			if json.Unmarshal(ev.Payload, &pl) == nil {
				for _, ref := range pl.Added {
					if r := rawByID[ref.ID]; r != nil {
						v.RawHead[string(r.Kind)] = r
					}
				}
				applyBinds(ev, v)
			}
		case domain.EvDecision:
			var pl domain.DecisionPayload
			if json.Unmarshal(ev.Payload, &pl) == nil {
				d := &Decision{
					EventSeq: ev.Seq, EventID: ev.ID, TargetKind: pl.TargetKind,
					NodeID: pl.NodeID, CID: pl.CID, Action: pl.Action,
					Comment: pl.Comment, Snapshot: pl.Snapshot, Reviewer: pl.Reviewer, Active: true,
				}
				decByCID[pl.CID] = d
				v.Decisions = append(v.Decisions, *d)
			}
		case domain.EvConflict:
			var pl domain.ConflictPayload
			if json.Unmarshal(ev.Payload, &pl) == nil {
				if d := decByCID[pl.LostCID]; d != nil {
					d.Active = false
				}
				// mark recorded decision history entries
				for i := range v.Decisions {
					if v.Decisions[i].CID == pl.LostCID {
						v.Decisions[i].Active = false
					}
				}
			}
		case domain.EvUndo:
			var pl domain.UndoPayload
			if json.Unmarshal(ev.Payload, &pl) == nil {
				undone[pl.UndoneEventSeq] = true
				for i := range v.Decisions {
					if v.Decisions[i].EventSeq == pl.UndoneEventSeq {
						v.Decisions[i].Active = false
						v.Decisions[i].UndoneBy = ev.Seq
					}
				}
				if d := findDec(v.Decisions, pl.UndoneEventSeq); d != nil {
					d.Active = false
				}
			}
		case domain.EvRule:
			v.Rules = append(v.Rules, ev)
		case domain.EvCreate:
			var pl domain.CreatePayload
			if json.Unmarshal(ev.Payload, &pl) == nil {
				v.Name = pl.Name
				v.Note = pl.Note
			}
		}
	}
	_ = undone
	v.Params = params
	v.ParamsID = paramsID

	// load nodes bound at atSeq
	for slot, id := range v.Slots {
		if cat == nil || id == "" {
			continue
		}
		d, err := cat.DerivedByID(projID, id)
		if err != nil {
			continue
		}
		switch slot {
		case domain.SlotCorrect:
			v.Correct = d
		case domain.SlotDTG:
			v.DTG = d
		case domain.SlotStages:
			v.Stages = d
		case domain.SlotResidue:
			v.Residue = d
		}
	}

	// at head, all bound nodes are current. In history view, compare bound id
	// with the node id that current inputs+params produce identity-wise: we
	// mark stale when a later bind for the slot exists beyond atSeq.
	latestBind := map[string]int{}
	for _, b := range v.BindHistory {
		if b.EventSeq > latestBind[b.Slot] {
			latestBind[b.Slot] = b.EventSeq
		}
	}
	if !v.Head {
		for slot, seq := range latestBind {
			if seq > atSeq {
				v.StaleSlots[slot] = true
			}
		}
	}

	// project candidates
	if v.Stages != nil && v.Stages.Stages != nil {
		for _, c := range candsOf(v.Stages) {
			cv := CandidateView{StageCandidate: c, NodeID: v.Stages.ID}
			if d := decByCID[c.CID]; d != nil && d.NodeID == v.Stages.ID && d.Active {
				cv.Decision = d.Action
				cv.DecEvent = d.EventSeq
				cv.Comment = d.Comment
			} else if c.Status == "rejected" {
				cv.Decision = "rejected"
			} else if c.Status == "unknown" {
				cv.Decision = "unknown"
			} else {
				cv.Decision = "proposed"
			}
			v.Candidates = append(v.Candidates, cv)
		}
		for _, r := range v.Stages.Stages.Rejections {
			v.Rejections = append(v.Rejections, CandidateView{StageCandidate: r, NodeID: v.Stages.ID, Decision: "rejected"})
		}
	}
	if v.Residue != nil && v.Residue.Residue != nil {
		for _, r := range v.Residue.Residue.Residues {
			rv := ResidueView{Residue: r, NodeID: v.Residue.ID}
			if d := decByCID[r.RID]; d != nil && d.NodeID == v.Residue.ID && d.Active {
				rv.Decision = d.Action
				rv.DecEvent = d.EventSeq
				rv.Comment = d.Comment
			} else if r.Status == "unknown" {
				rv.Decision = "unknown"
			} else {
				rv.Decision = "proposed"
			}
			v.Residues = append(v.Residues, rv)
		}
	}

	// mark stale on items whose node slot rebound later
	if !v.Head {
		for i := range v.Candidates {
			if v.StaleSlots[domain.SlotStages] {
				v.Candidates[i].Stale = true
			}
		}
		for i := range v.Residues {
			if v.StaleSlots[domain.SlotResidue] {
				v.Residues[i].Stale = true
			}
		}
	}

	sort.SliceStable(v.BindHistory, func(i, j int) bool {
		if v.BindHistory[i].EventSeq != v.BindHistory[j].EventSeq {
			return v.BindHistory[i].EventSeq < v.BindHistory[j].EventSeq
		}
		return v.BindHistory[i].Slot < v.BindHistory[j].Slot
	})
	return v, nil
}

func candsOf(d *domain.Derived) []domain.StageCandidate {
	if d.Stages == nil {
		return nil
	}
	return d.Stages.Candidates
}

func findDec(ds []Decision, seq int) *Decision {
	for i := range ds {
		if ds[i].EventSeq == seq {
			return &ds[i]
		}
	}
	return nil
}

func applyBinds(ev domain.Event, v *View) {
	var pl struct {
		Binds []SlotBind `json:"binds"`
	}
	if json.Unmarshal(ev.Payload, &pl) != nil {
		return
	}
	for _, b := range pl.Binds {
		b.EventSeq = ev.Seq
		v.Slots[b.Slot] = b.NodeID
		v.BindHistory = append(v.BindHistory, b)
	}
}
