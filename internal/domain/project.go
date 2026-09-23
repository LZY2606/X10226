package domain

// Project groups all sources, derived nodes, decisions and history for one TGA
// review. Projects are self-contained and fully exportable.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

// ArchiveEntry is one counterexample-archive record: a single direct-source
// change (or scenario), both adopted and rejected outcomes, machine-readable
// rule judgments and the exact scope of objects affected.
type ArchiveEntry struct {
	ID            string         `json:"id"`
	ProjectID     string         `json:"project_id"`
	Scenario      string         `json:"scenario"`
	Title         string         `json:"title"`
	Status        string         `json:"status"`
	ChangedSource string         `json:"changed_source,omitempty"`
	Before        map[string]any `json:"before,omitempty"`
	After         map[string]any `json:"after,omitempty"`
	Rules         []RuleEvent    `json:"rules,omitempty"`
	Scope         []string       `json:"scope,omitempty"`
	Adopted       bool           `json:"adopted"`
	CreatedAt     string         `json:"created_at"`
}
