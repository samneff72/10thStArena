// Copyright 2026 10th Street Robotics. All Rights Reserved.
//
// GpioFieldEStopPanel reads a physically wired dual-channel e-stop button
// connected to Raspberry Pi GPIO pins.
//
// Wiring: three conductors run to the button — common GND, the NC contact, and
// the NO contact. Both GPIO lines use the Pi's internal pull-up. Released, the
// NC contact is closed (LOW) and the NO contact is open (HIGH); pressed, they
// swap. Any other combination is a wiring fault: both HIGH means an open
// circuit (cut conductor or broken ground), both LOW means a short.
//
// A fault stops the field exactly like a pressed button. The latch persists
// after the button is released and must be cleared via Clear(), which refuses
// while the live reading is anything but healthy-and-released.
//
// Passing ncPin = 0 configures a single-channel NO-only input, which behaves
// like the pre-dual-channel driver and cannot detect wiring faults.

package hardware

import (
	"sync"
	"time"
)

// lineReader abstracts the GPIO pin value read for testability.
// On Linux the production constructor provides a *gpiocdev.Line.
// Tests inject a mockLineReader without any real GPIO dependency.
type lineReader interface {
	Value() (int, error)
}

// GpioFieldEStopPanel implements FieldEStopPanel using a pair of GPIO pins.
// It is safe to call State() and Clear() from any goroutine.
type GpioFieldEStopPanel struct {
	nc lineReader // nil for a single-channel (NO-only) input
	no lineReader

	mu           sync.Mutex
	filter       DiscrepancyFilter
	latched      StopState
	latchedFault FaultKind
}

// State returns the latched condition, re-reading the pins first.
//
// The latch only ever escalates: an active button latches over OK, and a fault
// latches over an active button, so a fault that appears and then flickers
// back to a plain press still has to be acknowledged as a fault.
func (g *GpioFieldEStopPanel) State() (StopState, FaultKind) {
	g.mu.Lock()
	defer g.mu.Unlock()

	state, fault := g.read(time.Now())
	if state == StopFault || (state == StopActive && g.latched == StopOK) {
		g.latched, g.latchedFault = state, fault
	}
	return g.latched, g.latchedFault
}

// Clear resets the latch only if the button currently reads healthy and
// released. If it is still held, or the wiring is still faulted, this is a
// safe no-op and the arena's Update() loop re-latches on the next tick.
func (g *GpioFieldEStopPanel) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if state, _ := g.read(time.Now()); state == StopOK {
		g.latched, g.latchedFault = StopOK, FaultNone
	}
}

// Close releases the GPIO lines.
//
// Required because the panel is rebuilt whenever settings are saved, and the kernel gives a
// line to one requester at a time. Without releasing the old panel first, the second save
// of a run fails to open the pins, and the arena falls back to NoopFieldEStopPanel -- which
// reports StopOK forever. The field would show a healthy e-stop with nothing watching the
// button, which is the one direction this must never fail in.
//
// Idempotent, and safe on a single-channel panel where nc is nil. The lines are reached
// through a type assertion rather than through lineReader, which stays Value()-only so the
// tests can keep substituting plain fakes.
func (g *GpioFieldEStopPanel) Close() {
	g.mu.Lock()
	defer g.mu.Unlock()

	closeLine := func(line lineReader) {
		if closer, ok := line.(interface{ Close() error }); ok {
			_ = closer.Close()
		}
	}
	if g.nc != nil {
		closeLine(g.nc)
		g.nc = nil
	}
	if g.no != nil {
		closeLine(g.no)
		g.no = nil
	}
}

// read samples both channels and decodes them. Callers must hold g.mu.
//
// A read error is a fault immediately rather than being fed through the
// discrepancy window: an unreadable line is not a button in travel, and
// holding the previous value would be the one failure mode that hides itself.
func (g *GpioFieldEStopPanel) read(now time.Time) (StopState, FaultKind) {
	// A closed panel reads as a fault rather than panicking on the nil line. Nothing should
	// poll one, but a stop input is the wrong place to find out by crashing -- and reporting
	// a fault stops the field, where reporting OK would not.
	if g.no == nil {
		return StopFault, FaultReadError
	}
	noVal, err := g.no.Value()
	if err != nil {
		return StopFault, FaultReadError
	}
	if g.nc == nil {
		return DecodeSingle(noVal), FaultNone
	}
	ncVal, err := g.nc.Value()
	if err != nil {
		return StopFault, FaultReadError
	}
	state, fault := DecodeChannels(ncVal, noVal)
	return g.filter.Update(state, fault, now)
}
