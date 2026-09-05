// Package hardware defines interfaces for field hardware drivers.
// Types are defined independently of field/ to avoid circular imports.
package hardware

import (
	"time"

	"github.com/team841/bioarena/game"
)

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

// LightingState carries all context a FieldLights driver needs.
// SetState is called at every phase transition and shift boundary.
type LightingState struct {
	Phase        MatchPhase
	Shift        game.Shift // spans the whole match; game.ShiftCount when not in one
	AutoWinner   Alliance   // which alliance's HUB goes inactive first in Shift1
	ShiftWarning bool       // true during 3s window before next HUB deactivation
}

// FieldLights controls field indicator lighting.
type FieldLights interface {
	SetState(state LightingState) error
}

// StopState is the condition of one stop input.
//
// E-stop buttons are dual-channel: one NC contact and one NO contact returning
// on separate conductors. Healthy operation is always complementary, so any
// non-complementary reading means the wiring — not the button — is telling us
// something. A-stops are single-channel and can only report OK or Active.
type StopState uint8

const (
	StopOK     StopState = iota // both channels agree: button released
	StopActive                  // both channels agree: button pressed
	StopFault                   // channels disagree, unreadable, or unverifiable
)

func (s StopState) String() string {
	switch s {
	case StopOK:
		return "ok"
	case StopActive:
		return "stopped"
	case StopFault:
		return "fault"
	}
	return "unknown"
}

// FaultKind identifies why an input is in StopFault. It is FaultNone in every
// other state.
type FaultKind uint8

const (
	FaultNone        FaultKind = iota
	FaultBothOpen              // NC and NO both open: cut conductor or broken common ground
	FaultBothClosed            // NC and NO both closed: shorted conductors or welded contact
	FaultReadError             // the GPIO line could not be read
	FaultUnreachable           // the panel Pi did not answer
	FaultStale                 // the panel answered with a sample too old to trust
)

func (f FaultKind) String() string {
	switch f {
	case FaultNone:
		return "none"
	case FaultBothOpen:
		return "both channels open (cut conductor or broken ground)"
	case FaultBothClosed:
		return "both channels closed (shorted conductors or welded contact)"
	case FaultReadError:
		return "GPIO read error"
	case FaultUnreachable:
		return "panel unreachable"
	case FaultStale:
		return "panel data stale"
	}
	return "unknown fault"
}

// InputState is the decoded condition of one configured stop input.
type InputState struct {
	Station string    `json:"station"`  // "R1","R2","R3","B1","B2","B3", or "all"
	IsAStop bool      `json:"is_astop"` // true = a-stop (driver-initiated), false = e-stop
	State   StopState `json:"state"`
	Fault   FaultKind `json:"fault"`
}

// Stopped reports whether this input should hold its station(s) stopped.
// A wiring fault stops the field exactly like a pressed button: if we cannot
// prove the button is released, we must assume it is not.
func (i InputState) Stopped() bool { return i.State != StopOK }

// PanelSnapshot is the JSON body returned by a panel Pi's /poll endpoint.
//
// AgeMs is measured against the panel's own clock, so no clock synchronisation
// between the panel Pi and the main Pi is required — only that the panel is
// honest about how long ago it last read its pins.
type PanelSnapshot struct {
	Alliance string       `json:"alliance"`
	AgeMs    int64        `json:"age_ms"`
	Inputs   []InputState `json:"inputs"`
}

// EStopPanel reads physical e-stop/a-stop inputs via polling.
// Arena calls Poll() each tick; it returns the state of every configured
// input, not just the active ones — absence of an input is not evidence that
// it is healthy. Polling matches the PLC call semantics and avoids goroutine
// complexity.
type EStopPanel interface {
	Poll() []InputState
}

// FieldEStopPanel is a latching field-wide e-stop button.
// Arena calls State() every loop tick (~10 ms).
// Clear() is called by the web UI after the operator acknowledges the condition.
// Unlike EStopPanel, this interface carries state: once triggered the latch
// persists until Clear() is called while the button reads healthy and released.
type FieldEStopPanel interface {
	State() (StopState, FaultKind) // latched condition
	Clear()                        // reset latch; no-op unless the live reading is StopOK
	Close()                        // release any hardware; the panel is rebuilt on every settings save
}

// DefaultDiscrepancyWindow is how long the two channels of an e-stop may
// disagree before the disagreement is called a wiring fault. Every mushroom
// button passes through a both-open reading while its contacts are in travel;
// this window is what keeps a normal press from logging a fault.
const DefaultDiscrepancyWindow = 300 * time.Millisecond

// DecodeChannels maps a raw dual-channel reading to a state. Values are GPIO
// levels: 0 = LOW (contact closed to ground), 1 = HIGH (contact open, held up
// by the pull-up).
//
// The NC contact is closed when the button is released, so a cut conductor
// anywhere in the sense loop reads HIGH — the fault direction is toward
// detection rather than toward a false OK.
func DecodeChannels(nc, no int) (StopState, FaultKind) {
	switch {
	case nc == 0 && no == 1:
		return StopOK, FaultNone
	case nc == 1 && no == 0:
		return StopActive, FaultNone
	case nc == 1 && no == 1:
		return StopFault, FaultBothOpen
	default:
		return StopFault, FaultBothClosed
	}
}

// DecodeSingle maps a single-channel NO reading, used for A-stops.
// It cannot detect wiring faults; that is the cost of the second conductor.
func DecodeSingle(no int) StopState {
	if no == 0 {
		return StopActive
	}
	return StopOK
}

// DiscrepancyFilter holds off fault classification until the two channels have
// disagreed for longer than a button's contact travel takes.
//
// While the mismatch is inside the window the input reports StopActive: the
// field stops immediately on the first ambiguous reading and only the *label*
// waits. Nothing about this filter delays a stop.
type DiscrepancyFilter struct {
	Window time.Duration
	since  time.Time // zero while the last reading was complementary
}

// Update folds one raw decode into the filtered result. now is passed in
// rather than read from the clock so tests can drive the window directly.
func (d *DiscrepancyFilter) Update(state StopState, fault FaultKind, now time.Time) (StopState, FaultKind) {
	if state != StopFault {
		d.since = time.Time{}
		return state, fault
	}
	if d.since.IsZero() {
		d.since = now
	}
	if now.Sub(d.since) < d.Window {
		return StopActive, FaultNone
	}
	return StopFault, fault
}
