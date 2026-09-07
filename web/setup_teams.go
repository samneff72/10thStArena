// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for managing teams.

package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/team841/bioarena/model"
)

// Returns a single team as JSON.
func (web *Web) teamGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		handleWebErr(w, err)
		return
	}

	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, "Team not found.", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err = json.NewEncoder(w).Encode(struct {
		Id     int    `json:"id"`
		Name   string `json:"name"`
		WpaKey string `json:"wpaKey"`
	}{team.Id, team.Name, team.WpaKey}); err != nil {
		handleWebErr(w, err)
	}
}

// Creates a new team with just an ID; used by the free practice UI when an anonymous team is encountered.
// Returns 201 on success, 409 if the team already exists.
func (web *Web) teamQuickAddHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PostFormValue("id"))
	if err != nil || teamId <= 0 {
		http.Error(w, "Team number must be a positive integer.", http.StatusBadRequest)
		return
	}

	existingTeam, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if existingTeam != nil {
		http.Error(w, "A team with that number already exists.", http.StatusConflict)
		return
	}

	// Given a WPA key up front, derived from the team number: zero-padded to eight digits,
	// which is WPA2's minimum length. A team created without one has no working WiFi, and
	// the operator has no way to know that until a robot fails to associate. Predictable
	// rather than random so it can be read off the team number at the driver station.
	if err = web.arena.Database.CreateTeam(
		&model.Team{Id: teamId, WpaKey: fmt.Sprintf("%08d", teamId)},
	); err != nil {
		handleWebErr(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// Shows the team list page.
func (web *Web) teamsGetHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	web.renderTeams(w, r, "")
}

// Creates a new team.
func (web *Web) teamsAddHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PostFormValue("id"))
	if err != nil || teamId <= 0 {
		web.renderTeams(w, r, "Team number must be a positive integer.")
		return
	}

	existingTeam, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if existingTeam != nil {
		web.renderTeams(w, r, "A team with that number already exists.")
		return
	}

	team := model.Team{
		Id:     teamId,
		Name:   r.PostFormValue("name"),
		WpaKey: r.PostFormValue("wpaKey"),
	}

	if err = web.arena.Database.CreateTeam(&team); err != nil {
		handleWebErr(w, err)
		return
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

// Updates an existing team.
func (web *Web) teamsEditHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		handleWebErr(w, err)
		return
	}

	team, err := web.arena.Database.GetTeamById(teamId)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	if team == nil {
		http.Error(w, "Team not found.", http.StatusNotFound)
		return
	}

	team.Name = r.PostFormValue("name")
	team.WpaKey = r.PostFormValue("wpaKey")

	if err = web.arena.Database.UpdateTeam(team); err != nil {
		handleWebErr(w, err)
		return
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

// Deletes a team.
func (web *Web) teamsDeleteHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	teamId, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		handleWebErr(w, err)
		return
	}

	if err = web.arena.Database.DeleteTeam(teamId); err != nil {
		handleWebErr(w, err)
		return
	}

	http.Redirect(w, r, "/setup/teams", 303)
}

func (web *Web) renderTeams(w http.ResponseWriter, r *http.Request, errorMessage string) {
	template, err := web.parseFiles("templates/setup_teams.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	teams, err := web.arena.Database.GetAllTeams()
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		Teams        []model.Team
		ErrorMessage string
	}{web.arena.EventSettings, teams, errorMessage}
	if err = template.ExecuteTemplate(w, "base", data); err != nil {
		handleWebErr(w, err)
		return
	}
}
