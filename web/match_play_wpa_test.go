package web

import (
	"testing"

	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
	gorillawebsocket "github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
)

// Match Play registers the WPA key alongside the team number, so a match can be set up
// without going to the Free Practice page for the key. The key is what the radio is
// configured with, so a station registered without one has no working WiFi.
func TestMatchPlayRegisterTeamsAppliesWpaKeys(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, WpaKey: "oldkey254"}))
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 1114, WpaKey: "keep1114"}))

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/match_play/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)
	readWebsocketMultiple(t, ws, 4)

	ws.Write("registerTeams", map[string]any{
		"Red1": 254, "Red2": 0, "Red3": 0, "Blue1": 1114, "Blue2": 0, "Blue3": 0,
		// B1's key is blank: an untouched box must leave the team's existing key alone
		// rather than erasing it.
		"WpaKeys": map[string]string{"R1": "newkey254", "B1": ""},
	})
	readWebsocketType(t, ws, "matchLoad")

	red1, err := web.arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.Equal(t, "newkey254", red1.WpaKey, "a typed key should be stored")

	blue1, err := web.arena.Database.GetTeamById(1114)
	assert.Nil(t, err)
	assert.Equal(t, "keep1114", blue1.WpaKey, "a blank box should not erase an existing key")
}

// Registering with no keys at all must still work: the payload is optional, and an older
// page or a scripted client sends only the team numbers.
func TestMatchPlayRegisterTeamsWithoutWpaKeys(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, WpaKey: "untouched"}))

	server, wsUrl := web.startTestServer()
	defer server.Close()
	conn, _, err := gorillawebsocket.DefaultDialer.Dial(wsUrl+"/match_play/websocket", nil)
	assert.Nil(t, err)
	defer conn.Close()
	ws := websocket.NewTestWebsocket(conn)
	readWebsocketMultiple(t, ws, 4)

	ws.Write("registerTeams", map[string]int{
		"Red1": 254, "Red2": 0, "Red3": 0, "Blue1": 0, "Blue2": 0, "Blue3": 0,
	})
	readWebsocketType(t, ws, "matchLoad")

	assert.Equal(t, 254, web.arena.CurrentMatch.Red1)
	team, err := web.arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.Equal(t, "untouched", team.WpaKey)
}
