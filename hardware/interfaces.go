// Package hardware defines interfaces for field hardware drivers.
// Types are defined independently of field/ to avoid circular imports.
package hardware

// MatchPhase describes the current field state for hardware drivers.
type MatchPhase int

const (
	PhaseIdle     MatchPhase = iota
	PhaseAuto
	PhasePause
	PhaseTeleop
	PhaseFinished
)

// Alliance identifies which alliance won AUTO.
type Alliance int

const (
	AllianceNone Alliance = iota // tie or randomly assigned at match start
	AllianceRed
	AllianceBlue
)

// TeleopSubPhase represents REBUILT 2026 teleop segments.
// Only meaningful when Phase == PhaseTeleop.
type TeleopSubPhase int

const (
	SubPhaseNone       TeleopSubPhase = iota
	SubPhaseTransition // T=2:20–2:10, both HUBs active
	SubPhaseShift1     // T=2:10–1:45, one HUB inactive (per AutoWinner)
	SubPhaseShift2     // T=1:45–1:20, alternates from Shift1
	SubPhaseShift3     // T=1:20–0:55, alternates from Shift2
	SubPhaseShift4     // T=0:55–0:30, alternates from Shift3
	SubPhaseEndGame    // T=0:30–0:00, both HUBs active
)

// LightingState carries all context a FieldLights driver needs.
// SetState is called at every phase transition and sub-phase boundary.
type LightingState struct {
	Phase          MatchPhase
	TeleopSubPhase TeleopSubPhase // only meaningful when Phase == PhaseTeleop
	AutoWinner     Alliance       // which alliance's HUB goes inactive first in Shift1
	ShiftWarning   bool           // true during 3s window before next HUB deactivation
}

// FieldLights controls field indicator lighting.
type FieldLights interface {
	SetState(state LightingState) error
}

// EStopEvent represents a single hardware e-stop or a-stop activation.
type EStopEvent struct {
	Station string // "R1","R2","R3","B1","B2","B3", or "all"
	IsAStop bool   // true = a-stop (driver-initiated), false = e-stop
}

// EStopPanel reads physical e-stop/a-stop inputs via polling.
// Arena calls Poll() each tick; it returns currently-active stops.
// Polling matches the PLC call semantics and avoids goroutine complexity.
type EStopPanel interface {
	Poll() []EStopEvent
}

// FieldEStopPanel is a latching field-wide e-stop button.
// Arena calls Triggered() every loop tick (~10 ms).
// Clear() is called by the web UI after the operator acknowledges the condition.
// Unlike EStopPanel, this interface carries state: once triggered the latch
// persists until Clear() is called while the button is physically released.
type FieldEStopPanel interface {
	Triggered() bool // true while latch is active (button pressed or not yet cleared)
	Clear()          // reset latch; no-op if button is still physically held
}
