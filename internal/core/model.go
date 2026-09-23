package core

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"time"
)

// Clock supplies virtual or real time. Tests inject a fixed-step clock.
type Clock func() time.Time

// Raw input layer kinds. Raw content is immutable once submitted;
// revisions are new objects with ParentID pointing at the previous version.
const (
	KindSamples     = "samples"      // 原始采样点 time,mass
	KindDrift       = "drift"        // 天平漂移校正
	KindFurnace     = "furnace"      // 炉程序
	KindValves      = "valves"       // 气体阀事件
	KindBlank       = "blank"        // 空白曲线
	KindInitialMass = "initial_mass" // 样品初重
)

// Derived result kinds.
const (
	RCorrected  = "corrected_curve"
	RSampleTemp = "sample_temp"
	RAtmosphere = "atmosphere"
	RDTG        = "dtg"
	RStage      = "stage_candidate"
	RResidue    = "residue"
	RFindings   = "findings"
)

// Event types.
const (
	EvImport   = "import"
	EvDecision = "decision"
	EvConflict = "conflict"
	EvResolve  = "resolve"
	EvUndo     = "undo"
	EvFinding  = "finding"
)

type Object struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	ParentID   string `json:"parent_id"`
	Content    string `json:"content"`
	CreatedSeq int64  `json:"created_seq"`
}

type Event struct {
	Seq     int64  `json:"seq"`
	ID      string `json:"id"`
	Type    string `json:"type"`
	Payload string `json:"payload"`
	At      string `json:"at"`
}

type Result struct {
	ID         string   `json:"id"`
	Kind       string   `json:"kind"`
	Algo       string   `json:"algo"`
	ParamsFP   string   `json:"params_fp"`
	Sources    []string `json:"sources"`
	Value      string   `json:"value"`
	CreatedSeq int64    `json:"created_seq"`
}

// ResultView is a Result annotated with staleness for a given point in time.
type ResultView struct {
	Result
	Stale bool `json:"stale"`
}

func hashID(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func canonical(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// ShortID renders a stable short identity for display.
func ShortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
