// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Web routes for controlling match play.

package web

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"

	"github.com/Team254/cheesy-arena/field"
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
	"github.com/mitchellh/mapstructure"
)

type MatchPlayListItem struct {
	Id         int
	ShortName  string
	Time       string
	Status     game.MatchStatus
	ColorClass string
}

type MatchPlayList []MatchPlayListItem

// Shows the match play control interface.
func (web *Web) matchPlayHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
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

// Renders a partial template containing the list of matches.
func (web *Web) matchPlayMatchLoadHandler(w http.ResponseWriter, r *http.Request) {
	if !web.userIsAdmin(w, r) {
		return
	}

	practiceMatches, err := web.buildMatchPlayList(model.Practice)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	qualificationMatches, err := web.buildMatchPlayList(model.Qualification)
	if err != nil {
		handleWebErr(w, err)
		return
	}
	playoffMatches, err := web.buildMatchPlayList(model.Playoff)
	if err != nil {
		handleWebErr(w, err)
		return
	}

	matchesByType := map[model.MatchType]MatchPlayList{
		model.Practice:      practiceMatches,
		model.Qualification: qualificationMatches,
		model.Playoff:       playoffMatches,
	}
	currentMatchType := web.arena.CurrentMatch.Type
	if currentMatchType == model.Test {
		currentMatchType = model.Practice
	}

	template, err := web.parseFiles("templates/match_play_match_load.html")
	if err != nil {
		handleWebErr(w, err)
		return
	}
	data := struct {
		MatchesByType    map[model.MatchType]MatchPlayList
		CurrentMatchType model.MatchType
	}{
		matchesByType,
		currentMatchType,
	}
	err = template.ExecuteTemplate(w, "match_play_match_load.html", data)
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
		case "loadMatch":
			args := struct {
				MatchId int
			}{}
			err = mapstructure.Decode(data, &args)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			err = web.arena.ResetMatch()
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			if args.MatchId == 0 {
				err = web.arena.LoadTestMatch()
			} else {
				match, err := web.arena.Database.GetMatchById(args.MatchId)
				if err != nil {
					ws.WriteError(err.Error())
					continue
				}
				if match == nil {
					ws.WriteError(fmt.Sprintf("invalid match ID %d", args.MatchId))
					continue
				}
				err = web.arena.LoadMatch(match)
			}
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
		case "substituteTeams":
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
		case "commitResults":
			if web.arena.MatchState != field.PostMatch {
				ws.WriteError("cannot commit match while it is in progress")
				continue
			}
			err = web.arena.ResetMatch()
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			err = web.arena.LoadNextMatch(true)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
		case "discardResults":
			err = web.arena.ResetMatch()
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
			err = web.arena.LoadNextMatch(false)
			if err != nil {
				ws.WriteError(err.Error())
				continue
			}
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

// Helper function to implement the required interface for Sort.
func (list MatchPlayList) Len() int {
	return len(list)
}

// Helper function to implement the required interface for Sort.
func (list MatchPlayList) Less(i, j int) bool {
	return list[i].Status == game.MatchScheduled && list[j].Status != game.MatchScheduled
}

// Helper function to implement the required interface for Sort.
func (list MatchPlayList) Swap(i, j int) {
	list[i], list[j] = list[j], list[i]
}

// Constructs the list of matches to display on the side of the match play interface.
func (web *Web) buildMatchPlayList(matchType model.MatchType) (MatchPlayList, error) {
	matches, err := web.arena.Database.GetMatchesByType(matchType, false)
	if err != nil {
		return MatchPlayList{}, err
	}

	matchPlayList := make(MatchPlayList, len(matches))
	for i, match := range matches {
		matchPlayList[i].Id = match.Id
		matchPlayList[i].ShortName = match.ShortName
		matchPlayList[i].Time = match.Time.Local().Format("3:04 PM")
		matchPlayList[i].Status = match.Status
		switch match.Status {
		case game.RedWonMatch:
			matchPlayList[i].ColorClass = "red"
		case game.BlueWonMatch:
			matchPlayList[i].ColorClass = "blue"
		case game.TieMatch:
			matchPlayList[i].ColorClass = "yellow"
		default:
			matchPlayList[i].ColorClass = ""
		}
		if web.arena.CurrentMatch != nil && matchPlayList[i].Id == web.arena.CurrentMatch.Id {
			matchPlayList[i].ColorClass = "green"
		}
	}

	// Sort the list to put all completed matches at the bottom.
	sort.Stable(matchPlayList)

	return matchPlayList, nil
}
