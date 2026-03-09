// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Functions for controlling the arena and match play.

package field

import (
	"fmt"
	"log"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Team254/cheesy-arena/game"
	"github.com/Team254/cheesy-arena/hardware"
	"github.com/Team254/cheesy-arena/model"
	"github.com/Team254/cheesy-arena/network"
	"github.com/Team254/cheesy-arena/plc"
)

const (
	arenaLoopPeriodMs        = 10
	arenaLoopWarningMs       = 5
	dsPacketPeriodMs         = 500
	dsPacketWarningMs        = 550
	periodicTaskPeriodSec    = 30
	matchEndScoreDwellSec    = 3
	preLoadNextMatchDelaySec = 5
	scheduledBreakDelaySec   = 5
	earlyLateThresholdMin    = 2.5
	MaxMatchGapMin           = 20
)

// Progression of match states.
type MatchState int

const (
	PreMatch MatchState = iota
	StartMatch
	WarmupPeriod
	AutoPeriod
	PausePeriod
	TeleopPeriod
	PostMatch
	FreePractice // Sibling branch to match-play path; no timers.
)

type Arena struct {
	Database             *model.Database
	EventSettings        *model.EventSettings
	accessPoint          network.AccessPoint
	networkSwitch        *network.Switch
	redSCC               *network.SCCSwitch
	blueSCC              *network.SCCSwitch
	Plc                  plc.Plc
	FieldLights          hardware.FieldLights
	EStopPanels          []hardware.EStopPanel
	FieldEStop           hardware.FieldEStopPanel
	AutoWinner           hardware.Alliance
	AllianceStations     map[string]*AllianceStation
	ArenaNotifiers
	MatchState
	lastMatchState       MatchState
	lastLightingState    hardware.LightingState
	CurrentMatch         *model.Match
	MatchStartTime       time.Time
	LastMatchTimeSec     float64
	lastDsPacketTime     time.Time
	lastPeriodicTaskTime time.Time
	EventStatus          EventStatus
	MuteMatchSounds      bool
	matchAborted         bool
	soundsPlayed         map[*game.MatchSound]struct{}

	freePracticeReconfiguring atomic.Bool  // true while AP is being reconfigured for a slot change
	freePracticeReconfigMu    sync.Mutex   // serialises concurrent SetFreePracticeSlot calls
	fieldEStopActive          atomic.Bool  // latched when GPIO field e-stop fires; cleared by ClearFieldEStop()
}

type AllianceStation struct {
	DsConn     *DriverStationConnection
	Ethernet   bool
	AStop      atomic.Bool
	EStop      atomic.Bool
	Bypass     atomic.Bool
	Team       *model.Team
	WifiStatus network.TeamWifiStatus
	aStopReset bool
}

// Creates the arena and sets it to its initial state.
func NewArena(dbPath string) (*Arena, error) {
	arena := new(Arena)
	arena.configureNotifiers()
	arena.Plc = new(plc.FakePlc)
	log.Println("WARNING: FakePlc active — physical e-stop hardware is not monitored.")
	arena.FieldLights = &hardware.NoopFieldLights{}
	arena.EStopPanels = []hardware.EStopPanel{}
	arena.FieldEStop = &hardware.NoopFieldEStopPanel{}

	arena.AllianceStations = make(map[string]*AllianceStation)
	arena.AllianceStations["R1"] = new(AllianceStation)
	arena.AllianceStations["R2"] = new(AllianceStation)
	arena.AllianceStations["R3"] = new(AllianceStation)
	arena.AllianceStations["B1"] = new(AllianceStation)
	arena.AllianceStations["B2"] = new(AllianceStation)
	arena.AllianceStations["B3"] = new(AllianceStation)

	var err error
	arena.Database, err = model.OpenDatabase(dbPath)
	if err != nil {
		return nil, err
	}
	err = arena.LoadSettings()
	if err != nil {
		return nil, err
	}

	// Load empty match as current.
	arena.MatchState = PreMatch
	arena.LoadTestMatch()
	arena.LastMatchTimeSec = 0
	arena.lastMatchState = -1

	return arena, nil
}

// Loads or reloads the event settings upon initial setup or change.
func (arena *Arena) LoadSettings() error {
	settings, err := arena.Database.GetEventSettings()
	if err != nil {
		return err
	}
	arena.EventSettings = settings

	// Initialize the components that depend on settings.
	accessPointWifiStatuses := [6]*network.TeamWifiStatus{
		&arena.AllianceStations["R1"].WifiStatus,
		&arena.AllianceStations["R2"].WifiStatus,
		&arena.AllianceStations["R3"].WifiStatus,
		&arena.AllianceStations["B1"].WifiStatus,
		&arena.AllianceStations["B2"].WifiStatus,
		&arena.AllianceStations["B3"].WifiStatus,
	}
	arena.accessPoint.SetSettings(
		settings.ApAddress,
		settings.ApPassword,
		settings.ApChannel,
		settings.NetworkSecurityEnabled,
		accessPointWifiStatuses,
	)
	arena.networkSwitch = network.NewSwitch(settings.SwitchAddress, settings.SwitchPassword)
	sccUpCommands := strings.Split(settings.SCCUpCommands, "\n")
	sccDownCommands := strings.Split(settings.SCCDownCommands, "\n")
	arena.redSCC = network.NewSCCSwitch(
		settings.RedSCCAddress,
		settings.SCCUsername,
		settings.SCCPassword,
		sccUpCommands,
		sccDownCommands,
	)
	arena.blueSCC = network.NewSCCSwitch(
		settings.BlueSCCAddress,
		settings.SCCUsername,
		settings.SCCPassword,
		sccUpCommands,
		sccDownCommands,
	)
	arena.Plc.SetAddress(settings.PlcAddress)

	game.MatchTiming.WarmupDurationSec = settings.WarmupDurationSec
	game.MatchTiming.AutoDurationSec = settings.AutoDurationSec
	game.MatchTiming.PauseDurationSec = settings.PauseDurationSec
	game.MatchTiming.TeleopDurationSec = settings.TeleopDurationSec
	game.MatchTiming.WarningRemainingDurationSec = settings.WarningRemainingDurationSec
	game.UpdateMatchSounds()
	arena.MatchTimingNotifier.Notify()

	return nil
}

// Sets up the arena for the given match.
func (arena *Arena) LoadMatch(match *model.Match) error {
	if arena.MatchState != PreMatch {
		return fmt.Errorf("cannot load match while there is a match still in progress or with results pending")
	}

	arena.CurrentMatch = match

	err := arena.assignTeam(match.Red1, "R1")
	if err != nil {
		return err
	}
	err = arena.assignTeam(match.Red2, "R2")
	if err != nil {
		return err
	}
	err = arena.assignTeam(match.Red3, "R3")
	if err != nil {
		return err
	}
	err = arena.assignTeam(match.Blue1, "B1")
	if err != nil {
		return err
	}
	err = arena.assignTeam(match.Blue2, "B2")
	if err != nil {
		return err
	}
	err = arena.assignTeam(match.Blue3, "B3")
	if err != nil {
		return err
	}

	arena.setupNetwork(
		[6]*model.Team{
			arena.AllianceStations["R1"].Team,
			arena.AllianceStations["R2"].Team,
			arena.AllianceStations["R3"].Team,
			arena.AllianceStations["B1"].Team,
			arena.AllianceStations["B2"].Team,
			arena.AllianceStations["B3"].Team,
		},
		false,
	)

	// Reset the arena state.
	arena.soundsPlayed = make(map[*game.MatchSound]struct{})
	arena.Plc.ResetMatch()

	// Notify any listeners about the new match.
	arena.MatchLoadNotifier.Notify()

	return nil
}

// Sets a new test match containing no teams as the current match.
func (arena *Arena) LoadTestMatch() error {
	return arena.LoadMatch(&model.Match{Type: model.Test, ShortName: "T", LongName: "Test Match"})
}

// Loads the first unplayed match of the current match type.
func (arena *Arena) LoadNextMatch(startScheduledBreak bool) error {
	nextMatch, err := arena.getNextMatch(false)
	if err != nil {
		return err
	}
	if nextMatch == nil {
		return arena.LoadTestMatch()
	}
	err = arena.LoadMatch(nextMatch)
	if err != nil {
		return err
	}

	return nil
}

// Assigns the given team to the given station, also substituting it into the match record.
func (arena *Arena) SubstituteTeams(red1, red2, red3, blue1, blue2, blue3 int) error {
	if !arena.CurrentMatch.ShouldAllowSubstitution() {
		return fmt.Errorf("Can't substitute teams for qualification matches.")
	}

	if err := arena.validateTeams(red1, red2, red3, blue1, blue2, blue3); err != nil {
		return err
	}
	if err := arena.assignTeam(red1, "R1"); err != nil {
		return err
	}
	if err := arena.assignTeam(red2, "R2"); err != nil {
		return err
	}
	if err := arena.assignTeam(red3, "R3"); err != nil {
		return err
	}
	if err := arena.assignTeam(blue1, "B1"); err != nil {
		return err
	}
	if err := arena.assignTeam(blue2, "B2"); err != nil {
		return err
	}
	if err := arena.assignTeam(blue3, "B3"); err != nil {
		return err
	}

	arena.CurrentMatch.Red1 = red1
	arena.CurrentMatch.Red2 = red2
	arena.CurrentMatch.Red3 = red3
	arena.CurrentMatch.Blue1 = blue1
	arena.CurrentMatch.Blue2 = blue2
	arena.CurrentMatch.Blue3 = blue3
	arena.setupNetwork(
		[6]*model.Team{
			arena.AllianceStations["R1"].Team,
			arena.AllianceStations["R2"].Team,
			arena.AllianceStations["R3"].Team,
			arena.AllianceStations["B1"].Team,
			arena.AllianceStations["B2"].Team,
			arena.AllianceStations["B3"].Team,
		},
		false,
	)
	arena.MatchLoadNotifier.Notify()

	if arena.CurrentMatch.Type != model.Test {
		arena.Database.UpdateMatch(arena.CurrentMatch)
	}
	return nil
}

// Starts the match if all conditions are met.
func (arena *Arena) StartMatch() error {
	err := arena.checkCanStartMatch()
	if err == nil {
		// Save the match start time to the database for posterity.
		arena.CurrentMatch.StartedAt = time.Now()
		if arena.CurrentMatch.Type != model.Test {
			arena.Database.UpdateMatch(arena.CurrentMatch)
		}
		arena.updateCycleTime(arena.CurrentMatch.StartedAt)

		// Save the missed packet count to subtract it from the running count.
		for _, allianceStation := range arena.AllianceStations {
			if allianceStation.DsConn != nil {
				err = allianceStation.DsConn.signalMatchStart(arena.CurrentMatch, &allianceStation.WifiStatus)
				if err != nil {
					log.Println(err)
				}
			}

			// Save the teams that have successfully connected to the field.
			if allianceStation.Team != nil && !allianceStation.Team.HasConnected && allianceStation.DsConn != nil &&
				allianceStation.DsConn.RobotLinked {
				allianceStation.Team.HasConnected = true
				arena.Database.UpdateTeam(allianceStation.Team)
			}
		}

		arena.MatchState = StartMatch
	}
	return err
}

// Kills the current match if it is underway.
func (arena *Arena) AbortMatch() error {
	if arena.MatchState == PreMatch || arena.MatchState == PostMatch {
		return fmt.Errorf("cannot abort match when it is not in progress")
	}

	if arena.MatchState != WarmupPeriod {
		arena.PlaySound("abort")
	}
	arena.MatchState = PostMatch
	arena.matchAborted = true
	return nil
}

// Clears out the match and resets the arena state unless there is a match underway.
func (arena *Arena) ResetMatch() error {
	if arena.MatchState != PostMatch && arena.MatchState != PreMatch {
		return fmt.Errorf("cannot reset match while it is in progress")
	}
	arena.MatchState = PreMatch
	arena.matchAborted = false
	arena.AllianceStations["R1"].Bypass.Store(false)
	arena.AllianceStations["R2"].Bypass.Store(false)
	arena.AllianceStations["R3"].Bypass.Store(false)
	arena.AllianceStations["B1"].Bypass.Store(false)
	arena.AllianceStations["B2"].Bypass.Store(false)
	arena.AllianceStations["B3"].Bypass.Store(false)
	arena.MuteMatchSounds = false
	return nil
}

// DisableAll sets Bypass on every alliance station so the next DS packet disables
// all robots. Safe to call from any goroutine (atomic write). Intended for use
// during graceful shutdown (SIGTERM).
func (arena *Arena) DisableAll() {
	for _, as := range arena.AllianceStations {
		as.Bypass.Store(true)
	}
}

// ClearFieldEStop is called by the "clearFieldEStop" WebSocket command.
// It resets the GPIO field e-stop latch and clears all station e-stops so that
// driver stations can re-enable their robots. If the button is still physically
// held the underlying Clear() is a no-op, and the latch stays active.
// Safe to call from any goroutine (all writes are atomic).
func (arena *Arena) ClearFieldEStop() {
	arena.FieldEStop.Clear()
	if !arena.FieldEStop.Triggered() {
		arena.fieldEStopActive.Store(false)
		for _, as := range arena.AllianceStations {
			as.EStop.Store(false)
		}
	}
}

// Returns the fractional number of seconds since the start of the match.
func (arena *Arena) MatchTimeSec() float64 {
	switch arena.MatchState {
	case PreMatch, StartMatch, PostMatch, FreePractice:
		return 0
	default:
		return time.Since(arena.MatchStartTime).Seconds()
	}
}

// Performs a single iteration of checking inputs and timers and setting outputs accordingly to control the
// flow of a match.
func (arena *Arena) Update() {
	// Decide what state the robots need to be in, depending on where we are in the match.
	auto := false
	enabled := false
	sendDsPacket := false
	matchTimeSec := arena.MatchTimeSec()
	switch arena.MatchState {
	case PreMatch:
		auto = true
		enabled = false
	case StartMatch:
		arena.MatchStartTime = time.Now()
		arena.LastMatchTimeSec = -1
		auto = true
		if game.MatchTiming.WarmupDurationSec > 0 {
			arena.MatchState = WarmupPeriod
			enabled = false
			sendDsPacket = false
		} else {
			arena.MatchState = AutoPeriod
			arena.assignAutoWinner()
			enabled = true
			sendDsPacket = true
		}
		arena.Plc.ResetMatch()
	case WarmupPeriod:
		auto = true
		enabled = false
		if matchTimeSec >= float64(game.MatchTiming.WarmupDurationSec) {
			arena.MatchState = AutoPeriod
			arena.assignAutoWinner()
			auto = true
			enabled = true
			sendDsPacket = true
		}
	case AutoPeriod:
		auto = true
		enabled = true
		if matchTimeSec >= game.GetDurationToAutoEnd().Seconds() {
			auto = false
			sendDsPacket = true
			if game.MatchTiming.PauseDurationSec > 0 {
				arena.MatchState = PausePeriod
				enabled = false
			} else {
				arena.MatchState = TeleopPeriod
				enabled = true
			}
		}
	case PausePeriod:
		auto = false
		enabled = false
		if matchTimeSec >= game.GetDurationToTeleopStart().Seconds() {
			arena.MatchState = TeleopPeriod
			auto = false
			enabled = true
			sendDsPacket = true
		}
	case TeleopPeriod:
		auto = false
		enabled = true
		if matchTimeSec >= game.GetDurationToTeleopEnd().Seconds() {
			arena.MatchState = PostMatch
			auto = false
			enabled = false
			sendDsPacket = true
			go func() {
				// Configure the network in advance for the next match after a delay.
				time.Sleep(time.Second * preLoadNextMatchDelaySec)
				arena.preLoadNextMatch()
			}()
		}
	case FreePractice:
		// No timer logic. Grant field-enable to all stations unless a slot change is in progress.
		auto = false
		enabled = !arena.freePracticeReconfiguring.Load()
	}

	// Send a match tick notification if passing an integer second threshold or if the match state changed.
	if int(matchTimeSec) != int(arena.LastMatchTimeSec) || arena.MatchState != arena.lastMatchState {
		arena.MatchTimeNotifier.Notify()
	}

	// Send a packet if at a period transition point or if it's been long enough since the last one.
	msSinceLastDsPacket := int(time.Since(arena.lastDsPacketTime).Seconds() * 1000)
	if sendDsPacket || msSinceLastDsPacket >= dsPacketPeriodMs {
		if msSinceLastDsPacket >= dsPacketWarningMs && arena.lastDsPacketTime.After(time.Time{}) {
			log.Printf("Warning: Long time since last driver station packet: %dms", msSinceLastDsPacket)
		}
		arena.sendDsPacket(auto, enabled)
		arena.ArenaStatusNotifier.Notify()
	}

	arena.handleSounds(matchTimeSec)

	// Poll GPIO field e-stop (latching; fires once per press, cleared via web UI).
	if arena.FieldEStop.Triggered() && !arena.fieldEStopActive.Load() {
		arena.fieldEStopActive.Store(true)
		for _, as := range arena.AllianceStations {
			as.EStop.Store(true)
		}
		switch arena.MatchState {
		case StartMatch, WarmupPeriod, AutoPeriod, PausePeriod, TeleopPeriod:
			_ = arena.AbortMatch()
		}
	}

	// Poll hardware e-stop panels (runs on arena goroutine — no locking needed).
	for _, panel := range arena.EStopPanels {
		for _, event := range panel.Poll() {
			if event.Station == "all" {
				for _, s := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
					arena.handleTeamStop(s, !event.IsAStop, event.IsAStop)
				}
			} else {
				arena.handleTeamStop(event.Station, !event.IsAStop, event.IsAStop)
			}
		}
	}

	// Notify FieldLights driver on any state or sub-phase change.
	if ls := arena.computeLightingState(matchTimeSec); ls != arena.lastLightingState {
		if err := arena.FieldLights.SetState(ls); err != nil {
			log.Printf("FieldLights.SetState: %v", err)
		}
		arena.lastLightingState = ls
	}

	// Handle field sensors/lights/actuators.
	arena.handlePlcInputOutput()

	arena.LastMatchTimeSec = matchTimeSec
	arena.lastMatchState = arena.MatchState
}

// Loops indefinitely to track and update the arena components.
func (arena *Arena) Run() {
	// Start other loops in goroutines.
	go arena.listenForDriverStations()
	go arena.listenForDsUdpPackets()
	go arena.accessPoint.Run()
	go arena.Plc.Run()

	ticker := time.NewTicker(time.Millisecond * arenaLoopPeriodMs)
	defer ticker.Stop()
	for range ticker.C {
		loopStartTime := time.Now()
		arena.Update()
		if time.Since(loopStartTime).Milliseconds() > arenaLoopWarningMs {
			log.Printf("Warning: Arena loop iteration took a long time: %dms", time.Since(loopStartTime).Milliseconds())
		}
		if time.Since(arena.lastPeriodicTaskTime).Seconds() >= periodicTaskPeriodSec {
			arena.lastPeriodicTaskTime = time.Now()
			go arena.runPeriodicTasks()
		}
	}
}

// Checks that the given teams are present in the database, allowing team ID 0 which indicates an empty spot.
func (arena *Arena) validateTeams(teamIds ...int) error {
	for _, teamId := range teamIds {
		if teamId == 0 {
			continue
		}
		team, err := arena.Database.GetTeamById(teamId)
		if err != nil {
			return err
		}
		if team == nil {
			return fmt.Errorf("Team %d is not present at the event.", teamId)
		}
	}
	return nil
}

// Loads a team into an alliance station, cleaning up the previous team there if there is one.
func (arena *Arena) assignTeam(teamId int, station string) error {
	// Reject invalid station values.
	if _, ok := arena.AllianceStations[station]; !ok {
		return fmt.Errorf("Invalid alliance station '%s'.", station)
	}

	// Force the A-stop to be reset by the new team if it is already pressed (if the PLC is enabled).
	arena.AllianceStations[station].aStopReset = !arena.Plc.IsEnabled()

	// Do nothing if the station is already assigned to the requested team.
	dsConn := arena.AllianceStations[station].DsConn
	if dsConn != nil && dsConn.TeamId == teamId {
		return nil
	}
	if dsConn != nil {
		dsConn.close()
		arena.AllianceStations[station].Team = nil
		arena.AllianceStations[station].DsConn = nil
	}

	// Leave the station empty if the team number is zero.
	if teamId == 0 {
		arena.AllianceStations[station].Team = nil
		return nil
	}

	// Load the team model. If it doesn't exist, enable anonymous operation.
	team, err := arena.Database.GetTeamById(teamId)
	if err != nil {
		return err
	}
	if team == nil {
		team = &model.Team{Id: teamId}
	}

	arena.AllianceStations[station].Team = team
	return nil
}

// Returns the next match of the same type that is currently loaded, or nil if there are no more matches.
func (arena *Arena) getNextMatch(excludeCurrent bool) (*model.Match, error) {
	if arena.CurrentMatch.Type == model.Test {
		return nil, nil
	}

	matches, err := arena.Database.GetMatchesByType(arena.CurrentMatch.Type, false)
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		if !match.IsComplete() && !(excludeCurrent && match.Id == arena.CurrentMatch.Id) {
			return &match, nil
		}
	}

	// There are no matches left of the same type.
	return nil, nil
}

// Configures the field network for the next match in advance of the current match being scored and committed.
func (arena *Arena) preLoadNextMatch() {
	if arena.MatchState != PostMatch {
		// The next match has already been loaded; no need to do anything.
		return
	}

	nextMatch, err := arena.getNextMatch(true)
	if err != nil {
		log.Printf("Failed to pre-load next match: %s", err.Error())
	}
	if nextMatch == nil {
		return
	}

	teamIds := [6]int{nextMatch.Red1, nextMatch.Red2, nextMatch.Red3, nextMatch.Blue1, nextMatch.Blue2, nextMatch.Blue3}

	var teams [6]*model.Team
	for i, teamId := range teamIds {
		if teamId == 0 {
			continue
		}
		if teams[i], err = arena.Database.GetTeamById(teamId); err != nil {
			log.Printf("Failed to get model for Team %d while pre-loading next match: %s", teamId, err.Error())
		}
	}
	arena.setupNetwork(teams, true)
}

// Enable or disable the team ethernet ports on both SCCs
func (arena *Arena) setSCCEthernetEnabled(enabled bool) {
	if arena.EventSettings.SCCManagementEnabled {
		var wg sync.WaitGroup
		wg.Add(2)

		configureSCC := func(scc *network.SCCSwitch, name string) {
			defer wg.Done()
			err := scc.SetTeamEthernetEnabled(enabled)
			if err != nil {
				log.Printf("Failed to set %s SCC enabled state to %t: %s", name, enabled, err.Error())
			}
		}
		go configureSCC(arena.redSCC, "red")
		go configureSCC(arena.blueSCC, "blue")
		wg.Wait()
	}
}

// Asynchronously reconfigures the networking hardware for the new set of teams.
func (arena *Arena) setupNetwork(teams [6]*model.Team, isPreload bool) {
	if arena.EventSettings.NetworkSecurityEnabled {
		if err := arena.accessPoint.ConfigureTeamWifi(teams); err != nil {
			log.Printf("Failed to configure team WiFi: %s", err.Error())
		}
		go func() {
			arena.setSCCEthernetEnabled(false)
			if err := arena.networkSwitch.ConfigureTeamEthernet(teams); err != nil {
				log.Printf("Failed to configure team Ethernet: %s", err.Error())
			}
			arena.setSCCEthernetEnabled(true)
		}()
	}
}

// Returns nil if the match can be started, and an error otherwise.
func (arena *Arena) checkCanStartMatch() error {
	if arena.MatchState != PreMatch {
		return fmt.Errorf("cannot start match while there is a match still in progress or with results pending")
	}

	if arena.fieldEStopActive.Load() {
		return fmt.Errorf("cannot start match while field emergency stop is active")
	}

	err := arena.checkAllianceStationsReady("R1", "R2", "R3", "B1", "B2", "B3")
	if err != nil {
		return err
	}

	if arena.Plc.IsEnabled() {
		if !arena.Plc.IsHealthy() {
			return fmt.Errorf("cannot start match while PLC is not healthy")
		}
		if arena.Plc.GetFieldEStop() {
			return fmt.Errorf("cannot start match while field emergency stop is active")
		}
		for name, status := range arena.Plc.GetArmorBlockStatuses() {
			if !status {
				return fmt.Errorf("cannot start match while PLC ArmorBlock %q is not connected", name)
			}
		}
	}

	return nil
}

func (arena *Arena) checkAllianceStationsReady(stations ...string) error {
	for _, station := range stations {
		allianceStation := arena.AllianceStations[station]
		if allianceStation.EStop.Load() {
			return fmt.Errorf("cannot start match while an emergency stop is active")
		}
		if !allianceStation.aStopReset {
			return fmt.Errorf("cannot start match if an autonomous stop has not been reset since the previous match")
		}
		if !allianceStation.Bypass.Load() {
			if allianceStation.DsConn == nil || !allianceStation.DsConn.RobotLinked {
				return fmt.Errorf("cannot start match until all robots are connected or bypassed")
			}
		}
	}

	return nil
}

func (arena *Arena) sendDsPacket(auto bool, enabled bool) {
	for _, allianceStation := range arena.AllianceStations {
		dsConn := allianceStation.DsConn
		if dsConn != nil {
			dsConn.Auto = auto
			dsConn.Enabled = enabled && !allianceStation.EStop.Load() && !(auto && allianceStation.AStop.Load()) &&
				!allianceStation.Bypass.Load()
			dsConn.EStop = allianceStation.EStop.Load()
			dsConn.AStop = allianceStation.AStop.Load()
			err := dsConn.update(arena)
			if err != nil {
				log.Printf("Unable to send driver station packet for team %d.", allianceStation.Team.Id)
			}
		}
	}
	arena.lastDsPacketTime = time.Now()
}

// Returns the alliance station identifier for the given team, or the empty string if the team is not present
// in the current match.
func (arena *Arena) getAssignedAllianceStation(teamId int) string {
	for station, allianceStation := range arena.AllianceStations {
		if allianceStation.Team != nil && allianceStation.Team.Id == teamId {
			return station
		}
	}

	return ""
}

// Updates the score given new input information from the field PLC, and actuates PLC outputs accordingly.
func (arena *Arena) handlePlcInputOutput() {
	if !arena.Plc.IsEnabled() {
		return
	}

	// Handle PLC functions that are always active.
	if arena.Plc.GetFieldEStop() && !arena.matchAborted {
		arena.AbortMatch()
	}
	redEStops, blueEStops := arena.Plc.GetTeamEStops()
	redAStops, blueAStops := arena.Plc.GetTeamAStops()
	arena.handleTeamStop("R1", redEStops[0], redAStops[0])
	arena.handleTeamStop("R2", redEStops[1], redAStops[1])
	arena.handleTeamStop("R3", redEStops[2], redAStops[2])
	arena.handleTeamStop("B1", blueEStops[0], blueAStops[0])
	arena.handleTeamStop("B2", blueEStops[1], blueAStops[1])
	arena.handleTeamStop("B3", blueEStops[2], blueAStops[2])
	redEthernets, blueEthernets := arena.Plc.GetEthernetConnected()
	arena.AllianceStations["R1"].Ethernet = redEthernets[0]
	arena.AllianceStations["R2"].Ethernet = redEthernets[1]
	arena.AllianceStations["R3"].Ethernet = redEthernets[2]
	arena.AllianceStations["B1"].Ethernet = blueEthernets[0]
	arena.AllianceStations["B2"].Ethernet = blueEthernets[1]
	arena.AllianceStations["B3"].Ethernet = blueEthernets[2]

	redAllianceReady := arena.checkAllianceStationsReady("R1", "R2", "R3") == nil
	blueAllianceReady := arena.checkAllianceStationsReady("B1", "B2", "B3") == nil

	// Handle the evergreen PLC functions: stack lights, stack buzzer, and field reset light.
	switch arena.MatchState {
	case PreMatch:
		if arena.lastMatchState != PreMatch {
			arena.Plc.SetFieldResetLight(true)
		}
		// Set the stack light state -- solid alliance color(s) if robots are not connected, solid orange if scores are
		// not input, or blinking green if ready.
		greenStackLight := redAllianceReady && blueAllianceReady && arena.Plc.GetCycleState(2, 0, 2)
		arena.Plc.SetStackLights(!redAllianceReady, !blueAllianceReady, false, greenStackLight)
		arena.Plc.SetStackBuzzer(redAllianceReady && blueAllianceReady)

		// Turn off lights if all teams become ready.
		if redAllianceReady && blueAllianceReady {
			arena.Plc.SetFieldResetLight(false)
			if arena.CurrentMatch.FieldReadyAt.IsZero() {
				arena.CurrentMatch.FieldReadyAt = time.Now()
			}
		}
	case PostMatch:
		arena.Plc.SetStackLights(false, false, false, false)
	case AutoPeriod, PausePeriod, TeleopPeriod:
		arena.Plc.SetStackBuzzer(false)
		arena.Plc.SetStackLights(!redAllianceReady, !blueAllianceReady, false, true)
	}

	// Handle the truss lights.
	if arena.MatchState == AutoPeriod || arena.MatchState == PausePeriod || arena.MatchState == TeleopPeriod {
		warningSequenceActive, lights := trussLightWarningSequence(arena.MatchTimeSec())
		if warningSequenceActive {
			arena.Plc.SetTrussLights(lights, lights)
		} else {
			arena.Plc.SetTrussLights([3]bool{true, true, true}, [3]bool{true, true, true})
		}
	} else {
		matchStartTime := arena.MatchStartTime
		currentTime := time.Now()
		teleopGracePeriod := matchStartTime.Add(game.GetDurationToTeleopEnd() + game.TeleopGracePeriodSec*time.Second)
		inGracePeriod := arena.MatchState == PostMatch && currentTime.Before(teleopGracePeriod) && !arena.matchAborted
		arena.Plc.SetTrussLights(
			[3]bool{inGracePeriod, inGracePeriod, inGracePeriod}, [3]bool{inGracePeriod, inGracePeriod, inGracePeriod},
		)
	}
}

func (arena *Arena) handleTeamStop(station string, eStopState, aStopState bool) {
	allianceStation := arena.AllianceStations[station]
	if eStopState {
		allianceStation.EStop.Store(true)
	} else if arena.MatchTimeSec() == 0 {
		// Keep the E-stop latched until the match is over.
		allianceStation.EStop.Store(false)
	}
	if aStopState {
		allianceStation.AStop.Store(true)
	} else if arena.MatchState != AutoPeriod {
		// Keep the A-stop latched until the autonomous period is over.
		allianceStation.AStop.Store(false)
		allianceStation.aStopReset = true
	}
}

func (arena *Arena) handleSounds(matchTimeSec float64) {
	if arena.MatchState == PreMatch || arena.MatchState == FreePractice {
		// Only apply this logic during a match.
		return
	}

	for _, sound := range game.MatchSounds {
		if sound.MatchTimeSec < 0 {
			// Skip sounds with negative timestamps; they are meant to only be triggered explicitly.
			continue
		}
		if _, ok := arena.soundsPlayed[sound]; !ok {
			if matchTimeSec >= sound.MatchTimeSec && matchTimeSec-sound.MatchTimeSec < 1 {
				arena.PlaySound(sound.Name)
				arena.soundsPlayed[sound] = struct{}{}
			}
		}
	}
}

func (arena *Arena) PlaySound(name string) {
	if !arena.MuteMatchSounds {
		arena.PlaySoundNotifier.NotifyWithMessage(name)
	}
}

// Performs any actions that need to run at the interval specified by periodicTaskPeriodSec.
func (arena *Arena) runPeriodicTasks() {
	arena.updateEarlyLateMessage()
}

// trussLightWarningSequence generates the sequence of truss light states during the "sonar ping" warning sound. It
// returns true if the sequence is active, and an array of booleans indicating the state of each truss light.
func trussLightWarningSequence(matchTimeSec float64) (bool, [3]bool) {
	stepTimeSec := 0.2
	sequence := []int{1, 2, 3, 2, 1, 2, 3, 0, 0, 1, 2, 3, 2, 1, 2, 3, 0, 0}
	startTime := float64(
		game.MatchTiming.WarmupDurationSec + game.MatchTiming.AutoDurationSec + game.MatchTiming.PauseDurationSec +
			game.MatchTiming.TeleopDurationSec - game.MatchTiming.WarningRemainingDurationSec,
	)
	lights := [3]bool{false, false, false}

	if matchTimeSec < startTime {
		// The sequence is not active yet.
		return false, lights
	}

	step := int((matchTimeSec - startTime) / stepTimeSec)
	if step < len(sequence) && sequence[step] > 0 {
		lights[sequence[step]-1] = true
	}
	return step < len(sequence), lights
}

// EnterFreePractice transitions the arena from PreMatch into FreePractice mode.
// Returns an error if called from any other state.
func (arena *Arena) EnterFreePractice() error {
	if arena.MatchState != PreMatch {
		return fmt.Errorf("cannot enter free practice while a match is in progress or results are pending")
	}
	arena.MatchState = FreePractice
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// ExitFreePractice clears all slots, resets AP, and returns to PreMatch.
// Robots are disabled before any slot is cleared, ensuring they are never
// briefly enabled-but-disconnected during the transition.
func (arena *Arena) ExitFreePractice() error {
	if arena.MatchState != FreePractice {
		return fmt.Errorf("not in free practice mode")
	}

	arena.freePracticeReconfigMu.Lock()
	defer arena.freePracticeReconfigMu.Unlock()

	// Disable all robots immediately; the next arena tick will send disabled packets.
	arena.freePracticeReconfiguring.Store(true)

	// Clear every slot.
	for _, station := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		as := arena.AllianceStations[station]
		if as.DsConn != nil {
			as.DsConn.close()
			as.DsConn = nil
		}
		as.Team = nil
		as.EStop.Store(false)
		as.AStop.Store(false)
	}

	// Reset the AP to an empty configuration.
	var emptyTeams [6]*model.Team
	if err := arena.accessPoint.ConfigureTeamWifi(emptyTeams); err != nil {
		log.Printf("ExitFreePractice: failed to reset AP: %v", err)
		// Continue regardless — we are exiting free practice.
	}

	arena.freePracticeReconfiguring.Store(false)
	arena.MatchState = PreMatch
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// SetFreePracticeSlot registers a team in the given station.
// teamId must be ≥ 1 and must not already be assigned to another slot.
// Triggers a brief AP reconfiguration during which all robots are disabled.
// If AP reconfiguration fails the slot assignment is rolled back.
func (arena *Arena) SetFreePracticeSlot(station string, teamId int, wpaKey string) error {
	if arena.MatchState != FreePractice {
		return fmt.Errorf("not in free practice mode")
	}
	if _, ok := arena.AllianceStations[station]; !ok {
		return fmt.Errorf("invalid alliance station %q", station)
	}
	if teamId <= 0 {
		return fmt.Errorf("team number must be 1 or greater")
	}

	// Reject duplicate team numbers across slots.
	for id, as := range arena.AllianceStations {
		if id != station && as.Team != nil && as.Team.Id == teamId {
			return fmt.Errorf("team %d is already registered in station %s", teamId, id)
		}
	}

	arena.freePracticeReconfigMu.Lock()
	defer arena.freePracticeReconfigMu.Unlock()

	arena.freePracticeReconfiguring.Store(true)

	as := arena.AllianceStations[station]
	oldTeam := as.Team

	// Close any existing DS connection for the slot.
	if as.DsConn != nil {
		as.DsConn.close()
		as.DsConn = nil
	}
	as.Team = &model.Team{Id: teamId, WpaKey: wpaKey}

	// Build the current 6-team list for AP configuration.
	teams := arena.freePracticeTeams()
	if err := arena.accessPoint.ConfigureTeamWifi(teams); err != nil {
		// Rollback in-memory state.
		as.Team = oldTeam
		arena.freePracticeReconfiguring.Store(false)
		return fmt.Errorf("AP reconfiguration failed (rolled back): %w", err)
	}

	arena.freePracticeReconfiguring.Store(false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// ClearFreePracticeSlot removes the team from the given station.
// If the slot is already empty no AP reconfiguration is triggered.
// Triggers a brief AP reconfiguration pause otherwise.
func (arena *Arena) ClearFreePracticeSlot(station string) error {
	if arena.MatchState != FreePractice {
		return fmt.Errorf("not in free practice mode")
	}
	if _, ok := arena.AllianceStations[station]; !ok {
		return fmt.Errorf("invalid alliance station %q", station)
	}

	as := arena.AllianceStations[station]
	if as.Team == nil {
		return nil // already empty — no reconfiguration needed
	}

	arena.freePracticeReconfigMu.Lock()
	defer arena.freePracticeReconfigMu.Unlock()

	arena.freePracticeReconfiguring.Store(true)

	oldTeam := as.Team
	if as.DsConn != nil {
		as.DsConn.close()
		as.DsConn = nil
	}
	as.Team = nil

	teams := arena.freePracticeTeams()
	if err := arena.accessPoint.ConfigureTeamWifi(teams); err != nil {
		// Rollback in-memory state.
		as.Team = oldTeam
		arena.freePracticeReconfiguring.Store(false)
		return fmt.Errorf("AP reconfiguration failed (rolled back): %w", err)
	}

	arena.freePracticeReconfiguring.Store(false)
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// freePracticeTeams builds the [6]*model.Team array (R1…B3) from current AllianceStations.
func (arena *Arena) freePracticeTeams() [6]*model.Team {
	var teams [6]*model.Team
	for i, s := range []string{"R1", "R2", "R3", "B1", "B2", "B3"} {
		teams[i] = arena.AllianceStations[s].Team
	}
	return teams
}

// assignAutoWinner randomly picks which alliance's HUB goes inactive first in Shift1.
func (arena *Arena) assignAutoWinner() {
	if rand.Intn(2) == 0 {
		arena.AutoWinner = hardware.AllianceRed
	} else {
		arena.AutoWinner = hardware.AllianceBlue
	}
}

// computeLightingState derives the current LightingState from arena state and match time.
func (arena *Arena) computeLightingState(matchTimeSec float64) hardware.LightingState {
	var phase hardware.MatchPhase
	switch arena.MatchState {
	case AutoPeriod:
		phase = hardware.PhaseAuto
	case PausePeriod:
		phase = hardware.PhasePause
	case TeleopPeriod:
		phase = hardware.PhaseTeleop
	case PostMatch:
		phase = hardware.PhaseFinished
	default:
		phase = hardware.PhaseIdle
	}

	var subPhase hardware.TeleopSubPhase
	var warning bool
	if arena.MatchState == TeleopPeriod {
		teleopStart := game.GetDurationToTeleopStart().Seconds()
		remaining := int(float64(game.MatchTiming.TeleopDurationSec) - (matchTimeSec - teleopStart))
		subPhase = teleopSubPhase(remaining)
		warning = shiftWarning(remaining)
	}

	return hardware.LightingState{
		Phase:          phase,
		TeleopSubPhase: subPhase,
		AutoWinner:     arena.AutoWinner,
		ShiftWarning:   warning,
	}
}

// teleopSubPhase returns the REBUILT 2026 sub-phase for the given remaining teleop seconds.
func teleopSubPhase(remaining int) hardware.TeleopSubPhase {
	switch {
	case remaining > 130:
		return hardware.SubPhaseTransition // T=2:20–2:10
	case remaining > 105:
		return hardware.SubPhaseShift1 // T=2:10–1:45
	case remaining > 80:
		return hardware.SubPhaseShift2 // T=1:45–1:20
	case remaining > 55:
		return hardware.SubPhaseShift3 // T=1:20–0:55
	case remaining > 30:
		return hardware.SubPhaseShift4 // T=0:55–0:30
	default:
		return hardware.SubPhaseEndGame // T=0:30–0:00
	}
}

// shiftWarning returns true during the 3s window before each HUB deactivation boundary.
func shiftWarning(remaining int) bool {
	return (remaining >= 130 && remaining < 133) || // 3s before Shift1
		(remaining >= 105 && remaining < 108) || // 3s before Shift2
		(remaining >= 80 && remaining < 83) || // 3s before Shift3
		(remaining >= 55 && remaining < 58) // 3s before Shift4
}

// stationOrder is the fill order for auto-assignment fallback (R1→R2→R3→B1→B2→B3).
var stationOrder = []string{"R1", "R2", "R3", "B1", "B2", "B3"}

// autoAssignTeam detects the physical station for the connecting team (via switch VLAN
// query) and assigns them to it. Falls back to the first empty station if detection fails.
// Creates a DB record for the team if one does not already exist.
// Returns the assigned station name, or "" if unavailable.
func (arena *Arena) autoAssignTeam(teamId int) string {
	if arena.MatchState != PreMatch {
		return ""
	}
	if !arena.CurrentMatch.ShouldAllowSubstitution() {
		return ""
	}

	// Ensure the team exists in the DB with a valid WPA key.
	team, err := arena.Database.GetTeamById(teamId)
	if err != nil {
		log.Printf("Error looking up Team %d for auto-assignment: %v", teamId, err)
		return ""
	}
	if team == nil {
		team = &model.Team{
			Id:     teamId,
			WpaKey: fmt.Sprintf("%08d", teamId),
		}
		if err := arena.Database.CreateTeam(team); err != nil {
			log.Printf("Error creating Team %d for auto-assignment: %v", teamId, err)
			return ""
		}
	}

	// Try to detect the physical station via the switch VLAN/ARP table.
	station, err := arena.networkSwitch.GetStationForTeamId(teamId)
	if err != nil {
		log.Printf("Switch station detection for Team %d failed: %v; falling back to sequential.", teamId, err)
	}

	// If switch detection succeeded and the station is empty, use it;
	// otherwise fall back to the first available empty station.
	if station == "" || arena.AllianceStations[station].Team != nil {
		station = ""
		for _, s := range stationOrder {
			if arena.AllianceStations[s].Team == nil {
				station = s
				break
			}
		}
	}
	if station == "" {
		log.Printf("No empty station available for auto-assignment of Team %d.", teamId)
		return ""
	}

	if err := arena.assignTeam(teamId, station); err != nil {
		log.Printf("Error auto-assigning Team %d to %s: %v", teamId, station, err)
		return ""
	}
	switch station {
	case "R1":
		arena.CurrentMatch.Red1 = teamId
	case "R2":
		arena.CurrentMatch.Red2 = teamId
	case "R3":
		arena.CurrentMatch.Red3 = teamId
	case "B1":
		arena.CurrentMatch.Blue1 = teamId
	case "B2":
		arena.CurrentMatch.Blue2 = teamId
	case "B3":
		arena.CurrentMatch.Blue3 = teamId
	}
	arena.setupNetwork([6]*model.Team{
		arena.AllianceStations["R1"].Team, arena.AllianceStations["R2"].Team,
		arena.AllianceStations["R3"].Team, arena.AllianceStations["B1"].Team,
		arena.AllianceStations["B2"].Team, arena.AllianceStations["B3"].Team,
	}, false)
	arena.MatchLoadNotifier.Notify()
	if arena.CurrentMatch.Type != model.Test {
		arena.Database.UpdateMatch(arena.CurrentMatch)
	}
	log.Printf("Auto-assigned Team %d to station %s.", teamId, station)
	return station
}
