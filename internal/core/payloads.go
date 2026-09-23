package core

// --- raw_points -----------------------------------------------------------

type SamplePoint struct {
	Row  int      `json:"row"`
	Time RawValue `json:"time"`
	Mass RawValue `json:"mass"`
	Temp RawValue `json:"temp"`
}

type RawPointsPayload struct {
	Units  map[string]string `json:"units"`
	Points []SamplePoint     `json:"points"`
}

// --- drift_correction -----------------------------------------------------

type DriftCorrectionPayload struct {
	Model  string   `json:"model"` // "linear"
	Rate   RawValue `json:"rate"`  // drift rate, canonical mg/min
	Anchor RawValue `json:"anchor"`
	Offset RawValue `json:"offset"`
}

// --- furnace_program ------------------------------------------------------

type FurnaceSegment struct {
	Row      int      `json:"row"`
	Start    RawValue `json:"start"`
	End      RawValue `json:"end"`
	StartTemp RawValue `json:"start_temp"`
	EndTemp   RawValue `json:"end_temp"`
}

type FurnaceProgramPayload struct {
	Units    map[string]string `json:"units"`
	Segments []FurnaceSegment  `json:"segments"`
}

// --- gas_events -----------------------------------------------------------

type GasEvent struct {
	Row     int      `json:"row"`
	Time    RawValue `json:"time"` // valve switch time, basis=furnace_clock
	FromGas string   `json:"from_gas"`
	ToGas   string   `json:"to_gas"`
	Flow    RawValue `json:"flow"`
}

type GasEventsPayload struct {
	TransportDelay RawValue   `json:"transport_delay"`
	ThermalLag     RawValue   `json:"thermal_lag"`
	Units          map[string]string `json:"units"`
	Events         []GasEvent `json:"events"`
}

// --- blank_curve ----------------------------------------------------------

type BlankPoint struct {
	Row  int      `json:"row"`
	Time RawValue `json:"time"`
	Mass RawValue `json:"mass"`
}

type BlankCurvePayload struct {
	Units  map[string]string `json:"units"`
	Points []BlankPoint      `json:"points"`
}

// --- initial_mass ---------------------------------------------------------

// InitialMassPayload keeps the stratified layers of the weighing record:
// crucible tare, gross (crucible+sample) and reported net sample mass.
type InitialMassPayload struct {
	Tare  RawValue `json:"tare"`
	Gross RawValue `json:"gross"`
	Net   RawValue `json:"net"`
	Basis string   `json:"basis"` // e.g. "as_received", "dry"
}
