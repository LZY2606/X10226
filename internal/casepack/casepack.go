// Package casepack embeds the offline, domain-specific TGA sample pack and
// replays its scenarios through the service. It replaces no domain material
// with generic spreadsheets: every fragment is tga-1 TGA data.
package casepack

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"path"

	"tgareview/internal/domain"
	"tgareview/internal/service"
)

//go:embed testdata/baseline.tga testdata/manifest.json
var baselineFS embed.FS

//go:embed testdata/scenarios/*.tga
var scenarioFS embed.FS

// Manifest describes the offline pack.
type Manifest struct {
	Format    string     `json:"format"`
	Title     string     `json:"title"`
	Seed      int64      `json:"seed"`
	Baseline  string     `json:"baseline"`
	Scenarios []Scenario `json:"scenarios"`
	Narrative Narrative  `json:"narrative"`
}

// Scenario is one single-source boundary case.
type Scenario struct {
	ID     string `json:"id"`
	File   string `json:"file"`
	Expect string `json:"expect"`
}

// Narrative is the master loss-lineage script.
type Narrative struct {
	ID    string `json:"id"`
	Steps []Step `json:"steps"`
}

// Step is one replay operation.
type Step struct {
	Op               string  `json:"op"`
	File             string  `json:"file,omitempty"`
	Kind             string  `json:"kind,omitempty"`
	Code             string  `json:"code,omitempty"`
	Message          string  `json:"message,omitempty"`
	AtmosphereDelayS float64 `json:"atmosphere_delay_s,omitempty"`
	Reason           string  `json:"reason,omitempty"`
	Kind2            string  `json:"kind,omitempty"` // decision kind
	Which            string  `json:"which,omitempty"`
	Choose           string  `json:"choose,omitempty"`
	Target           string  `json:"target,omitempty"`
}

// LoadManifest parses the embedded manifest.
func LoadManifest() (*Manifest, error) {
	data, err := baselineFS.ReadFile("testdata/manifest.json")
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Baseline returns the embedded baseline fragment text.
func Baseline() (string, error) {
	b, err := baselineFS.ReadFile("testdata/baseline.tga")
	return string(b), err
}

// ScenarioFile returns one embedded scenario fragment.
func ScenarioFile(p string) (string, error) {
	b, err := scenarioFS.ReadFile(path.Join("testdata", p))
	return string(b), err
}

// Runner executes the pack against a service with a fixed seed and virtual time.
type Runner struct {
	svc *service.Service
}

// NewRunner binds a service.
func NewRunner(svc *service.Service) *Runner { return &Runner{svc: svc} }

// CreateBaseline imports the baseline into a fresh project.
func (r *Runner) CreateBaseline(ctx context.Context, name string) (string, error) {
	m, err := LoadManifest()
	if err != nil {
		return "", err
	}
	pid, err := r.svc.CreateProject(ctx, name, "casepack:baseline", m.Seed)
	if err != nil {
		return "", err
	}
	body, err := Baseline()
	if err != nil {
		return "", err
	}
	if _, err := r.svc.ImportFragment(ctx, pid, body, "import", "", "baseline", "analyst"); err != nil {
		return "", err
	}
	return pid, nil
}

// RunScenario creates a project: baseline first, then one single-source change.
func (r *Runner) RunScenario(ctx context.Context, sc Scenario) (string, *service.ImportResult, error) {
	m, err := LoadManifest()
	if err != nil {
		return "", nil, err
	}
	pid, err := r.svc.CreateProject(ctx, "counter:"+sc.ID, "casepack:"+sc.ID, m.Seed)
	if err != nil {
		return "", nil, err
	}
	base, err := Baseline()
	if err != nil {
		return "", nil, err
	}
	if _, err := r.svc.ImportFragment(ctx, pid, base, "import", "", "baseline", "analyst"); err != nil {
		return "", nil, err
	}
	frag, err := ScenarioFile(sc.File)
	if err != nil {
		return "", nil, err
	}
	kind := "import"
	if sc.ID == "drift" || sc.ID == "weight_revision" {
		kind = "revision"
	}
	res, err := r.svc.ImportFragment(ctx, pid, frag, kind, "", sc.ID, "analyst")
	if err != nil {
		return "", nil, err
	}
	return pid, res, nil
}

// PlayNarrative executes the master loss-lineage story and returns the project
// id plus the sequence numbers recorded at each step.
func (r *Runner) PlayNarrative(ctx context.Context) (string, []NarrativeStepResult, error) {
	m, err := LoadManifest()
	if err != nil {
		return "", nil, err
	}
	pid, err := r.svc.CreateProject(ctx, "narrative:"+m.Narrative.ID, "casepack:"+m.Narrative.ID, m.Seed)
	if err != nil {
		return "", nil, err
	}
	var trace []NarrativeStepResult
	base, _ := Baseline()
	var firstStageID, conflictID string
	var lastDecisionEvent string
	for i, step := range m.Narrative.Steps {
		switch step.Op {
		case "import":
			frag := base
			if step.File != "baseline.tga" {
				f, err := ScenarioFile(step.File)
				if err != nil {
					return "", nil, err
				}
				frag = f
			}
			kind := step.Kind
			if kind == "" {
				kind = "import"
			}
			res, err := r.svc.ImportFragment(ctx, pid, frag, kind, "", "narrative", "analyst")
			if err != nil {
				return "", nil, err
			}
			trace = append(trace, NarrativeStepResult{Index: i, Op: step.Op, Seq: res.HeadSeq})
		case "add_note":
			seq, err := r.svc.AddNote(ctx, pid, step.Code, step.Message, nil, "analyst")
			if err != nil {
				return "", nil, err
			}
			trace = append(trace, NarrativeStepResult{Index: i, Op: step.Op, Seq: seq})
		case "set_params":
			p := domain.DefaultParams()
			p.AtmosphereDelayS = step.AtmosphereDelayS
			seq, err := r.svc.SetParams(ctx, pid, p, step.Reason, "analyst")
			if err != nil {
				return "", nil, err
			}
			trace = append(trace, NarrativeStepResult{Index: i, Op: step.Op, Seq: seq})
		case "decide":
			view, err := r.svc.HeadView(ctx, pid)
			if err != nil {
				return "", nil, err
			}
			st := firstStage(view)
			if st == "" {
				return "", nil, fmt.Errorf("narrative: no stage candidate")
			}
			firstStageID = st
			k := domain.DecAcceptStage
			if step.Kind == "reject_stage" {
				k = domain.DecRejectStage
			}
			seq, cid, err := r.svc.Decide(ctx, pid, service.DecideRequest{
				Kind: k, Target: "stage", TargetID: st,
				Basis: "narrative", Note: step.Op, Actor: "reviewer",
			})
			if err != nil {
				return "", nil, err
			}
			conflictID = cid
			trace = append(trace, NarrativeStepResult{Index: i, Op: step.Op, Seq: seq})
			_ = lastDecisionEvent
		case "resolve_conflict":
			if conflictID == "" {
				return "", nil, fmt.Errorf("narrative: no open conflict to resolve")
			}
			seq, err := r.svc.ResolveConflict(ctx, pid, conflictID, firstStageID,
				"accept the stage per second reviewer", "lead-reviewer")
			if err != nil {
				return "", nil, err
			}
			trace = append(trace, NarrativeStepResult{Index: i, Op: step.Op, Seq: seq})
		case "undo":
			// undo the most recent conflict.resolved / decision event
			target := findLastEvent(ctx, r.svc, pid, "conflict.resolved")
			if target == "" {
				target = findLastEvent(ctx, r.svc, pid, "decision")
			}
			if target == "" {
				return "", nil, fmt.Errorf("narrative: nothing to undo")
			}
			seq, err := r.svc.UndoEvent(ctx, pid, target, "narrative undo", "reviewer")
			if err != nil {
				return "", nil, err
			}
			trace = append(trace, NarrativeStepResult{Index: i, Op: step.Op, Seq: seq})
		default:
			return "", nil, fmt.Errorf("narrative: unknown op %q", step.Op)
		}
	}
	return pid, trace, nil
}

// NarrativeStepResult records the head seq after one narrative step.
type NarrativeStepResult struct {
	Index int    `json:"index"`
	Op    string `json:"op"`
	Seq   int64  `json:"seq"`
}

func firstStage(v *service.ProjectView) string {
	for _, st := range v.View.Stages {
		if st.Rejected == nil {
			return st.ID
		}
	}
	if len(v.View.Stages) > 0 {
		return v.View.Stages[0].ID
	}
	return ""
}

func findLastEvent(ctx context.Context, svc *service.Service, pid, typ string) string {
	events, err := svc.RawEvents(ctx, pid)
	if err != nil {
		return ""
	}
	for i := len(events) - 1; i >= 0; i-- {
		if string(events[i].Type) == typ {
			return events[i].EventID
		}
	}
	return ""
}
