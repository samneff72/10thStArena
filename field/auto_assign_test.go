// Copyright 2026 Team 254. All Rights Reserved.
//
// Tests for autoAssignTeam in arena.go.

package field

import (
	"github.com/Team254/cheesy-arena/model"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestAutoAssignTeamFallbackToR1(t *testing.T) {
	arena := setupTestArena(t)
	// Default arena has a test match loaded (ShouldAllowSubstitution = true) and PreMatch state.
	// Switch address is "" so GetStationForTeamId returns ""; falls back to R1.
	station := arena.autoAssignTeam(254)
	assert.Equal(t, "R1", station)
	assert.Equal(t, 254, arena.CurrentMatch.Red1)
	assert.NotNil(t, arena.AllianceStations["R1"].Team)
	assert.Equal(t, 254, arena.AllianceStations["R1"].Team.Id)
}

func TestAutoAssignTeamSequentialFill(t *testing.T) {
	arena := setupTestArena(t)
	// First team goes to R1, second to R2.
	assert.Equal(t, "R1", arena.autoAssignTeam(111))
	assert.Equal(t, "R2", arena.autoAssignTeam(222))
	assert.Equal(t, "R3", arena.autoAssignTeam(333))
	assert.Equal(t, "B1", arena.autoAssignTeam(444))
	assert.Equal(t, "B2", arena.autoAssignTeam(555))
	assert.Equal(t, "B3", arena.autoAssignTeam(666))
}

func TestAutoAssignTeamAllStationsOccupied(t *testing.T) {
	arena := setupTestArena(t)
	arena.autoAssignTeam(111)
	arena.autoAssignTeam(222)
	arena.autoAssignTeam(333)
	arena.autoAssignTeam(444)
	arena.autoAssignTeam(555)
	arena.autoAssignTeam(666)
	// All stations full; should return "".
	station := arena.autoAssignTeam(777)
	assert.Equal(t, "", station)
}

func TestAutoAssignTeamExistingDbRecordNotOverwritten(t *testing.T) {
	arena := setupTestArena(t)
	// Pre-create team with a custom WPA key.
	existing := &model.Team{Id: 254, WpaKey: "mykey1234"}
	assert.Nil(t, arena.Database.CreateTeam(existing))

	arena.autoAssignTeam(254)

	// Verify WPA key was not overwritten.
	team, err := arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.Equal(t, "mykey1234", team.WpaKey)
}

func TestAutoAssignTeamCreatesTeamWithPredictableWpaKey(t *testing.T) {
	arena := setupTestArena(t)
	arena.autoAssignTeam(254)

	team, err := arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.NotNil(t, team)
	assert.Equal(t, "00000254", team.WpaKey)
}

func TestAutoAssignTeamNotPreMatch(t *testing.T) {
	arena := setupTestArena(t)
	arena.MatchState = AutoPeriod
	station := arena.autoAssignTeam(254)
	assert.Equal(t, "", station)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
}

func TestAutoAssignTeamQualificationMatch(t *testing.T) {
	arena := setupTestArena(t)
	qualMatch := model.Match{Type: model.Qualification, ShortName: "Q1", LongName: "Qualification 1"}
	assert.Nil(t, arena.Database.CreateMatch(&qualMatch))
	assert.Nil(t, arena.LoadMatch(&qualMatch))

	station := arena.autoAssignTeam(254)
	assert.Equal(t, "", station)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
}
