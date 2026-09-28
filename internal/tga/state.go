package tga

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Clock lets tests and replay use virtual time.
type Clock func() time.Time

// Project is the replayed state at a point in the event log.
type Project struct {
	ID          string
	Title       string
	Description string

	Events []*Event

	// raw layer: newest commit per section plus full history
	commits    map[Section][]*RawCommit
	parsed     map[string]*ParsedSections // commitID -> parse
	headCommit map[Section]string

	// accepted lineage: source section -> current accepted commit
	current map[Section]*RawCommit

	Params []Params // version history
	params Params

	// derived artifacts for every params version, keyed by artifact id.
	artifacts map[string]any

	// decisions keyed by logical candidate key (cluster+variant) and residue
	decisionByKey map[string]*Decision
	decisions     []*Decision

	bases []*RuleBasisPayload
}

// Decision is the applied human decision state.
type Decision struct {
	ID            string
	EventSeq      int
	Subject       string
	CandidateKey  string
	Action        string
	Reason        string
	Reviewer      string
	Supersedes    string // prior decision id this overrides
	Resolution    string
	At            time.Time
	Undone        bool
	UndoneBy      string
}

func newProject(id, title, desc string) *Project {
	return &Project{
		ID: id, Title: title, Description: desc,
		commits:       map[Section][]*RawCommit{},
		parsed:        map[string]*ParsedSections{},
		headCommit:    map[Section]string{},
		current:       map[Section]*RawCommit{},
		Params:        []Params{defaultParams()},
		params:        defaultParams(),
		artifacts:     map[string]any{},
		decisionByKey: map[string]*Decision{},
	}
}

// StateEngine appends events and rebuilds derived state.
type StateEngine struct {
	clock Clock
}

func NewEngine(clock Clock) *StateEngine {
	if clock == nil {
		clock = time.Now
	}
	return &StateEngine{clock: clock}
}

// CreateProject appends a project_created event.
func (e *StateEngine) CreateProject(p *Project, title, desc string) (*Event, error) {
	if p == nil {
		id := stableProjectID(title + ":" + desc)
		p = newProject(id, title, desc)
	}
	return appendEvent(p, EvProjectCreated, ProjectCreatedPayload{Title: title, Description: desc}, e.clock())
}

// CommitRaw submits one immutable chunk for a section. Parent is linked to the
// section head. Half-failed sections (Accepted=false) are retained but never
// promoted to the normalised layer.
func (e *StateEngine) CommitRaw(p *Project, section Section, rawText, label string) (*Event, error) {
	if !validSection(section) {
		return nil, fmt.Errorf("unknown section %q", section)
	}
	cid := commitID(p.ID, len(p.Events)+1, section, rawText, label)
	parsed, err := ParseInput(rawText, cid)
	if err != nil {
		return nil, err
	}
	// isolate the single section's rows/diagnostics from a multi-section chunk
	ok := sectionAccepted(parsed, section)
	var diags []Diagnostic
	for _, d := range parsed.Diags {
		if d.Section == section || d.Section == "" {
			diags = append(diags, d)
		}
	}
	parent := p.headCommit[section]
	pl := RawCommittedPayload{
		CommitID: cid, Section: section, Parent: parent, Format: FormatID,
		RawText: rawText, Accepted: ok, Label: label, Diags: diags,
		InputClip: clipFor(rawText, section),
	}
	return appendEvent(p, EvRawCommitted, pl, e.clock())
}

// ReviseParams stores a new parameter version; dependent artifacts recompute.
func (e *StateEngine) ReviseParams(p *Project, np Params, reason string) (*Event, error) {
	np.Version = len(p.Params)
	np.ParentEventID = ""
	return appendEvent(p, EvParamsRevised, ParamsRevisedPayload{Params: np, Reason: reason}, e.clock())
}

// Decide records accept/reject/adjust. Conflicting decisions are linked rather
// than overwritten; undo is itself an event.
func (e *StateEngine) Decide(p *Project, subject, candidateKey, action, reason, reviewer string, adjust *Params) (*Event, error) {
	id := decisionID(p.ID, len(p.Events)+1, []any{subject, candidateKey, action, reason})
	pl := DecisionPayload{
		DecisionID: id, Subject: subject, Action: action, Reason: reason,
		Reviewer: reviewer, Adjust: adjust, CandidateKey: candidateKey,
	}
	if cand, ok := p.decisionByKey[candidateKey]; ok && cand != nil && !cand.Undone {
		if cand.Action != action {
			pl.ConflictFrom = cand.ID
			pl.ConflictResolution = "later decision supersedes earlier decision; prior version retained in history"
		}
	}
	return appendEvent(p, EvDecision, pl, e.clock())
}

// AddBasis stores a machine-readable judgement basis.
func (e *StateEngine) AddBasis(p *Project, code, statement string, evidence map[string]any, relates []string) (*Event, error) {
	id := basisID(p.ID, len(p.Events)+1, code, evidence)
	return appendEvent(p, EvRuleBasis, RuleBasisPayload{BasisID: id, Code: code, Statement: statement, Evidence: evidence, RelatesTo: relates}, e.clock())
}

// Undo marks a previous event undone and recomputes state from the log.
func (e *StateEngine) Undo(p *Project, targetEventID, reason string) (*Event, error) {
	found := false
	for _, ev := range p.Events {
		if ev.ID == targetEventID {
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("no event %s", targetEventID)
	}
	return appendEvent(p, EvUndo, UndoPayload{TargetEventID: targetEventID, Reason: reason}, e.clock())
}

func validSection(s Section) bool {
	for _, q := range AllSections {
		if q == s {
			return true
		}
	}
	return false
}

func sectionAccepted(parsed *ParsedSections, s Section) bool {
	// A section accepts when it parsed without hard errors and produced the
	// expected payload. Samples need >=1 good point; scalar layers just need
	// their object; procedural layers may be empty (still accepted).
	if v, ok := parsed.SectionOK[s]; ok && !v {
		return false
	}
	switch s {
	case SecSamples:
		return len(parsed.Samples) > 0
	case SecDrift:
		return parsed.Drift != nil
	case SecInitialMass:
		return parsed.InitialMass != nil
	case SecBlank:
		return len(parsed.Blank) > 0
	case SecFurnace, SecGas:
		return true
	}
	return true
}

func clipFor(raw, s Section) string {
	// keep the fragment for the affected section (input evidence)
	in := false
	var out []byte
	for _, line := range splitLines(raw) {
		if len(line) >= 2 && line[0] == '#' && line[1] == '#' {
			in = Section(sanitizeHeader(line[2:])) == s
		}
		if in {
			out = append(out, line...)
			out = append(out, '\n')
		}
	}
	return string(out)
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// appendEvent encodes the payload and rebuilds the project state.
func appendEvent(p *Project, kind EventKind, payload any, at time.Time) (*Event, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	seq := len(p.Events) + 1
	var id string
	switch kind {
	case EvProjectCreated:
		id = eventID(p.ID, seq, string(kind), payload)
	default:
		id = eventID(p.ID, seq, string(kind), payload)
	}
	ev := &Event{ID: id, ProjectID: p.ID, Seq: seq, Kind: kind, Payload: b, At: at}
	p.Events = append(p.Events, ev)
	if err := Rebuild(p); err != nil {
		// keep event in log but surface the failure
		return ev, err
	}
	return ev, nil
}

// sourcesFor returns current accepted source lineage for the given sections.
func sourcesFor(p *Project, secs []Section) []SourceVersion {
	var out []SourceVersion
	for _, s := range secs {
		if c := p.current[s]; c != nil {
			out = append(out, SourceVersion{Section: s, CommitID: c.ID, Seq: c.Seq})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Section < out[j].Section })
	return out
}
