// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.

package web

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/team841/bioarena/model"
	"github.com/stretchr/testify/assert"
)

func TestTeamGetHandlerSuccess(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254, Name: "The Cheesy Poofs", WpaKey: "testkey"}))

	recorder := web.getHttpResponse("/setup/teams/254")
	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))

	var result struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		WpaKey string `json:"wpaKey"`
	}
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, 254, result.Id)
	assert.Equal(t, "The Cheesy Poofs", result.Name)
	assert.Equal(t, "testkey", result.WpaKey)
}

func TestTeamGetHandlerNotFound(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.getHttpResponse("/setup/teams/9999")
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestTeamQuickAddHandler(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.postHttpResponse("/setup/teams/quick-add", "id=254")
	assert.Equal(t, http.StatusCreated, recorder.Code)

	team, err := web.arena.Database.GetTeamById(254)
	assert.Nil(t, err)
	assert.NotNil(t, team)
	assert.Equal(t, 254, team.Id)
	assert.Equal(t, "", team.Name)

	// A key derived from the team number, zero-padded to WPA2's eight-character minimum.
	// Created without one, the team has no working WiFi and nothing tells the operator so
	// until a robot fails to associate.
	assert.Equal(t, "00000254", team.WpaKey)
}

func TestTeamQuickAddHandlerDuplicate(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 254}))

	recorder := web.postHttpResponse("/setup/teams/quick-add", "id=254")
	assert.Equal(t, http.StatusConflict, recorder.Code)
}

func TestTeamQuickAddHandlerInvalidId(t *testing.T) {
	web := setupTestWeb(t)

	recorder := web.postHttpResponse("/setup/teams/quick-add", "id=0")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	recorder = web.postHttpResponse("/setup/teams/quick-add", "id=notanumber")
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestTeamGetHandlerNoWpaKey(t *testing.T) {
	web := setupTestWeb(t)
	assert.Nil(t, web.arena.Database.CreateTeam(&model.Team{Id: 100, Name: "No Key Team", WpaKey: ""}))

	recorder := web.getHttpResponse("/setup/teams/100")
	assert.Equal(t, http.StatusOK, recorder.Code)

	var result struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		WpaKey string `json:"wpaKey"`
	}
	assert.Nil(t, json.Unmarshal(recorder.Body.Bytes(), &result))
	assert.Equal(t, 100, result.Id)
	assert.Equal(t, "", result.WpaKey)
}
