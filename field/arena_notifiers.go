// Copyright 2018 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Contains configuration of the publish-subscribe notifiers that allow the arena to push updates to websocket clients.

package field

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/websocket"
)

type ArenaNotifiers struct {
	ArenaStatusNotifier *websocket.Notifier
	MatchLoadNotifier   *websocket.Notifier
	MatchTimeNotifier   *websocket.Notifier
	MatchTimingNotifier *websocket.Notifier
	PlaySoundNotifier   *websocket.Notifier
}

type MatchTimeMessage struct {
	MatchState
	MatchTimeSec int
}

// Instantiates notifiers and configures their message producing methods.
func (arena *Arena) configureNotifiers() {
	arena.ArenaStatusNotifier = websocket.NewNotifier("arenaStatus", arena.generateArenaStatusMessage)
	arena.MatchLoadNotifier = websocket.NewNotifier("matchLoad", arena.GenerateMatchLoadMessage)
	arena.MatchTimeNotifier = websocket.NewNotifier("matchTime", arena.generateMatchTimeMessage)
	arena.MatchTimingNotifier = websocket.NewNotifier("matchTiming", arena.generateMatchTimingMessage)
	arena.PlaySoundNotifier = websocket.NewNotifier("playSound", nil)
}

func (arena *Arena) generateArenaStatusMessage() any {
	return &struct {
		MatchId          int
		AllianceStations map[string]*AllianceStation
		MatchState
		CanStartMatch              bool
		AccessPointStatus          string
		SwitchStatus               string
		RedSCCStatus               string
		BlueSCCStatus              string
		PlcIsHealthy               bool
		FieldEStop                 bool
		PlcArmorBlockStatuses      map[string]bool
		FreePracticeReconfiguring  bool
	}{
		arena.CurrentMatch.Id,
		arena.AllianceStations,
		arena.MatchState,
		arena.checkCanStartMatch() == nil,
		arena.accessPoint.Status,
		arena.networkSwitch.Status,
		arena.redSCC.Status,
		arena.blueSCC.Status,
		arena.Plc.IsHealthy(),
		arena.Plc.GetFieldEStop(),
		arena.Plc.GetArmorBlockStatuses(),
		arena.freePracticeReconfiguring.Load(),
	}
}

func (arena *Arena) GenerateMatchLoadMessage() any {
	teams := make(map[string]*model.Team)
	for station, allianceStation := range arena.AllianceStations {
		teams[station] = allianceStation.Team
	}

	matchResult, _ := arena.Database.GetMatchResultForMatch(arena.CurrentMatch.Id)
	isReplay := matchResult != nil

	return &struct {
		Match             *model.Match
		AllowSubstitution bool
		IsReplay          bool
		Teams             map[string]*model.Team
	}{
		arena.CurrentMatch,
		arena.CurrentMatch.ShouldAllowSubstitution(),
		isReplay,
		teams,
	}
}

func (arena *Arena) generateMatchTimeMessage() any {
	return MatchTimeMessage{arena.MatchState, int(arena.MatchTimeSec())}
}

func (arena *Arena) generateMatchTimingMessage() any {
	return &game.MatchTiming
}
