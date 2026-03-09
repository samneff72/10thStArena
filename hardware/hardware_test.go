package hardware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// mockLineReader simulates a GPIO pin value for unit testing.
type mockLineReader struct{ val int }

func (m *mockLineReader) Value() (int, error) { return m.val, nil }

func newTestGpioPanel(initialPinValue int) *GpioFieldEStopPanel {
	return &GpioFieldEStopPanel{reader: &mockLineReader{val: initialPinValue}}
}

func TestNoopFieldLightsImplementsInterface(t *testing.T) {
	var fl FieldLights = &NoopFieldLights{}
	assert.NoError(t, fl.SetState(LightingState{Phase: PhaseAuto}))
	assert.NoError(t, fl.SetState(LightingState{Phase: PhaseTeleop}))
}

func TestNoopEStopPanelImplementsInterface(t *testing.T) {
	var ep EStopPanel = &NoopEStopPanel{}
	assert.Nil(t, ep.Poll())
}

func TestLightingStateEquality(t *testing.T) {
	s1 := LightingState{Phase: PhaseTeleop, TeleopSubPhase: SubPhaseShift1, AutoWinner: AllianceRed, ShiftWarning: false}
	s2 := LightingState{Phase: PhaseTeleop, TeleopSubPhase: SubPhaseShift1, AutoWinner: AllianceRed, ShiftWarning: false}
	s3 := LightingState{Phase: PhaseTeleop, TeleopSubPhase: SubPhaseShift1, AutoWinner: AllianceRed, ShiftWarning: true}

	assert.Equal(t, s1, s2)
	assert.NotEqual(t, s1, s3)
}

func TestMatchPhaseConstants(t *testing.T) {
	assert.Equal(t, MatchPhase(0), PhaseIdle)
	assert.Equal(t, MatchPhase(1), PhaseAuto)
	assert.Equal(t, MatchPhase(2), PhasePause)
	assert.Equal(t, MatchPhase(3), PhaseTeleop)
	assert.Equal(t, MatchPhase(4), PhaseFinished)
}

func TestNoopFieldEStopPanelImplementsInterface(t *testing.T) {
	var fep FieldEStopPanel = &NoopFieldEStopPanel{}
	assert.False(t, fep.Triggered())
	fep.Clear() // must not panic
	assert.False(t, fep.Triggered())
}

func TestGpioFieldEStopPanel_LatchOnLow(t *testing.T) {
	// Pin starts HIGH (released) — should not trigger.
	panel := newTestGpioPanel(1)
	assert.False(t, panel.Triggered())
}

func TestGpioFieldEStopPanel_LatchOnActiveLow(t *testing.T) {
	// Pin reads LOW (0) — active-low: latch should engage.
	panel := newTestGpioPanel(0)
	assert.True(t, panel.Triggered())
}

func TestGpioFieldEStopPanel_LatchPersistsAfterRelease(t *testing.T) {
	// Simulate: button pressed (LOW) → latches.
	m := &mockLineReader{val: 0}
	panel := &GpioFieldEStopPanel{reader: m}
	assert.True(t, panel.Triggered())

	// Button released (HIGH) — latch must persist.
	m.val = 1
	assert.True(t, panel.Triggered(), "latch must remain after button release")
}

func TestGpioFieldEStopPanel_ClearReleasedButton(t *testing.T) {
	m := &mockLineReader{val: 0}
	panel := &GpioFieldEStopPanel{reader: m}
	assert.True(t, panel.Triggered()) // latch set

	// Release button, then clear.
	m.val = 1
	panel.Clear()
	assert.False(t, panel.Triggered(), "latch should be cleared when button is released")
}

func TestGpioFieldEStopPanel_ClearNoopWhileHeld(t *testing.T) {
	// Button still held (LOW) — Clear() must be a no-op.
	panel := newTestGpioPanel(0)
	panel.Triggered() // latch
	panel.Clear()
	assert.True(t, panel.Triggered(), "clear while held must be a no-op")
}

func TestTeleopSubPhaseConstants(t *testing.T) {
	assert.Equal(t, TeleopSubPhase(0), SubPhaseNone)
	assert.Equal(t, TeleopSubPhase(1), SubPhaseTransition)
	assert.Equal(t, TeleopSubPhase(2), SubPhaseShift1)
	assert.Equal(t, TeleopSubPhase(3), SubPhaseShift2)
	assert.Equal(t, TeleopSubPhase(4), SubPhaseShift3)
	assert.Equal(t, TeleopSubPhase(5), SubPhaseShift4)
	assert.Equal(t, TeleopSubPhase(6), SubPhaseEndGame)
}
