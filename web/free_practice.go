// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for free practice mode.

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

// Shows the free practice operator interface.
func (web *Web) freePracticeHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	template, err := web.parseFiles("templates/free_practice.html", "templates/base.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	// Embed EventSettings so base.html can access .EventSettings.Name etc.
	// Also expose FreePracticeState so the template can inject it as a JS constant.
	data := struct {
		*model.EventSettings
		FreePracticeState    field.MatchState
		WarnExitFreePractice bool
	}{
		web.arena.EventSettings,
		field.FreePractice,
		r.URL.Query().Get("warn") == "1",
	}
	err = template.ExecuteTemplate(w, "base", data)
	if err != nil {
		handleWebErr(w, err)
		return
	}
}

// The websocket endpoint for the free practice operator UI.
func (web *Web) freePracticeWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	ws, err := websocket.NewWebsocket(w, r)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	defer ws.Close()

	// Push arena status updates to the client in real time.
	go ws.HandleNotifiers(web.arena.ArenaStatusNotifier)

	// Loop, waiting for commands from the operator UI.
	for {
		messageType, data, err := ws.Read()
		if err != nil {
			if err == io.EOF {
				return
			}
			log.Println(err)
			return
		}

		switch messageType {
		case "enterFreePractice":
			if err = web.arena.EnterFreePractice(); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}

		case "exitFreePractice":
			if err = web.arena.ExitFreePractice(); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}

		case "setSlot":
			args := struct {
				Station string
				TeamId  int
				WpaKey  string
			}{}
			if err = mapstructure.Decode(data, &args); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = web.arena.SetFreePracticeSlot(args.Station, args.TeamId, args.WpaKey); err != nil {
				ws.WriteError(err.Error())
				continue
			}

		case "clearSlot":
			station, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			if err = web.arena.ClearFreePracticeSlot(station); err != nil {
				ws.WriteError(err.Error())
				continue
			}

		case "toggleEStop":
			station, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			as, ok := web.arena.AllianceStations[station]
			if !ok {
				ws.WriteError(fmt.Sprintf("Invalid alliance station '%s'.", station))
				continue
			}
			as.EStop.Store(!as.EStop.Load())
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}

		case "clearFieldEStop":
			web.arena.ClearFieldEStop()
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}

		default:
			ws.WriteError(fmt.Sprintf("Invalid message type '%s'.", messageType))
		}
	}
}
