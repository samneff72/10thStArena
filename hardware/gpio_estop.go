// Copyright 2026 10th Street Robotics. All Rights Reserved.
//
// GpioFieldEStopPanel reads a physically wired NC (normally-closed) e-stop button
// connected to a Raspberry Pi GPIO pin.
//
// Wiring: NC contact between GPIO pin and GND; Pi internal pull-up enabled.
// Active-low: pin reads LOW (0) when the e-stop condition is triggered.
// The latch persists after the button is released and must be cleared via Clear().

package hardware

import "sync/atomic"

// lineReader abstracts the GPIO pin value read for testability.
// On Linux the production constructor provides a *gpiod.Line.
// Tests inject a mockLineReader without any real GPIO dependency.
type lineReader interface {
	Value() (int, error)
}

// GpioFieldEStopPanel implements FieldEStopPanel using a GPIO pin.
// It is safe to call Triggered() and Clear() from any goroutine.
type GpioFieldEStopPanel struct {
	reader    lineReader
	triggered atomic.Bool
}

// Triggered returns true while the latch is active.
// It re-latches on every call if the pin still reads active-low (LOW = 0),
// so the latch cannot be silently cleared by a concurrent goroutine while
// the button remains physically held.
func (g *GpioFieldEStopPanel) Triggered() bool {
	if val, err := g.reader.Value(); err == nil && val == 0 {
		g.triggered.Store(true)
	}
	return g.triggered.Load()
}

// Clear resets the latch only if the button is physically released (pin reads HIGH).
// If the button is still held this is a safe no-op; the arena Update() loop will
// re-detect the condition on the next tick.
func (g *GpioFieldEStopPanel) Clear() {
	if val, err := g.reader.Value(); err == nil && val == 1 {
		g.triggered.Store(false)
	}
}
