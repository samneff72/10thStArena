package field

import (
	"testing"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/stretchr/testify/assert"
)

// --- teleopShift ---

func TestTeleopShiftBoundaries(t *testing.T) {
	cases := []struct {
		remaining int
		want      game.Shift
	}{
		{135, game.ShiftTransition}, // above transition window
		{131, game.ShiftTransition},
		{130, game.Shift1}, // boundary: <=130 → Shift1
		{106, game.Shift1},
		{105, game.Shift2}, // boundary: <=105 → Shift2
		{81, game.Shift2},
		{80, game.Shift3}, // boundary: <=80 → Shift3
		{56, game.Shift3},
		{55, game.Shift4}, // boundary: <=55 → Shift4
		{31, game.Shift4},
		{30, game.ShiftEndgame}, // boundary: <=30 → EndGame
		{0, game.ShiftEndgame},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, teleopShift(c.remaining), "remaining=%d", c.remaining)
	}
}

// --- shiftChangeSounds ---

// The cue marks the four HUB handovers and nothing else. The shifts that do not sound are
// the point of the test: each already has a cue of its own, and stacking two on one moment
// is what this excludes.
func TestShiftChangeSoundsOnlyOnHandovers(t *testing.T) {
	for _, shift := range []game.Shift{game.Shift1, game.Shift2, game.Shift3, game.Shift4} {
		assert.True(t, shiftChangeSounds(shift), "shift %d should sound", shift)
	}
	for _, shift := range []game.Shift{
		game.ShiftAuto,       // "start"
		game.ShiftTransition, // "resume"
		game.ShiftEndgame,    // "warning"
		game.ShiftPostMatch,  // "end"
		game.ShiftCount,      // not a shift at all
	} {
		assert.False(t, shiftChangeSounds(shift), "shift %d should be silent", shift)
	}
}

// The cue is driven from the lighting transition, so the boundaries it fires on must be the
// ones teleopShift produces. Walking the teleop second by second proves there are exactly
// four, and that they sit where the lights change rather than near them.
func TestShiftChangeSoundsMatchLightingBoundaries(t *testing.T) {
	var boundaries []int
	previous := teleopShift(200) // before the first boundary
	for remaining := 200; remaining >= 0; remaining-- {
		current := teleopShift(remaining)
		if current != previous && shiftChangeSounds(current) {
			boundaries = append(boundaries, remaining)
		}
		previous = current
	}
	assert.Equal(t, []int{130, 105, 80, 55}, boundaries)
}

// --- shiftWarning ---

func TestShiftWarningWindows(t *testing.T) {
	// Warning should fire for the 3s window BEFORE each shift boundary.
	// Boundary at 130: warning at [130,133)
	assert.True(t, shiftWarning(130), "130")
	assert.True(t, shiftWarning(132), "132")
	assert.False(t, shiftWarning(133), "133 — outside window")
	assert.False(t, shiftWarning(129), "129 — already past boundary")

	// Boundary at 105: warning at [105,108)
	assert.True(t, shiftWarning(105))
	assert.True(t, shiftWarning(107))
	assert.False(t, shiftWarning(108))

	// Boundary at 80: warning at [80,83)
	assert.True(t, shiftWarning(80))
	assert.True(t, shiftWarning(82))
	assert.False(t, shiftWarning(83))

	// Boundary at 55: warning at [55,58)
	assert.True(t, shiftWarning(55))
	assert.True(t, shiftWarning(57))
	assert.False(t, shiftWarning(58))

	// No warning for EndGame
	assert.False(t, shiftWarning(29))
	assert.False(t, shiftWarning(0))
}

// --- computeLightingState ---

func TestComputeLightingStatePhaseMapping(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed

	// The shift spans the whole match, as upstream models it: AUTO and post-match have
	// their own shifts, and states that are not a shift report ShiftCount.
	arena.MatchState = PreMatch
	ls := arena.computeLightingState(0)
	assert.Equal(t, hardware.PhaseIdle, ls.Phase)
	assert.Equal(t, game.ShiftCount, ls.Shift)

	arena.MatchState = AutoPeriod
	ls = arena.computeLightingState(3)
	assert.Equal(t, hardware.PhaseAuto, ls.Phase)
	assert.Equal(t, game.ShiftAuto, ls.Shift)
	assert.Equal(t, hardware.AllianceRed, ls.AutoWinner)

	arena.MatchState = PausePeriod
	ls = arena.computeLightingState(18)
	assert.Equal(t, hardware.PhasePause, ls.Phase)
	assert.Equal(t, game.ShiftCount, ls.Shift)

	arena.MatchState = PostMatch
	ls = arena.computeLightingState(160)
	assert.Equal(t, hardware.PhaseFinished, ls.Phase)
	assert.Equal(t, game.ShiftPostMatch, ls.Shift)
}

// --- EStopPanel polling integration ---

// recordingPanel implements hardware.EStopPanel and returns a fixed snapshot.
type recordingPanel struct {
	inputs []hardware.InputState
}

func (r *recordingPanel) Poll() []hardware.InputState { return r.inputs }

// pollPanels drives the panels exactly the way Update() does.
func pollPanels(arena *Arena) {
	for _, p := range arena.EStopPanels {
		for _, input := range p.Poll() {
			stations := []string{input.Station}
			if input.Station == "all" {
				stations = allStationNames
			}
			for _, station := range stations {
				arena.handlePanelInput(station, input)
			}
		}
	}
}

func TestEStopPanelPollSingleStation(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{{Station: "R1", State: hardware.StopActive}},
	}}

	pollPanels(arena)

	assert.True(t, arena.AllianceStations["R1"].EStop.Load())
	assert.False(t, arena.AllianceStations["B1"].EStop.Load())
}

func TestEStopPanelPollAllStations(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{{Station: "all", State: hardware.StopActive}},
	}}

	pollPanels(arena)

	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s", station)
	}
}

func TestEStopPanelReleasedInputClearsStop(t *testing.T) {
	arena := setupTestArena(t)
	panel := &recordingPanel{
		inputs: []hardware.InputState{{Station: "R1", State: hardware.StopActive}},
	}
	arena.EStopPanels = []hardware.EStopPanel{panel}
	pollPanels(arena)
	assert.True(t, arena.AllianceStations["R1"].EStop.Load())

	// An explicit released reading is what clears the stop outside a match;
	// with the old presence-based protocol there was nothing to report here.
	panel.inputs = []hardware.InputState{{Station: "R1", State: hardware.StopOK}}
	pollPanels(arena)
	assert.False(t, arena.AllianceStations["R1"].EStop.Load())
}

func TestEStopPanelFaultStopsStation(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{
			{Station: "R2", State: hardware.StopFault, Fault: hardware.FaultBothOpen},
		},
	}}

	pollPanels(arena)

	assert.True(t, arena.AllianceStations["R2"].EStop.Load(), "a wiring fault must stop the station")
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.AllianceStations["R2"].EStopFault.Load())
}

func TestEStopPanelFaultBlocksMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	// R1 is checked first, so its fault is what the operator is told about.
	arena.AllianceStations["R1"].EStopFault.Store(uint32(hardware.FaultBothClosed))

	err := arena.checkCanStartMatch()
	assert.ErrorContains(t, err, "wiring fault")
	assert.ErrorContains(t, err, "R1")
}

func TestEStopPanelFaultClearsWhenWiringRepaired(t *testing.T) {
	arena := setupTestArena(t)
	panel := &recordingPanel{
		inputs: []hardware.InputState{
			{Station: "R1", State: hardware.StopFault, Fault: hardware.FaultBothOpen},
		},
	}
	arena.EStopPanels = []hardware.EStopPanel{panel}
	pollPanels(arena)
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.AllianceStations["R1"].EStopFault.Load())

	// The fault tracks the live wiring rather than latching, so a repair that
	// the panel reports as healthy takes the fault with it.
	panel.inputs = []hardware.InputState{{Station: "R1", State: hardware.StopOK}}
	pollPanels(arena)
	assert.Equal(t, uint32(hardware.FaultNone), arena.AllianceStations["R1"].EStopFault.Load())
	assert.False(t, arena.AllianceStations["R1"].EStop.Load())
	if err := arena.checkCanStartMatch(); err != nil {
		assert.NotContains(t, err.Error(), "wiring fault")
	}
}

func TestAStopInputDoesNotClearLatchedEStop(t *testing.T) {
	arena := setupTestArena(t)
	arena.EStopPanels = []hardware.EStopPanel{&recordingPanel{
		inputs: []hardware.InputState{
			{Station: "R1", IsAStop: false, State: hardware.StopActive},
			{Station: "R1", IsAStop: true, State: hardware.StopActive},
		},
	}}

	pollPanels(arena)

	// Each input only speaks for its own half of the station's state.
	assert.True(t, arena.AllianceStations["R1"].EStop.Load(), "an a-stop must not clear an e-stop")
	assert.True(t, arena.AllianceStations["R1"].AStop.Load())
}

// --- GPIO FieldEStop arena integration ---

// mockFieldEStop simulates a dual-channel GPIO field e-stop, latching the same
// way the real driver does.
type mockFieldEStop struct {
	live         hardware.StopState // what the pins currently read
	liveFault    hardware.FaultKind
	latched      hardware.StopState
	latchedFault hardware.FaultKind
}

func (m *mockFieldEStop) State() (hardware.StopState, hardware.FaultKind) {
	if m.live == hardware.StopFault || (m.live == hardware.StopActive && m.latched == hardware.StopOK) {
		m.latched, m.latchedFault = m.live, m.liveFault
	}
	return m.latched, m.latchedFault
}

func (m *mockFieldEStop) Clear() {
	if m.live == hardware.StopOK {
		m.latched, m.latchedFault = hardware.StopOK, hardware.FaultNone
	}
}

// pollFieldEStop drives the field e-stop the way Update() does.
func pollFieldEStop(arena *Arena) {
	state, fault := arena.FieldEStop.State()
	arena.fieldEStopFault.Store(uint32(fault))
	if state != hardware.StopOK && !arena.fieldEStopActive.Load() {
		arena.fieldEStopActive.Store(true)
		for _, as := range arena.AllianceStations {
			as.EStop.Store(true)
		}
	}
}

func TestFieldEStopDisablesAllStations(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{}
	arena.FieldEStop = mock

	mock.live = hardware.StopActive
	pollFieldEStop(arena)

	assert.True(t, arena.fieldEStopActive.Load())
	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s should be e-stopped", station)
	}
}

func TestFieldEStopFaultDisablesAllStations(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopFault, liveFault: hardware.FaultBothOpen}
	arena.FieldEStop = mock

	pollFieldEStop(arena)

	assert.True(t, arena.fieldEStopActive.Load())
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.fieldEStopFault.Load())
	for _, station := range allStationNames {
		assert.True(t, arena.AllianceStations[station].EStop.Load(), "station=%s should be e-stopped", station)
	}
}

func TestFieldEStopLatchPersistsAfterRelease(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopActive}
	arena.FieldEStop = mock

	pollFieldEStop(arena)
	mock.live = hardware.StopOK

	state, _ := arena.FieldEStop.State()
	assert.Equal(t, hardware.StopActive, state, "latch must persist after button release")
	assert.True(t, arena.fieldEStopActive.Load())
}

func TestFieldEStopClearReleasedButton(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopActive}
	arena.FieldEStop = mock
	pollFieldEStop(arena)

	// Release button and clear.
	mock.live = hardware.StopOK
	arena.ClearFieldEStop()

	assert.False(t, arena.fieldEStopActive.Load())
	for _, station := range allStationNames {
		assert.False(t, arena.AllianceStations[station].EStop.Load(), "station=%s should be cleared", station)
	}
}

func TestFieldEStopClearNoopWhileHeld(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopActive}
	arena.FieldEStop = mock
	pollFieldEStop(arena)

	// Try to clear while still held — should be no-op.
	arena.ClearFieldEStop()
	assert.True(t, arena.fieldEStopActive.Load(), "clear while held must be no-op")
}

func TestFieldEStopClearNoopWhileFaulted(t *testing.T) {
	arena := setupTestArena(t)
	mock := &mockFieldEStop{live: hardware.StopFault, liveFault: hardware.FaultBothOpen}
	arena.FieldEStop = mock
	pollFieldEStop(arena)

	// An operator must not be able to acknowledge away a fault that is still live.
	arena.ClearFieldEStop()
	assert.True(t, arena.fieldEStopActive.Load(), "clear while faulted must be no-op")
	assert.Equal(t, uint32(hardware.FaultBothOpen), arena.fieldEStopFault.Load())
}

func TestFieldEStopBlocksMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	arena.fieldEStopActive.Store(true)

	err := arena.checkCanStartMatch()
	assert.ErrorContains(t, err, "field emergency stop")
}

func TestFieldEStopFaultBlocksMatchStart(t *testing.T) {
	arena := setupTestArena(t)
	arena.fieldEStopFault.Store(uint32(hardware.FaultUnreachable))

	err := arena.checkCanStartMatch()
	assert.ErrorContains(t, err, "wiring fault")
}

// --- NoopFieldLights integration ---

func TestNoopFieldLightsIntegration(t *testing.T) {
	arena := setupTestArena(t)
	// Default is already Noop; confirm SetState never errors.
	states := []hardware.LightingState{
		{Phase: hardware.PhaseIdle},
		{Phase: hardware.PhaseAuto},
		{Phase: hardware.PhasePause},
		{Phase: hardware.PhaseTeleop, Shift: game.Shift1, AutoWinner: hardware.AllianceBlue},
		{Phase: hardware.PhaseFinished},
	}
	for _, s := range states {
		assert.NoError(t, arena.FieldLights.SetState(s))
	}
}
