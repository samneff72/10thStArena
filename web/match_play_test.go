// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package web

import (
	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/hardware"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"testing"
)

// mockWebFieldEStop is a controllable FieldEStopPanel for web handler tests.
type mockWebFieldEStop struct {
	pinHeld   bool
	triggered bool
}

func (m *mockWebFieldEStop) Triggered() bool {
	if m.pinHeld {
		m.triggered = true
	}
	return m.triggered
}
func (m *mockWebFieldEStop) Clear() {
	if !m.pinHeld {
		m.triggered = false
	}
}

func TestMatchPlay(t *testing.T) {
	web := setupTestWeb(t)

	// Check that the page renders.
	recorder := web.getHttpResponse("/match_play")
	assert.Equal(t, 200, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "Match Play")
}

func TestMatchPlayMatchLoadRouteRemoved(t *testing.T) {
	web := setupTestWeb(t)

	// The match_load route was removed; it should 404.
	recorder := web.getHttpResponse("/match_play/match_load")
	assert.Equal(t, 302, recorder.Code)
}

func TestMatchPlayWebsocketCommands(t *testing.T) {
	web := setupTestWeb(t)
	web.arena.Database.CreateTeam(&model.Team{Id: 254})

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/match_play/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	// Should get a few status updates right after connection.
	readWebsocketType(t, ws, "matchTiming")
	readWebsocketType(t, ws, "arenaStatus")
	readWebsocketType(t, ws, "matchLoad")
	readWebsocketType(t, ws, "matchTime")

	// Test that a server-side error is communicated to the client.
	ws.Write("nonexistenttype", nil)
	assert.Contains(t, readWebsocketError(t, ws), "Invalid message type")

	// Test match setup commands.
	ws.Write("substituteTeams", map[string]int{"Red1": 0, "Red2": 0, "Red3": 0, "Blue1": 1, "Blue2": 0, "Blue3": 0})
	assert.Equal(t, readWebsocketError(t, ws), "Team 1 is not present at the event.")
	ws.Write("substituteTeams", map[string]int{"Red1": 0, "Red2": 0, "Red3": 0, "Blue1": 254, "Blue2": 0, "Blue3": 0})
	readWebsocketType(t, ws, "matchLoad")
	assert.Equal(t, 254, web.arena.CurrentMatch.Blue1)
	ws.Write("substituteTeams", map[string]int{"Red1": 0, "Red2": 0, "Red3": 0, "Blue1": 0, "Blue2": 0, "Blue3": 0})
	readWebsocketType(t, ws, "matchLoad")
	assert.Equal(t, 0, web.arena.CurrentMatch.Blue1)
	ws.Write("toggleBypass", nil)
	assert.Contains(t, readWebsocketError(t, ws), "Failed to parse")
	ws.Write("toggleBypass", "R4")
	assert.Contains(t, readWebsocketError(t, ws), "Invalid alliance station")
	ws.Write("toggleBypass", "R3")
	readWebsocketType(t, ws, "arenaStatus")
	assert.Equal(t, true, web.arena.AllianceStations["R3"].Bypass.Load())
	ws.Write("toggleBypass", "R3")
	readWebsocketType(t, ws, "arenaStatus")
	assert.Equal(t, false, web.arena.AllianceStations["R3"].Bypass.Load())

	// Go through match flow.
	ws.Write("abortMatch", nil)
	assert.Contains(t, readWebsocketError(t, ws), "cannot abort match")
	ws.Write("startMatch", nil)
	assert.Contains(t, readWebsocketError(t, ws), "cannot start match")
	web.arena.AllianceStations["R1"].Bypass.Store(true)
	web.arena.AllianceStations["R2"].Bypass.Store(true)
	web.arena.AllianceStations["R3"].Bypass.Store(true)
	web.arena.AllianceStations["B1"].Bypass.Store(true)
	web.arena.AllianceStations["B2"].Bypass.Store(true)
	web.arena.AllianceStations["B3"].Bypass.Store(true)
	ws.Write("startMatch", nil)
	readWebsocketType(t, ws, "arenaStatus")
	assert.Equal(t, field.StartMatch, web.arena.MatchState)
	ws.Write("commitResults", nil)
	assert.Contains(t, readWebsocketError(t, ws), "cannot commit match while it is in progress")
	ws.Write("discardResults", nil)
	assert.Contains(t, readWebsocketError(t, ws), "cannot reset match while it is in progress")
	ws.Write("abortMatch", nil)
	readWebsocketType(t, ws, "arenaStatus")
	assert.Equal(t, field.PostMatch, web.arena.MatchState)
	ws.Write("commitResults", nil)
	readWebsocketType(t, ws, "matchLoad")
	assert.Equal(t, field.PreMatch, web.arena.MatchState)
	ws.Write("discardResults", nil)
	readWebsocketType(t, ws, "matchLoad")
	assert.Equal(t, field.PreMatch, web.arena.MatchState)
}

func TestMatchPlayWebsocketLoadMatch(t *testing.T) {
	web := setupTestWeb(t)

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/match_play/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	// Should get a few status updates right after connection.
	readWebsocketMultiple(t, ws, 4)

	// loadMatch always loads a fresh test match regardless of any payload.
	ws.Write("loadMatch", nil)
	readWebsocketType(t, ws, "matchLoad")
	assert.Equal(t, model.Test, web.arena.CurrentMatch.Type)
	assert.Equal(t, 0, web.arena.CurrentMatch.Red1)
	assert.Equal(t, 0, web.arena.CurrentMatch.Blue1)
}

func TestMatchPlayClearFieldEStop(t *testing.T) {
	web := setupTestWeb(t)
	mock := &mockWebFieldEStop{}
	web.arena.FieldEStop = mock

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/match_play/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	// Drain initial messages.
	readWebsocketType(t, ws, "matchTiming")
	readWebsocketType(t, ws, "arenaStatus")
	readWebsocketType(t, ws, "matchLoad")
	readWebsocketType(t, ws, "matchTime")

	// Simulate a field e-stop: latch all stations.
	mock.pinHeld = true
	web.arena.FieldEStop.Triggered()
	web.arena.AllianceStations["R1"].EStop.Store(true)
	web.arena.AllianceStations["B1"].EStop.Store(true)

	// clearFieldEStop while button still held — latch must stay active.
	ws.Write("clearFieldEStop", nil)
	readWebsocketType(t, ws, "arenaStatus")
	assert.True(t, mock.triggered, "latch should persist while button held")

	// Release button, then clear — stations should be cleared.
	mock.pinHeld = false
	ws.Write("clearFieldEStop", nil)
	readWebsocketType(t, ws, "arenaStatus")
	assert.False(t, mock.triggered, "latch should clear after button release")
	assert.False(t, web.arena.AllianceStations["R1"].EStop.Load())
	assert.False(t, web.arena.AllianceStations["B1"].EStop.Load())
}

func TestMatchPlayArenaStatusIncludesGpioFieldEStop(t *testing.T) {
	// Verify that the arenaStatus WebSocket message includes the GpioFieldEStopActive field.
	web := setupTestWeb(t)
	web.arena.FieldEStop = &hardware.NoopFieldEStopPanel{}

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/match_play/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)

	// Read all four initial messages and extract arenaStatus.
	messages := readWebsocketMultiple(t, ws, 4)
	arenaStatus, ok := messages["arenaStatus"]
	if !assert.True(t, ok, "arenaStatus not found in initial messages") {
		return
	}
	statusMap, ok := arenaStatus.(map[string]interface{})
	if !assert.True(t, ok, "arenaStatus data is not a map") {
		return
	}
	_, exists := statusMap["GpioFieldEStopActive"]
	assert.True(t, exists, "arenaStatus payload must include GpioFieldEStopActive field")
	assert.Equal(t, false, statusMap["GpioFieldEStopActive"],
		"GpioFieldEStopActive must be false when noop panel is installed")
}
