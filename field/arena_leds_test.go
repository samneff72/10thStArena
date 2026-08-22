package field

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/led"
	"github.com/team841/bioarena/model"
)

// teleopOffset returns a match start time placing the given remaining teleop seconds.
func teleopOffset(remaining int) time.Time {
	elapsed := game.GetDurationToTeleopStart() +
		time.Duration(game.MatchTiming.TeleopDurationSec-remaining)*time.Second
	return time.Now().Add(-elapsed)
}

// The dark HUB is the one whose alliance is inactive for the current shift, matching
// Table 6-3. Red winning AUTO means red is dark for shifts 1 and 3.
func TestUpdateHubLedsAlternatesWithAutoWinner(t *testing.T) {
	for _, c := range []struct {
		remaining int
		shift     string
		redMode   led.Mode
		blueMode  led.Mode
	}{
		{120, "Shift1", led.OffMode, led.BlueMode},
		{95, "Shift2", led.RedMode, led.OffMode},
		{70, "Shift3", led.OffMode, led.BlueMode},
		{45, "Shift4", led.RedMode, led.OffMode},
		{20, "Endgame", led.RedMode, led.BlueMode},
	} {
		arena := setupTestArena(t)
		arena.AutoWinner = hardware.AllianceRed
		arena.MatchState = TeleopPeriod
		arena.MatchStartTime = teleopOffset(c.remaining)

		arena.updateTeleopHubLeds(time.Now())

		redMode, blueMode := arena.Leds.GetModes()
		assert.Equal(t, c.redMode, redMode, "%s red", c.shift)
		assert.Equal(t, c.blueMode, blueMode, "%s blue", c.shift)
	}
}

// Reversing the AUTO winner reverses which HUB is dark.
func TestUpdateHubLedsReversesForBlueAutoWinner(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceBlue
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(120) // Shift1

	arena.updateTeleopHubLeds(time.Now())

	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedMode, redMode)
	assert.Equal(t, led.OffMode, blueMode)
}

// The HUB about to go dark pulses during the 3s window before the boundary.
func TestUpdateHubLedsPulsesBeforeDeactivation(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed
	arena.MatchState = TeleopPeriod

	// Two seconds before the Shift1 boundary: blue is active now and goes dark next,
	// so blue pulses.
	arena.MatchStartTime = teleopOffset(107)
	arena.updateTeleopHubLeds(time.Now())
	_, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.BluePulseMode, blueMode)
}

// The transition shift runs the advantage sweep on the AUTO winner's HUB.
func TestUpdateHubLedsTransitionAdvantage(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(137) // transition shift, outside the warning window

	arena.updateTeleopHubLeds(time.Now())

	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedAdvantageMode, redMode)
	assert.Equal(t, led.BlueMode, blueMode)
}

// With single-pixel fixtures the startup fill is meaningless, so the solid fallback
// must replace it with each zone's own alliance colour before it reaches the controller.
func TestHubLedFallbackSubstitutesPerPixelSequences(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = AutoPeriod
	arena.MatchStartTime = time.Now()

	arena.hubLedFallback = led.FallbackFull
	arena.updateHubLeds(time.Now())
	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedStartupMode, redMode)
	assert.Equal(t, led.BlueStartupMode, blueMode)

	arena.hubLedFallback = led.FallbackSolid
	arena.updateHubLeds(time.Now())
	redMode, blueMode = arena.Leds.GetModes()
	assert.Equal(t, led.RedMode, redMode)
	assert.Equal(t, led.BlueMode, blueMode)
}

// Binary fixtures cannot dim, so the pre-deactivation pulse becomes solid.
func TestHubLedBinaryFallbackFlattensPulse(t *testing.T) {
	arena := setupTestArena(t)
	arena.AutoWinner = hardware.AllianceRed
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(107) // 2s before the Shift1 boundary

	arena.hubLedFallback = led.FallbackSolid
	arena.updateTeleopHubLeds(time.Now())
	_, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.BluePulseMode, blueMode)

	arena.hubLedFallback = led.FallbackBinary
	arena.updateTeleopHubLeds(time.Now())
	_, blueMode = arena.Leds.GetModes()
	assert.Equal(t, led.BlueMode, blueMode)
}

// Settings drive the controller, and a bad layout must not stop the arena loading.
func TestApplyHubLedSettings(t *testing.T) {
	arena := setupTestArena(t)

	arena.applyHubLedSettings(&model.EventSettings{
		HubLedsSimplified:   true,
		HubLedsFallback:     "solid",
		HubLedsRedUniverse:  1,
		HubLedsRedAddress:   1,
		HubLedsBlueUniverse: 1,
		HubLedsBlueAddress:  25,
	})
	assert.Equal(t, led.FallbackSolid, arena.hubLedFallback)

	// An overlapping layout is rejected and logged; the fallback still applies and the
	// previous layout is kept rather than leaving the field half-configured.
	arena.applyHubLedSettings(&model.EventSettings{
		HubLedsSimplified:   true,
		HubLedsFallback:     "binary",
		HubLedsRedUniverse:  1,
		HubLedsRedAddress:   1,
		HubLedsBlueUniverse: 1,
		HubLedsBlueAddress:  4,
	})
	assert.Equal(t, led.FallbackBinary, arena.hubLedFallback)

	// An unrecognised fallback degrades to full rather than erroring out.
	arena.applyHubLedSettings(&model.EventSettings{HubLedsFallback: "sparkly"})
	assert.Equal(t, led.FallbackFull, arena.hubLedFallback)
}

// AUTO runs the startup fill on both HUBs.
func TestUpdateHubLedsAutoStartup(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = AutoPeriod
	arena.MatchStartTime = time.Now()

	arena.updateHubLeds(time.Now())

	redMode, blueMode := arena.Leds.GetModes()
	assert.Equal(t, led.RedStartupMode, redMode)
	assert.Equal(t, led.BlueStartupMode, blueMode)
}

// With no sACN address configured the controller runs its sequences but sends nothing,
// so the match loop must not error.
func TestUpdateHubLedsNoAddressDoesNotError(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = teleopOffset(100)

	assert.NotPanics(t, func() { arena.updateHubLeds(time.Now()) })
}

// The checkbox picks the wire protocol. The two speak different ports, so switching means
// a new controller rather than a new field on the old one.
func TestHubLedProtocolSwitch(t *testing.T) {
	arena := setupTestArena(t)

	_, isSacn := arena.Leds.(*led.Controller)
	assert.True(t, isSacn, "sACN is the default")

	arena.EventSettings.HubLedsArtNet = true
	arena.applyHubLedSettings(arena.EventSettings)
	_, isArtNet := arena.Leds.(*led.ArtNetController)
	assert.True(t, isArtNet, "ticking the box should switch to Art-Net")

	arena.EventSettings.HubLedsArtNet = false
	arena.applyHubLedSettings(arena.EventSettings)
	_, isSacn = arena.Leds.(*led.Controller)
	assert.True(t, isSacn, "unticking it should switch back")
}
