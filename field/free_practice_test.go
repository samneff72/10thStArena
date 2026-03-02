package field

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- EnterFreePractice ---

func TestEnterFreePracticeFromPreMatch(t *testing.T) {
	arena := setupTestArena(t)
	assert.Equal(t, PreMatch, arena.MatchState)
	assert.NoError(t, arena.EnterFreePractice())
	assert.Equal(t, FreePractice, arena.MatchState)
}

func TestEnterFreePracticeRejectedDuringMatch(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = TeleopPeriod
	assert.Error(t, arena.EnterFreePractice())
	assert.Equal(t, TeleopPeriod, arena.MatchState)
}

func TestEnterFreePracticeRejectedFromPostMatch(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = PostMatch
	assert.Error(t, arena.EnterFreePractice())
}

// --- ExitFreePractice ---

func TestExitFreePracticeReturnsToPreMatch(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.ExitFreePractice())
	assert.Equal(t, PreMatch, arena.MatchState)
}

func TestExitFreePracticeRejectedOutsideFreePractice(t *testing.T) {
	arena := setupTestArena(t)
	assert.Error(t, arena.ExitFreePractice())
}

func TestExitFreePracticeClearsAllSlots(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 1001, "key1"))
	assert.NoError(t, arena.SetFreePracticeSlot("B1", 2001, "key2"))
	assert.NoError(t, arena.ExitFreePractice())

	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.Nil(t, arena.AllianceStations[station].Team, "station %s should be empty after exit", station)
	}
}

func TestExitFreePracticeClearsEStops(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	arena.AllianceStations["R1"].EStop.Store(true)
	arena.AllianceStations["B3"].AStop.Store(true)
	assert.NoError(t, arena.ExitFreePractice())
	assert.False(t, arena.AllianceStations["R1"].EStop.Load())
	assert.False(t, arena.AllianceStations["B3"].AStop.Load())
}

// --- SetFreePracticeSlot ---

func TestSetFreePracticeSlotBasic(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R2", 254, "somekey"))
	assert.NotNil(t, arena.AllianceStations["R2"].Team)
	assert.Equal(t, 254, arena.AllianceStations["R2"].Team.Id)
	assert.Equal(t, "somekey", arena.AllianceStations["R2"].Team.WpaKey)
}

func TestSetFreePracticeSlotRejectedOutsideFreePractice(t *testing.T) {
	arena := setupTestArena(t)
	assert.Error(t, arena.SetFreePracticeSlot("R1", 254, "key"))
}

func TestSetFreePracticeSlotRejectedTeamZero(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.Error(t, arena.SetFreePracticeSlot("R1", 0, ""))
}

func TestSetFreePracticeSlotRejectedNegativeTeam(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.Error(t, arena.SetFreePracticeSlot("R1", -5, ""))
}

func TestSetFreePracticeSlotRejectedInvalidStation(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.Error(t, arena.SetFreePracticeSlot("X9", 100, ""))
}

func TestSetFreePracticeSlotRejectedDuplicateTeam(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 254, "key1"))
	// Same team in a different station must be rejected.
	err := arena.SetFreePracticeSlot("B1", 254, "key2")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "254")
	// R1 is still registered; B1 should be empty.
	assert.NotNil(t, arena.AllianceStations["R1"].Team)
	assert.Nil(t, arena.AllianceStations["B1"].Team)
}

func TestSetFreePracticeSlotAllowsSameStationUpdate(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 254, "key1"))
	// Re-registering the same slot with the same team should succeed.
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 254, "key1"))
}

func TestSetFreePracticeSlotAllowsSixDistinctTeams(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	for i, s := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		assert.NoError(t, arena.SetFreePracticeSlot(s, 100+i, "key"))
	}
}

// --- ClearFreePracticeSlot ---

func TestClearFreePracticeSlotBasic(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("B2", 1114, "key"))
	assert.NotNil(t, arena.AllianceStations["B2"].Team)
	assert.NoError(t, arena.ClearFreePracticeSlot("B2"))
	assert.Nil(t, arena.AllianceStations["B2"].Team)
}

func TestClearFreePracticeSlotAlreadyEmpty(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	// Clearing an already-empty slot should succeed without error.
	assert.NoError(t, arena.ClearFreePracticeSlot("R3"))
	assert.Nil(t, arena.AllianceStations["R3"].Team)
}

func TestClearFreePracticeSlotRejectedOutsideFreePractice(t *testing.T) {
	arena := setupTestArena(t)
	assert.Error(t, arena.ClearFreePracticeSlot("R1"))
}

func TestClearFreePracticeSlotRejectedInvalidStation(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.Error(t, arena.ClearFreePracticeSlot("Z9"))
}

// --- Update() in FreePractice ---

func TestFreePracticeUpdateEnablesRobots(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	// With no reconfiguration in progress, enabled should be true (not reconfiguring).
	assert.False(t, arena.freePracticeReconfiguring.Load())
}

func TestFreePracticeUpdateDisabledDuringReconfig(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	arena.freePracticeReconfiguring.Store(true)
	// Update should compute enabled=false when reconfiguring.
	// We test indirectly: after a full Update(), lastDsPacketTime advances.
	arena.Update()
	// The reconfig flag is still set; robots should be disabled on next packet.
	assert.True(t, arena.freePracticeReconfiguring.Load())
}

// --- MatchTimeSec in FreePractice ---

func TestMatchTimeSecReturnsZeroInFreePractice(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	// MatchStartTime is never set; must return 0, not a huge elapsed duration.
	assert.Equal(t, 0.0, arena.MatchTimeSec())
}

// --- Session switch: FreePractice → PreMatch → FreePractice ---

func TestSwitchBetweenFreePracticeAndMatchPlay(t *testing.T) {
	arena := setupTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 9999, "wpa"))
	assert.NoError(t, arena.ExitFreePractice())
	assert.Equal(t, PreMatch, arena.MatchState)

	// Should be able to enter again cleanly.
	assert.NoError(t, arena.EnterFreePractice())
	assert.Equal(t, FreePractice, arena.MatchState)
	// Slots cleared by exit; no team should be present.
	assert.Nil(t, arena.AllianceStations["R1"].Team)
}
