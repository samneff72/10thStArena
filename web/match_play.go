// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for controlling match play.

package web

import (
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/team841/bioarena/field"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
	"github.com/mitchellh/mapstructure"
)

var testMatchCounter int

// Shows the match play control interface.
func (web *Web) matchPlayHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	if web.arena.MatchState == field.FreePractice {
		http.Redirect(w, r, "/free_practice?warn=1", http.StatusSeeOther)
		return
	}

	template, err := web.parseFiles("templates/match_play.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		*model.EventSettings
		PlcIsEnabled          bool
		PlcArmorBlockStatuses map[string]bool
	}{
		web.arena.EventSettings,
		web.arena.Plc.IsEnabled(),
		web.arena.Plc.GetArmorBlockStatuses(),
	}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the match play client to send control commands and receive status updates.
func (web *Web) matchPlayWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()

	// Subscribe the websocket to the notifiers whose messages will be passed on to the client, in a separate goroutine.
	go ws.HandleNotifiers(
		web.arena.MatchTimingNotifier,
		web.arena.ArenaStatusNotifier,
		web.arena.MatchLoadNotifier,
		web.arena.MatchTimeNotifier,
	)

	// Loop, waiting for commands and responding to them, until the client closes the connection.
	for {
		messageType, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				// Client has closed the connection; nothing to do here.
				return
			}
			log.Println(err)
			return
		}

		switch messageType {
		case "registerTeams":
			args := struct {
				Red1  int
				Red2  int
				Red3  int
				Blue1 int
				Blue2 int
				Blue3 int
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			err = web.arena.SubstituteTeams(args.Red1, args.Red2, args.Red3, args.Blue1, args.Blue2, args.Blue3)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
		case "toggleBypass":
			station, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			if _, ok := web.arena.AllianceStations[station]; !ok {
				ws.WriteError(fmt.Sprintf("Invalid alliance station '%s'.", station))
				continue
			}
			as := web.arena.AllianceStations[station]
		as.Bypass.Store(!as.Bypass.Load())
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}
		case "toggleEStop":
			station, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			if _, ok := web.arena.AllianceStations[station]; !ok {
				ws.WriteError(fmt.Sprintf("Invalid alliance station '%s'.", station))
				continue
			}
			as := web.arena.AllianceStations[station]
			as.EStop.Store(!as.EStop.Load())
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}
		case "startMatch":
			args := struct {
				MuteMatchSounds bool
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			web.arena.MuteMatchSounds = args.MuteMatchSounds
			err = web.arena.StartMatch()
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}
		case "abortMatch":
			err = web.arena.AbortMatch()
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}
		case "clearFieldEStop":
			web.arena.ClearFieldEStop()
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}
		case "clearMatch":
			err = web.arena.ClearMatch()
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			testMatchCounter++
			log.Printf("Loading test match #%d", testMatchCounter)
		case "setTestMatchName":
			if web.arena.CurrentMatch.Type != model.Test {
				// Don't allow changing the name of a non-test match.
				continue
			}
			name, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			web.arena.CurrentMatch.LongName = name
			web.arena.MatchLoadNotifier.Notify()
		default:
			ws.WriteError(fmt.Sprintf("Invalid message type '%s'.", messageType))
		}
	}
}

