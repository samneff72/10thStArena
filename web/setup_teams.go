// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for managing teams.

package web

import (
	"net/http"
	"strconv"

	"github.com/Team254/cheesy-arena/model"
)

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
		Id:              teamId,
		Name:            r.PostFormValue("name"),
		Nickname:        r.PostFormValue("nickname"),
		City:            r.PostFormValue("city"),
		StateProv:       r.PostFormValue("stateProv"),
		Country:         r.PostFormValue("country"),
		SchoolName:      r.PostFormValue("schoolName"),
		RobotName:       r.PostFormValue("robotName"),
		Accomplishments: r.PostFormValue("accomplishments"),
		WpaKey:          r.PostFormValue("wpaKey"),
		HasConnected:    r.PostFormValue("hasConnected") == "on",
		FtaNotes:        r.PostFormValue("ftaNotes"),
	}
	team.RookieYear, _ = strconv.Atoi(r.PostFormValue("rookieYear"))

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
	team.Nickname = r.PostFormValue("nickname")
	team.City = r.PostFormValue("city")
	team.StateProv = r.PostFormValue("stateProv")
	team.Country = r.PostFormValue("country")
	team.SchoolName = r.PostFormValue("schoolName")
	team.RookieYear, _ = strconv.Atoi(r.PostFormValue("rookieYear"))
	team.RobotName = r.PostFormValue("robotName")
	team.Accomplishments = r.PostFormValue("accomplishments")
	team.WpaKey = r.PostFormValue("wpaKey")
	team.HasConnected = r.PostFormValue("hasConnected") == "on"
	team.FtaNotes = r.PostFormValue("ftaNotes")

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
