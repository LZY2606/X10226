package store

import (
	"database/sql"
	"encoding/json"
	"sort"
	"time"

	"tga-review/internal/domain"
)

// View is a projected review state at a point in history (or at head).
type View struct {
	Project        domain.Project      `json:"project"`
	Submissions    []domain.Submission `json:"submissions"`
	Latest         *domain.Derivation  `json:"latest"`
	AllDerivations []DerivationSummary `json:"all_derivations"`
	Events         []domain.Event      `json:"events"`
	Stale          []StaleItem         `json:"stale"`
	AtEventSeq     int                 `json:"at_event_seq"`
	HeadSeq        int                 `json:"head_event_seq"`
	IsHistory      bool                `json:"is_history"`
}

// DerivationSummary is a lightweight version entry for the history rail.
type DerivationSummary struct {
	ID               string             `json:"id"`
	Seq              int                `json:"seq"`
	EventID          string             `json:"event_id"`
	EventSeq         int                `json:"event_seq"`
	CreatedAt        time.Time          `json:"created_at"`
	ParamFingerprint string             `json:"param_fingerprint"`
	Sources          []domain.SourceRef `json:"sources"`
	StageCount       int                `json:"stage_count"`
	ResiduePct       *float64           `json:"final_residue_pct"`
}

// StaleItem marks an old result that is still viewable but not current.
type StaleItem struct {
	DerivationID string `json:"derivation_id"`
	TargetKind   string `json:"target_kind"`
	TargetKey    string `json:"target_key"`
	Status       string `json:"status"`
	Reason       string `json:"reason"`
}

// HeadView projects the current state.
func (s *Store) HeadView(pid string) (*View, error) {
	return s.ViewAt(pid, 0)
}

// ViewAt reconstructs the visible state immediately after event seq atSeq.
// atSeq==0 means current head. It restores the dependency versions and the
// verdicts visible then, never overwriting history with newer computations.
func (s *Store) ViewAt(pid string, atSeq int) (*View, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	p, err := s.getProjectLocked(tx, pid)
	if err != nil {
		return nil, err
	}
	v := &View{Project: *p}

	events, err := eventsLocked(tx, pid)
	if err != nil {
		return nil, err
	}
	v.HeadSeq = 0
	if len(events) > 0 {
		v.HeadSeq = events[len(events)-1].Seq
	}
	if atSeq <= 0 || atSeq >= v.HeadSeq {
		atSeq = v.HeadSeq
	} else {
		v.IsHistory = true
	}
	v.AtEventSeq = atSeq

	// events visible up to atSeq (history rail still shows full timeline)
	for _, e := range events {
		if e.Seq <= atSeq {
			v.Events = append(v.Events, e)
		}
	}

	// submissions present up to that point
	subs, err := submissionsAtLocked(tx, pid, atSeq)
	if err != nil {
		return nil, err
	}
	v.Submissions = subs

	// the derivation current at that point = latest derivation whose trigger
	// event seq <= atSeq
	dsums, err := derivationSummariesLocked(tx, pid)
	if err != nil {
		return nil, err
	}
	var current *domain.Derivation
	for i := range dsums {
		if dsums[i].EventSeq <= atSeq {
			d, err := s.GetDerivation(pid, dsums[i].ID)
			if err != nil {
				return nil, err
			}
			current = d
		}
	}
	v.AllDerivations = dsums
	v.Latest = current

	if current != nil {
		if err := projectViewLocked(tx, v, current, atSeq); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func eventsLocked(tx *sql.Tx, pid string) ([]domain.Event, error) {
	rows, err := tx.Query(
		`SELECT id, project_id, seq, type, at, payload FROM events WHERE project_id=? ORDER BY seq`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var at, pl string
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Seq, &e.Type, &at, &pl); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		_ = json.Unmarshal([]byte(pl), &e.Payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

func submissionsAtLocked(tx *sql.Tx, pid string, atSeq int) ([]domain.Submission, error) {
	// join submissions to their trigger event seq (submission_committed)
	rows, err := tx.Query(`
		SELECT s.data, MIN(e.seq) FROM submissions s
		JOIN events e ON e.project_id=s.project_id
			AND json_extract(e.payload,'$.submission_id')=s.id
		WHERE s.project_id=? AND e.seq<=?
		GROUP BY s.id ORDER BY s.seq`, pid, atSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Submission
	for rows.Next() {
		var data string
		var eseq int
		if err := rows.Scan(&data, &eseq); err != nil {
			return nil, err
		}
		var sub domain.Submission
		if err := json.Unmarshal([]byte(data), &sub); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

func derivationSummariesLocked(tx *sql.Tx, pid string) ([]DerivationSummary, error) {
	rows, err := tx.Query(
		`SELECT data FROM derivations WHERE project_id=? ORDER BY seq`, pid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DerivationSummary
	for rows.Next() {
		var data string
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var d domain.Derivation
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return nil, err
		}
		sum := DerivationSummary{
			ID: d.ID, Seq: d.Seq, EventID: d.EventID, CreatedAt: d.CreatedAt,
			ParamFingerprint: d.ParamFingerprint, Sources: d.Sources,
			StageCount: len(d.Stages),
		}
		for _, r := range d.Residues {
			if r.Label == "最终残留率" {
				p := r.Pct
				sum.ResiduePct = &p
			}
		}
		out = append(out, sum)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// attach trigger event seqs
	for i := range out {
		var eseq int
		err := tx.QueryRow(`SELECT seq FROM events WHERE id=?`, out[i].EventID).Scan(&eseq)
		if err == nil {
			out[i].EventSeq = eseq
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].EventSeq < out[j].EventSeq })
	return out, nil
}

// projectViewLocked annotates the current derivation with verdict projection
// and builds the stale-item rail from superseded derivations.
func projectViewLocked(tx *sql.Tx, v *View, current *domain.Derivation, atSeq int) error {
	events, err := eventsLocked(tx, current.ProjectID)
	if err != nil {
		return err
	}
	// Build per-target verdict state by replaying decision and carry/expire
	// events up to atSeq. State is scoped to a derivation id.
	type verdict struct {
		derivationID, action, decisionID string
		status                           string // accepted | rejected | carried | expired
	}
	state := map[string]*verdict{}
	for _, e := range events {
		if e.Seq > atSeq {
			break
		}
		switch e.Type {
		case "decision_accept", "decision_reject":
			k := tk(e.Payload)
			state[k] = &verdict{
				derivationID: strP(e.Payload, "derivation_id"),
				action:       e.Type[len("decision_"):],
				decisionID:   strP(e.Payload, "decision_id"),
				status:       e.Type[len("decision_"):],
			}
		case "decision_undo":
			k := tk(e.Payload)
			delete(state, k)
		case "decision_carried":
			k := tk(e.Payload)
			if st, ok := state[k]; ok {
				st.derivationID = strP(e.Payload, "to_derivation")
				st.status = "carried"
			}
		case "decision_expired":
			k := tk(e.Payload)
			if st, ok := state[k]; ok {
				st.derivationID = strP(e.Payload, "to_derivation")
				st.status = "expired"
			}
		}
	}
	for i := range current.Stages {
		c := &current.Stages[i]
		key := "candidate:" + c.Key
		if st, ok := state[key]; ok && st.derivationID == current.ID {
			c.Status = st.status
			c.DecisionID = st.decisionID
		}
	}
	for i := range current.Residues {
		r := &current.Residues[i]
		key := "residue:" + r.Key
		if st, ok := state[key]; ok && st.derivationID == current.ID {
			r.Status = st.status
			r.DecisionID = st.decisionID
		}
	}
	// stale items: expired verdicts remain viewable in old derivations
	for _, e := range events {
		if e.Seq > atSeq {
			break
		}
		if e.Type == "decision_expired" {
			v.Stale = append(v.Stale, StaleItem{
				DerivationID: strP(e.Payload, "from_derivation"),
				TargetKind:   strP(e.Payload, "target_kind"),
				TargetKey:    strP(e.Payload, "target_key"),
				Status:       "expired",
				Reason:       strP(e.Payload, "note"),
			})
		}
	}
	return nil
}

func tk(m map[string]any) string {
	return strP(m, "target_kind") + ":" + strP(m, "target_key")
}

func strP(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
