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

	"github.com/mitchellh/mapstructure"
	"github.com/team841/bioarena/field"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/websocket"
)

var testMatchCounter int

// Shows the match play control interface.
func (web *Web) matchPlayHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	// Opening a page is what selects it for every kiosk. A display that follows lands
	// back here and sets the same value, which is why the setter ignores no-ops.
	web.arena.SetCurrentView(field.ViewMatchPlay)

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
		// Upstream delivers sound cues to the audience display, which this fork does not
		// have. Without this subscription the notifier fires into nothing and the field is
		// silent, however the sounds themselves are configured.
		web.arena.PlaySoundNotifier,
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
				// Keyed by station, since that is what the operator typed into. Mapped onto
				// team numbers below, because a key belongs to a team and not to a seat.
				WpaKeys map[string]string
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}

			// Stored before the substitution, so the network configuration it triggers
			// carries the new keys to the access point. Doing it after would leave the
			// radios on the previous keys until something else reconfigured them.
			if len(args.WpaKeys) > 0 {
				teamForStation := map[string]int{
					"R1": args.Red1, "R2": args.Red2, "R3": args.Red3,
					"B1": args.Blue1, "B2": args.Blue2, "B3": args.Blue3,
				}
				keys := make(map[int]string, len(args.WpaKeys))
				for station, key := range args.WpaKeys {
					if teamId, ok := teamForStation[station]; ok {
						keys[teamId] = key
					}
				}
				if err = web.arena.SetTeamWpaKeys(keys); err != nil {
					ws.WriteError(err.Error())
					continue
				}
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
		// Applied the moment the box is toggled, not only at match start. Upstream sets the
		// flag from the startMatch payload alone, which was survivable while the default was
		// unmuted -- an explicit cue before the first match of a session still played. This
		// fork defaults to muted, so without this the checkbox would appear to do nothing
		// until a match ran, and an abort cue would be silent no matter what it showed.
		case "setMuteMatchSounds":
			args := struct {
				MuteMatchSounds bool
			}{}
			if err = mapstructure.Decode(data, &args); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			web.arena.SetMuteMatchSounds(args.MuteMatchSounds)
		case "startMatch":
			args := struct {
				MuteMatchSounds bool
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			web.arena.SetMuteMatchSounds(args.MuteMatchSounds)
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
		case "bypassEmptyStations":
			count := web.arena.BypassEmptyStations()
			log.Printf("Bypassed %d empty station(s)", count)
			if err = ws.WriteNotifier(web.arena.ArenaStatusNotifier); err != nil {
				log.Println(err)
			}
		case "setAutoWinnerMode":
			name, ok := data.(string)
			if !ok {
				ws.WriteError(fmt.Sprintf("Failed to parse '%s' message.", messageType))
				continue
			}
			mode, err := field.ParseAutoWinnerMode(name)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if err = web.arena.SetAutoWinnerMode(mode); err != nil {
				ws.WriteError(err.Error())
				continue
			}
			web.arena.MatchLoadNotifier.Notify()
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
