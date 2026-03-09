// Copyright 2018 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Contains configuration of the publish-subscribe notifiers that allow the arena to push updates to websocket clients.

package field

import (
	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/network"
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

// allianceStationView is a JSON-safe projection of AllianceStation.
// AllianceStation uses atomic.Bool for EStop/AStop/Bypass, which serializes as {} in JSON.
// This struct materialises those values so the JavaScript UI receives correct booleans.
type allianceStationView struct {
	DsConn     *DriverStationConnection
	Ethernet   bool
	AStop      bool
	EStop      bool
	Bypass     bool
	Team       *model.Team
	WifiStatus network.TeamWifiStatus
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
	stationViews := make(map[string]allianceStationView, len(arena.AllianceStations))
	for k, as := range arena.AllianceStations {
		stationViews[k] = allianceStationView{
			DsConn:     as.DsConn,
			Ethernet:   as.Ethernet,
			AStop:      as.AStop.Load(),
			EStop:      as.EStop.Load(),
			Bypass:     as.Bypass.Load(),
			Team:       as.Team,
			WifiStatus: as.WifiStatus,
		}
	}
	return &struct {
		MatchId                   int
		AllianceStations          map[string]allianceStationView
		MatchState
		CanStartMatch             bool
		AccessPointStatus         string
		SwitchStatus              string
		RedSCCStatus              string
		BlueSCCStatus             string
		PlcIsHealthy              bool
		FieldEStop                bool
		PlcArmorBlockStatuses     map[string]bool
		GpioFieldEStopActive      bool
		FreePracticeReconfiguring bool
	}{
		arena.CurrentMatch.Id,
		stationViews,
		arena.MatchState,
		arena.checkCanStartMatch() == nil,
		arena.accessPoint.Status,
		arena.networkSwitch.Status,
		arena.redSCC.Status,
		arena.blueSCC.Status,
		arena.Plc.IsHealthy(),
		arena.Plc.GetFieldEStop(),
		arena.Plc.GetArmorBlockStatuses(),
		arena.fieldEStopActive.Load(),
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
