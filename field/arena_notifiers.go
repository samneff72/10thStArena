// Copyright 2018 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Contains configuration of the publish-subscribe notifiers that allow the arena to push updates to websocket clients.

package field

import (
	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/network"
	"github.com/team841/bioarena/websocket"
)

// faultDescription renders a stored hardware.FaultKind for the web UI,
// returning "" for a healthy input so the JavaScript can test it directly.
func faultDescription(kind uint32) string {
	if hardware.FaultKind(kind) == hardware.FaultNone {
		return ""
	}
	return hardware.FaultKind(kind).String()
}

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
	// EStopFault describes a dual-channel wiring fault on this station's e-stop,
	// or is empty when the wiring reads healthy. A faulted station also has
	// EStop set, so a UI that ignores this field still shows the stop.
	EStopFault string
	// PortLinked is whether this station's driver station port has link, from the switch.
	// Meaningless unless StationLinksKnown is set on the enclosing message.
	PortLinked bool
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
			EStopFault: faultDescription(as.EStopFault.Load()),
			PortLinked: as.PortLinked.Load(),
		}
	}
	return &struct {
		MatchId                   int
		AllianceStations          map[string]allianceStationView
		MatchState
		CanStartMatch             bool
		AccessPointStatus         string
		SwitchStatus              string
		PlcIsHealthy              bool
		FieldEStop                bool
		PlcArmorBlockStatuses     map[string]bool
		GpioFieldEStopActive      bool
		GpioFieldEStopFault       string
		FreePracticeReconfiguring bool
		FieldDisabled             bool
		StationLinksKnown         bool
		// CurrentView is the operating page every kiosk should be showing, so that a
		// field with several displays does not have them disagreeing about what is
		// being run.
		CurrentView string
	}{
		arena.CurrentMatch.Id,
		stationViews,
		arena.MatchState,
		arena.checkCanStartMatch() == nil,
		arena.accessPoint.Status,
		arena.teamNetwork.GetStatus(),
		arena.Plc.IsHealthy(),
		arena.Plc.GetFieldEStop(),
		arena.Plc.GetArmorBlockStatuses(),
		arena.fieldEStopActive.Load(),
		faultDescription(arena.fieldEStopFault.Load()),
		arena.freePracticeReconfiguring.Load(),
		arena.fieldDisabled.Load(),
		arena.stationLinksKnown.Load(),
		// Locked variant: this generator runs from Update with the arena lock held.
		arena.currentViewLocked(),
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
		AutoWinnerMode    string
	}{
		arena.CurrentMatch,
		arena.CurrentMatch.ShouldAllowSubstitution(),
		isReplay,
		teams,
		arena.AutoWinnerMode.String(),
	}
}

func (arena *Arena) generateMatchTimeMessage() any {
	return MatchTimeMessage{arena.MatchState, int(arena.MatchTimeSec())}
}

func (arena *Arena) generateMatchTimingMessage() any {
	return &game.MatchTiming
}
