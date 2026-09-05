// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Functions for controlling the arena and match play.

package field

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/led"
	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/network"
	"github.com/team841/bioarena/plc"
)

const (
	arenaLoopPeriodMs        = 10
	arenaLoopWarningMs       = 5
	dsPacketPeriodMs         = 500
	dsPacketWarningMs        = 550
	periodicTaskPeriodSec    = 30
	matchEndScoreDwellSec    = 3
	preLoadNextMatchDelaySec = 5

	// How long the field dwells in PostMatch before returning to PreMatch on its own.
	// Long enough for the end-of-match sounds and the Hub LED post-match sequence to
	// finish, short enough that a practice round turns around quickly.
	postMatchAutoClearDelaySec = 5
	scheduledBreakDelaySec   = 5
	earlyLateThresholdMin    = 2.5

	// portBounceCooldown bounds how often a station's port can be cycled to rescue a
	// missing driver station. Long enough that a laptop with its driver station software
	// closed is not cycled repeatedly, short enough to be worth waiting for.
	portBounceCooldown = 60 * time.Second
	MaxMatchGapMin     = 20
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
	Database         *model.Database
	EventSettings    *model.EventSettings
	accessPoint      network.AccessPoint
	teamNetwork      teamNetwork
	Plc              plc.Plc
	FieldLights      hardware.FieldLights
	Leds             ledController
	hubLedsArtNet    bool
	hubLedFallback   led.Fallback
	EStopPanels      []hardware.EStopPanel
	FieldEStop       hardware.FieldEStopPanel
	AutoWinner       hardware.Alliance
	AutoWinnerMode   AutoWinnerMode
	GameData         string
	AllianceStations map[string]*AllianceStation
	ArenaNotifiers
	MatchState
	lastMatchState       MatchState
	lastLightingState    hardware.LightingState
	CurrentMatch         *model.Match
	MatchStartTime       time.Time
	postMatchStartTime   time.Time
	LastMatchTimeSec     float64
	lastDsPacketTime     time.Time
	lastPeriodicTaskTime time.Time
	EventStatus          EventStatus
	MuteMatchSounds      bool
	currentView          string // operating page the kiosks mirror; see SetCurrentView
	matchAborted         bool
	soundsPlayed         map[*game.MatchSound]struct{}

	// mu serialises the match loop against the web handlers. Update runs on the arena
	// goroutine every 10ms while StartMatch, ClearMatch, SubstituteTeams, DisableField
	// and the rest are called from HTTP and WebSocket goroutines, so without it two
	// operators -- or one operator and the loop -- can interleave mid-mutation.
	// sendDsPacket reading Team.Id while assignTeam sets Team to nil panics the process
	// outright, taking the field down.
	//
	// Exported methods take it; anything reachable from Update has an unexported
	// counterpart, since Go mutexes are not reentrant. Nothing under it may block on
	// network I/O -- see setupNetwork.
	mu sync.Mutex

	freePracticeReconfiguring atomic.Bool     // true while AP is being reconfigured for a slot change
	freePracticeReconfigMu    sync.Mutex      // serialises concurrent SetFreePracticeSlot calls
	ethernetConfigMutex       sync.Mutex      // guards ethernetConfigGeneration
	ethernetConfigGeneration  uint64          // increments per request; stale requests are dropped
	ethernetApplyMutex        sync.Mutex      // serialises wired reconfigurations
	fieldEStopActive          atomic.Bool     // latched when GPIO field e-stop fires; cleared by ClearFieldEStop()
	fieldEStopFault           atomic.Uint32   // hardware.FaultKind of the field e-stop; FaultNone when healthy
	fieldDisabled             atomic.Bool     // operator halt: robots disabled, field networking untouched
	stationLinksKnown         atomic.Bool     // true once the switch has reported driver station port links
	lastPortBounce            [6]time.Time    // when each station's port was last cycled to rescue a driver station
	stationDetectorOverride   stationDetector // nil in production; injected in tests
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
	// PortLinked is whether this station's driver station port has link, as last reported
	// by the switch. Only meaningful when Arena.stationLinksKnown is set.
	PortLinked atomic.Bool
	// EStopFault is the hardware.FaultKind reported by this station's dual-channel
	// e-stop, or FaultNone. It tracks the live wiring condition rather than
	// latching: once the wiring is repaired the panel stops reporting it.
	EStopFault atomic.Uint32
}

// allStationNames is every alliance station, for field-wide stops.
var allStationNames = []string{"R1", "R2", "R3", "B1", "B2", "B3"}

// Creates the arena and sets it to its initial state.
func NewArena(dbPath string) (*Arena, error) {
	arena := new(Arena)
	arena.configureNotifiers()
	arena.Plc = new(plc.FakePlc)
	arena.FieldLights = &hardware.NoopFieldLights{}
	// Output stays disabled until an sACN address is configured.
	arena.Leds = newLedController(false)
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

// teamNetwork is what the arena needs from the field switch. An interface only so tests
// can substitute one; there is a single implementation, network.Switch, because there is a
// single supported field: a Catalyst 3560-CX wired as the README describes.
type teamNetwork interface {
	ConfigureTeamEthernet(teams [6]*model.Team) error
	GetStationForTeamId(teamId int) (string, error)
	GetStationPortLinks() ([6]bool, error)
	CycleStationPort(station int) error
	GetStatus() string
}

// Loads or reloads the event settings upon initial setup or change.
func (arena *Arena) LoadSettings() error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

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
	arena.teamNetwork = network.NewSwitch(
		network.SwitchConfig{
			Address:   settings.SwitchAddress,
			Password:  settings.SwitchPassword,
			DnsServer: settings.SwitchDnsServer,
		},
	)

	if settings.FieldEStopPin != 0 {
		panel, err := hardware.NewGpioFieldEStopPanel("gpiochip0", settings.FieldEStopNcPin, settings.FieldEStopPin)
		if err != nil {
			log.Printf(
				"WARNING: Could not open field e-stop GPIO pins (NC %d, NO %d): %v",
				settings.FieldEStopNcPin,
				settings.FieldEStopPin,
				err,
			)
			arena.FieldEStop = &hardware.NoopFieldEStopPanel{}
		} else {
			if settings.FieldEStopNcPin == 0 {
				log.Println("WARNING: Field e-stop is wired single-channel — wiring faults will not be detected.")
			}
			arena.FieldEStop = panel
		}
	} else {
		log.Println("WARNING: No field e-stop pin configured — field-wide e-stop not monitored.")
		arena.FieldEStop = &hardware.NoopFieldEStopPanel{}
	}

	arena.EStopPanels = nil
	if addr := settings.RedEStopPanelAddress; addr != "" {
		arena.EStopPanels = append(arena.EStopPanels, hardware.NewNetworkEStopPanel(addr, "red"))
	}
	if addr := settings.BlueEStopPanelAddress; addr != "" {
		arena.EStopPanels = append(arena.EStopPanels, hardware.NewNetworkEStopPanel(addr, "blue"))
	}

	arena.applyHubLedSettings(settings)

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

	arena.setupNetwork(arena.currentTeams(), false)

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
	arena.mu.Lock()
	defer arena.mu.Unlock()

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
	arena.setupNetwork(arena.currentTeams(), false)
	arena.MatchLoadNotifier.Notify()

	if arena.CurrentMatch.Type != model.Test {
		arena.Database.UpdateMatch(arena.CurrentMatch)
	}
	return nil
}

// Starts the match if all conditions are met.
func (arena *Arena) StartMatch() error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

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
	arena.mu.Lock()
	defer arena.mu.Unlock()
	return arena.abortMatchLocked()
}

// abortMatchLocked is AbortMatch with the arena lock already held.
func (arena *Arena) abortMatchLocked() error {
	if arena.MatchState == PreMatch || arena.MatchState == PostMatch {
		return fmt.Errorf("cannot abort match when it is not in progress")
	}

	if arena.MatchState != WarmupPeriod {
		arena.PlaySound("abort")
	}
	arena.MatchState = PostMatch
	arena.postMatchStartTime = time.Now()
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
	arena.GameData = ""
	arena.AllianceStations["R1"].Bypass.Store(false)
	arena.AllianceStations["R2"].Bypass.Store(false)
	arena.AllianceStations["R3"].Bypass.Store(false)
	arena.AllianceStations["B1"].Bypass.Store(false)
	arena.AllianceStations["B2"].Bypass.Store(false)
	arena.AllianceStations["B3"].Bypass.Store(false)
	arena.MuteMatchSounds = false
	return nil
}

// ClearMatch resets to a new test match while preserving the current team station
// assignments so teams do not need to re-register between practice rounds.
func (arena *Arena) ClearMatch() error {
	arena.mu.Lock()
	defer arena.mu.Unlock()
	return arena.clearMatchLocked()
}

// clearMatchLocked is ClearMatch with the arena lock already held.
func (arena *Arena) clearMatchLocked() error {
	if arena.MatchState != PostMatch {
		return fmt.Errorf("cannot clear match while it is in progress")
	}
	red1 := arena.CurrentMatch.Red1
	red2 := arena.CurrentMatch.Red2
	red3 := arena.CurrentMatch.Red3
	blue1 := arena.CurrentMatch.Blue1
	blue2 := arena.CurrentMatch.Blue2
	blue3 := arena.CurrentMatch.Blue3

	// Carry bypass state across the clear, alongside the team assignments. A practice
	// field runs the same lineup round after round, and ResetMatch clearing every bypass
	// meant re-bypassing the empty stations after every match -- now that clearing
	// happens on its own, that would be every round without anyone asking for it.
	//
	// Restored after LoadMatch rather than skipped in ResetMatch, whose own callers still
	// want a full reset.
	bypassed := make(map[string]bool, len(arena.AllianceStations))
	for station, allianceStation := range arena.AllianceStations {
		bypassed[station] = allianceStation.Bypass.Load()
	}

	if err := arena.ResetMatch(); err != nil {
		return err
	}

	defer func() {
		for station, wasBypassed := range bypassed {
			arena.AllianceStations[station].Bypass.Store(wasBypassed)
		}
	}()

	return arena.LoadMatch(&model.Match{
		Type:      model.Test,
		ShortName: "T",
		LongName:  "Test Match",
		Red1:      red1,
		Red2:      red2,
		Red3:      red3,
		Blue1:     blue1,
		Blue2:     blue2,
		Blue3:     blue3,
	})
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
// held — or its wiring is faulted — the underlying Clear() is a no-op and the
// latch stays active, so an operator cannot acknowledge away a fault that is
// still present.
// Safe to call from any goroutine (all writes are atomic).
func (arena *Arena) ClearFieldEStop() {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	arena.FieldEStop.Clear()
	state, fault := arena.FieldEStop.State()
	arena.fieldEStopFault.Store(uint32(fault))
	if state == hardware.StopOK {
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
	arena.mu.Lock()
	defer arena.mu.Unlock()

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
		// Game data is withheld until the teleop transition, matching a real field.
		arena.GameData = ""
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
				arena.GameData = arena.gameDataForAutoWinner()
				enabled = true
			}
		}
	case PausePeriod:
		auto = false
		enabled = false
		if matchTimeSec >= game.GetDurationToTeleopStart().Seconds() {
			arena.MatchState = TeleopPeriod
			arena.GameData = arena.gameDataForAutoWinner()
			auto = false
			enabled = true
			sendDsPacket = true
		}
	case TeleopPeriod:
		auto = false
		enabled = true
		if matchTimeSec >= game.GetDurationToTeleopEnd().Seconds() {
			arena.MatchState = PostMatch
			arena.postMatchStartTime = time.Now()
			auto = false
			enabled = false
			sendDsPacket = true
			go func() {
				// Configure the network in advance for the next match after a delay.
				time.Sleep(time.Second * preLoadNextMatchDelaySec)
				arena.preLoadNextMatch()
			}()
		}
	case PostMatch:
		auto = false
		enabled = false

		// Return to PreMatch without an operator action. Clearing used to require one,
		// which stranded the field whenever the operator lost the web UI at match end.
		//
		// That is not hypothetical: the FRC Driver Station releases its IP configuration
		// when a match ends, so an operator running field control from a driver station
		// laptop is disconnected every single round. It is the driver station's own
		// behaviour and nothing here can prevent it, but the field no longer needs the
		// operator in order to reset.
		if time.Since(arena.postMatchStartTime) >= postMatchAutoClearDelaySec*time.Second {
			if err := arena.clearMatchLocked(); err != nil {
				log.Printf("Failed to clear match automatically: %v", err)
			}
		}
	case FreePractice:
		// No timer logic; stations are granted field-enable continuously.
		auto = false
		enabled = arena.freePracticeEnabled()
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
	// A wiring fault stops the field on the same path as a pressed button.
	fieldEStopState, fieldEStopFault := arena.FieldEStop.State()
	if prev := arena.fieldEStopFault.Swap(uint32(fieldEStopFault)); prev != uint32(fieldEStopFault) &&
		fieldEStopFault != hardware.FaultNone {
		log.Printf("WARNING: Field e-stop wiring fault: %s", fieldEStopFault)
	}
	if fieldEStopState != hardware.StopOK && !arena.fieldEStopActive.Load() {
		arena.fieldEStopActive.Store(true)
		for _, as := range arena.AllianceStations {
			as.EStop.Store(true)
		}
		arena.abortMatchForStop()
	}

	// Poll hardware e-stop panels (runs on arena goroutine — no locking needed).
	// Panels report the state of every configured input, so a released button
	// arrives as an explicit StopOK rather than as an absence.
	for _, panel := range arena.EStopPanels {
		for _, input := range panel.Poll() {
			stations := []string{input.Station}
			if input.Station == "all" {
				stations = allStationNames
			}
			for _, station := range stations {
				arena.handlePanelInput(station, input)
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

	// Advance the Hub LED sequences. Must run before lastMatchState is updated below,
	// since the post-match sequence fires on the transition into PostMatch.
	arena.updateHubLeds(time.Now())

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

	// Configure the field for whatever is loaded, rather than waiting for someone to load
	// a match. A controller that has just started is otherwise inert: the switch has no
	// VLANs, so a driver station plugged into it gets no address and cannot register
	// itself, and the badges report UNKNOWN because nothing has been attempted.
	//
	// In a goroutine because the access point takes seconds to answer and the arena loop
	// below should not wait for it.
	go arena.setupNetwork(arena.currentTeams(), false)

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

// Checks that the given teams are present in the database and that none appears twice,
// allowing team ID 0 which indicates an empty spot.
//
// The duplicate check matters more than it looks. A driver station is identified by its
// team number: both getAssignedAllianceStation and the UDP receive path find a station by
// scanning for a matching team. Go randomises map iteration order, so with one team in
// two stations those lookups return an arbitrary one of them, varying call to call. Two
// driver stations would contend for a single station's connection and telemetry would
// land on whichever the map happened to yield.
//
// The addressing collides too -- both stations derive the same subnet, the same SVI
// address, the same DHCP pool and the same SSID -- but the identity collision is the one
// that produces silent, nondeterministic misbehaviour rather than a switch error.
func (arena *Arena) validateTeams(teamIds ...int) error {
	seen := make(map[int]struct{}, len(teamIds))
	for _, teamId := range teamIds {
		if teamId == 0 {
			continue
		}
		if _, duplicate := seen[teamId]; duplicate {
			return fmt.Errorf("Team %d is assigned to more than one station.", teamId)
		}
		seen[teamId] = struct{}{}

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

// BypassEmptyStations sets Bypass on every station with no team assigned, leaving
// occupied stations untouched. This is the one-click form of what an operator running a
// 1v0 practice match would otherwise do five times by hand.
//
// Deliberately an explicit action rather than something applied automatically on match
// load: an unbypassed empty station blocking the start is a confirmation step, and
// removing it silently would also suppress the block when a station is empty by
// mistake. Returns the number of stations newly bypassed.
func (arena *Arena) BypassEmptyStations() int {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	var count int
	for _, allianceStation := range arena.AllianceStations {
		if allianceStation.Team == nil && !allianceStation.Bypass.Load() {
			allianceStation.Bypass.Store(true)
			count++
		}
	}
	return count
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
	// Runs on its own goroutine, delayed after the match ends, so it takes the lock like
	// any other caller. The network configuration it triggers is asynchronous.
	arena.mu.Lock()
	defer arena.mu.Unlock()

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

// currentTeams is the team in each alliance station, in station order, which is what the
// network drivers take.
func (arena *Arena) currentTeams() [6]*model.Team {
	var teams [6]*model.Team
	for i, station := range stationOrder {
		teams[i] = arena.AllianceStations[station].Team
	}
	return teams
}

// Asynchronously reconfigures the networking hardware for the new set of teams.
func (arena *Arena) setupNetwork(teams [6]*model.Team, isPreload bool) {
	if arena.EventSettings.NetworkSecurityEnabled {
		// Off the caller's goroutine: configuring the AP is a synchronous HTTP request
		// with a three second timeout, and callers hold the arena lock. Blocking there
		// would stall the match loop and stop driver station packets for the duration.
		// configureTeamEthernet is already asynchronous.
		go func() {
			if err := arena.accessPoint.ConfigureTeamWifi(teams); err != nil {
				log.Printf("Failed to configure team WiFi: %s", err.Error())
			}
		}()
	}
	arena.configureTeamEthernet(teams)
}

// configureTeamEthernet applies the wired configuration in the background, since a switch
// takes seconds to reconfigure and the caller is usually servicing a web request.
//
// A later call supersedes an earlier one that has not reached the hardware yet. Without
// that, registering teams one at a time queues a configuration per registration, and
// whichever goroutine happens to acquire the hardware last decides what the field ends up
// with -- which need not be the most recent team list.
func (arena *Arena) configureTeamEthernet(teams [6]*model.Team) {
	if !arena.EventSettings.NetworkSecurityEnabled {
		return
	}

	arena.ethernetConfigMutex.Lock()
	arena.ethernetConfigGeneration++
	generation := arena.ethernetConfigGeneration
	arena.ethernetConfigMutex.Unlock()

	go func() {
		// One application at a time, so that a superseded request drops out before
		// touching the hardware rather than after.
		arena.ethernetApplyMutex.Lock()
		defer arena.ethernetApplyMutex.Unlock()

		arena.ethernetConfigMutex.Lock()
		superseded := generation != arena.ethernetConfigGeneration
		arena.ethernetConfigMutex.Unlock()
		if superseded {
			return
		}

		if err := arena.teamNetwork.ConfigureTeamEthernet(teams); err != nil {
			log.Printf("Failed to configure team Ethernet: %s", err.Error())
		}
	}()
}

// Returns nil if the match can be started, and an error otherwise.
func (arena *Arena) checkCanStartMatch() error {
	if arena.MatchState != PreMatch {
		return fmt.Errorf("cannot start match while there is a match still in progress or with results pending")
	}

	if fault := hardware.FaultKind(arena.fieldEStopFault.Load()); fault != hardware.FaultNone {
		return fmt.Errorf("cannot start match while the field e-stop reports a wiring fault: %s", fault)
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
		if fault := hardware.FaultKind(allianceStation.EStopFault.Load()); fault != hardware.FaultNone {
			return fmt.Errorf("cannot start match while station %s reports an e-stop wiring fault: %s", station, fault)
		}
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
		// Locked variant: handlePlcInputOutput runs from Update with the lock held.
		arena.abortMatchLocked()
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

// handleTeamStop applies both stop kinds at once. The PLC path reports them
// together, so it calls this; the hardware panels report each input separately
// and call the halves directly.
func (arena *Arena) handleTeamStop(station string, eStopState, aStopState bool) {
	arena.handleTeamEStop(station, eStopState)
	arena.handleTeamAStop(station, aStopState)
}

func (arena *Arena) handleTeamEStop(station string, active bool) {
	allianceStation := arena.AllianceStations[station]
	if active {
		allianceStation.EStop.Store(true)
	} else if arena.MatchTimeSec() == 0 {
		// Keep the E-stop latched until the match is over.
		allianceStation.EStop.Store(false)
	}
}

func (arena *Arena) handleTeamAStop(station string, active bool) {
	allianceStation := arena.AllianceStations[station]
	if active {
		allianceStation.AStop.Store(true)
	} else if arena.MatchState != AutoPeriod {
		// Keep the A-stop latched until the autonomous period is over.
		allianceStation.AStop.Store(false)
		allianceStation.aStopReset = true
	}
}

// handlePanelInput folds one hardware panel input into station state.
//
// Only the half of the station's state that this input actually reports is
// touched: an a-stop reading says nothing about the e-stop, and vice versa.
func (arena *Arena) handlePanelInput(station string, input hardware.InputState) {
	if input.IsAStop {
		// A-stops are single-channel and cannot report a fault.
		arena.handleTeamAStop(station, input.State == hardware.StopActive)
		return
	}

	allianceStation := arena.AllianceStations[station]
	prev := allianceStation.EStopFault.Swap(uint32(input.Fault))
	if prev != uint32(input.Fault) && input.Fault != hardware.FaultNone {
		log.Printf("WARNING: Station %s e-stop wiring fault: %s", station, input.Fault)
	}

	wasStopped := allianceStation.EStop.Load()
	arena.handleTeamEStop(station, input.Stopped())
	if input.Stopped() && !wasStopped {
		arena.abortMatchForStop()
	}
}

// abortMatchForStop aborts an in-progress match. It is a no-op outside a match,
// which is what makes it safe to call on every rising edge of a stop.
func (arena *Arena) abortMatchForStop() {
	switch arena.MatchState {
	case StartMatch, WarmupPeriod, AutoPeriod, PausePeriod, TeleopPeriod:
		// Locked variant: every caller reaches here from Update.
		_ = arena.abortMatchLocked()
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
	arena.pollStationPortLinks()
}

// pollStationPortLinks asks the switch which driver station ports have link, so the UI can
// point out a laptop in the wrong station.
//
// Setup only. During a match or a free practice run this would put a Telnet session on the
// switch every thirty seconds for information nobody is looking at, and the wiring is
// settled by then anyway.
func (arena *Arena) pollStationPortLinks() {
	// Never during a match: this reads the switch, and the recovery below cycles ports.
	// Snapshot the state rather than holding the lock across the switch read, which is a
	// Telnet round trip and would stall the match loop for its duration.
	arena.mu.Lock()
	matchState := arena.MatchState
	arena.mu.Unlock()
	if matchState != PreMatch && matchState != FreePractice {
		return
	}

	links, err := arena.teamNetwork.GetStationPortLinks()
	if err != nil {
		// Logged only on the transition. An unconfigured switch would otherwise report
		// the same thing every thirty seconds for as long as the field runs.
		if arena.stationLinksKnown.Swap(false) {
			log.Printf("Cannot read driver station port links: %v", err)
		}
		return
	}

	// The switch read is done; take the lock for the station state below.
	arena.mu.Lock()
	defer arena.mu.Unlock()

	for i, station := range stationOrder {
		arena.AllianceStations[station].PortLinked.Store(links[i])
	}
	arena.stationLinksKnown.Store(true)
	arena.ArenaStatusNotifier.Notify()

	arena.recoverMissingDriverStations(links)
}

// recoverMissingDriverStations cycles the port of any station that has a team, has a cable,
// and has no driver station.
//
// The driver station releases its address on the match-end transition -- its own logic, not
// anything the field does -- and Windows then waits for something to change before asking
// for another. Replugging the cable works because the link event is what prompts it. So
// does this.
//
// Deliberately narrow: a station with no team, no cable, or a working driver station is
// left alone, and the cooldown keeps a laptop with its driver station software closed from
// having its port cycled every time round.
// recoverMissingDriverStations must be called with the arena lock held: it reads each
// station's Team and DsConn, which the web handlers reassign. Without it the nil check
// below and the Team.Id read further down can straddle an assignTeam that clears them,
// dereferencing nil and panicking the process.
func (arena *Arena) recoverMissingDriverStations(links [6]bool) {
	for i, station := range stationOrder {
		allianceStation := arena.AllianceStations[station]
		if allianceStation.Team == nil || allianceStation.DsConn != nil || !links[i] {
			continue
		}
		if time.Since(arena.lastPortBounce[i]) < portBounceCooldown {
			continue
		}
		arena.lastPortBounce[i] = time.Now()

		log.Printf(
			"%s has a cable and Team %d registered but no driver station; cycling its port to prompt a renewal.",
			station,
			allianceStation.Team.Id,
		)
		go func(index int, name string) {
			if err := arena.teamNetwork.CycleStationPort(index); err != nil {
				log.Printf("Could not cycle the %s port: %v", name, err)
			}
		}(i, station)
	}
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
	arena.mu.Lock()
	defer arena.mu.Unlock()

	if arena.MatchState != PreMatch {
		return fmt.Errorf("cannot enter free practice while a match is in progress or results are pending")
	}
	arena.fieldDisabled.Store(false)
	arena.MatchState = FreePractice
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// DisableField halts robot operation while leaving the field otherwise as it is: teams
// stay registered, SSIDs stay up, team subnets stay configured, and driver stations stay
// connected. It is the control an operator reaches for to stop the field between runs,
// and EnableField resumes without anyone re-registering or re-connecting.
//
// Use ExitFreePractice instead to take the whole field down.
func (arena *Arena) DisableField() {
	arena.fieldDisabled.Store(true)
	arena.ArenaStatusNotifier.Notify()
}

// EnableField resumes robot operation after DisableField.
func (arena *Arena) EnableField() {
	arena.fieldDisabled.Store(false)
	arena.ArenaStatusNotifier.Notify()
}

// IsFieldDisabled reports whether the operator has halted robot operation.
func (arena *Arena) IsFieldDisabled() bool {
	return arena.fieldDisabled.Load()
}

// freePracticeEnabled reports whether stations should be granted field-enable in free
// practice: not mid-reconfiguration, and not halted by the operator.
func (arena *Arena) freePracticeEnabled() bool {
	return !arena.freePracticeReconfiguring.Load() && !arena.fieldDisabled.Load()
}

// ExitFreePractice resets the field: every slot cleared, the AP emptied, the team subnets
// torn down, and the arena returned to PreMatch. Robots are disabled before any slot is
// cleared, ensuring they are never briefly enabled-but-disconnected during the transition.
//
// This is the heavy option. DisableField halts robots without disturbing any of it.
func (arena *Arena) ExitFreePractice() error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	if arena.MatchState != FreePractice {
		return fmt.Errorf("not in free practice mode (state=%d)", arena.MatchState)
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

	// Tear the team subnets down too, so no station is left routable to a team that has
	// gone home.
	arena.configureTeamEthernet(emptyTeams)

	arena.freePracticeReconfiguring.Store(false)
	arena.fieldDisabled.Store(false)
	arena.MatchState = PreMatch
	arena.ArenaStatusNotifier.Notify()
	return nil
}

// SetFreePracticeSlot registers a team in the given station.
// teamId must be ≥ 1 and must not already be assigned to another slot.
// Triggers a brief AP reconfiguration during which all robots are disabled.
// If AP reconfiguration fails the slot assignment is rolled back.
func (arena *Arena) SetFreePracticeSlot(station string, teamId int, wpaKey string) error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	if arena.MatchState != FreePractice && arena.MatchState != PreMatch {
		return fmt.Errorf("not in free practice mode (state=%d)", arena.MatchState)
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

	// The wired side too: without it a free practice slot gets an SSID but no VLAN
	// subinterface and no DHCP scope, so a driver station plugged into that station's
	// port never receives an address.
	arena.configureTeamEthernet(teams)

	// Best-effort: persist the WPA key back to the team record in the database.
	if dbTeam, dbErr := arena.Database.GetTeamById(teamId); dbErr == nil && dbTeam != nil && dbTeam.WpaKey != wpaKey {
		dbTeam.WpaKey = wpaKey
		if dbErr = arena.Database.UpdateTeam(dbTeam); dbErr != nil {
			log.Printf("Failed to persist WPA key for team %d: %v", teamId, dbErr)
		}
	}

	arena.ArenaStatusNotifier.Notify()
	return nil
}

// ClearFreePracticeSlot removes the team from the given station.
// If the slot is already empty no AP reconfiguration is triggered.
// Triggers a brief AP reconfiguration pause otherwise.
func (arena *Arena) ClearFreePracticeSlot(station string) error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	if arena.MatchState != FreePractice && arena.MatchState != PreMatch {
		return fmt.Errorf("not in free practice mode (state=%d)", arena.MatchState)
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
	arena.configureTeamEthernet(teams)
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

// AutoWinnerMode selects how the AUTO result is decided for a practice match. On a
// real field the winner is whichever alliance scored more FUEL during AUTO; bioarena
// does not score, so the operator chooses the outcome to practice against.
//
// The mode names an alliance rather than "win" or "lose" because arena state has no
// concept of which alliance is practising. The half-field mode can present it in
// win/lose terms once a live alliance is known.
type AutoWinnerMode int

const (
	AutoWinnerRandom AutoWinnerMode = iota
	AutoWinnerForceRed
	AutoWinnerForceBlue
)

func (mode AutoWinnerMode) String() string {
	switch mode {
	case AutoWinnerForceRed:
		return "red"
	case AutoWinnerForceBlue:
		return "blue"
	default:
		return "random"
	}
}

// ParseAutoWinnerMode converts the wire representation used by the web UI.
func ParseAutoWinnerMode(name string) (AutoWinnerMode, error) {
	switch name {
	case "random":
		return AutoWinnerRandom, nil
	case "red":
		return AutoWinnerForceRed, nil
	case "blue":
		return AutoWinnerForceBlue, nil
	default:
		return AutoWinnerRandom, fmt.Errorf("invalid AUTO winner mode %q", name)
	}
}

// SetAutoWinnerMode selects how the AUTO result will be decided for the next match.
// It cannot be changed once a match is underway: the winner is assigned at the start
// Field views that every kiosk mirrors. Only the two operating pages take part: the
// settings and team pages are administrative, and dragging every display to them because
// one operator opened one would be worse than the drift it fixed.
const (
	ViewMatchPlay    = "match_play"
	ViewFreePractice = "free_practice"
)

// SetCurrentView records which operating page the operators are on, so that kiosks
// opened on the other one follow. Whichever page was opened most recently wins.
//
// This is display state, not field state: it does not gate anything, and a kiosk that
// ignores it still works. It exists because a field can have several displays and they
// are useless if they disagree about what is being run.
func (arena *Arena) SetCurrentView(view string) {
	if view != ViewMatchPlay && view != ViewFreePractice {
		return
	}

	arena.mu.Lock()
	changed := arena.currentView != view
	arena.currentView = view
	arena.mu.Unlock()

	if changed {
		arena.ArenaStatusNotifier.Notify()
	}
}

// CurrentView is the operating page kiosks should be showing.
func (arena *Arena) CurrentView() string {
	arena.mu.Lock()
	defer arena.mu.Unlock()
	return arena.currentViewLocked()
}

// currentViewLocked assumes the arena lock is held. Free practice forces its own view:
// match play disables every control in that state, so a kiosk left there is useless
// regardless of where anyone navigated last.
func (arena *Arena) currentViewLocked() string {
	if arena.MatchState == FreePractice {
		return ViewFreePractice
	}
	if arena.currentView == "" {
		return ViewMatchPlay
	}
	return arena.currentView
}

// of AUTO and drives both the HUB lighting and the game data sent to driver stations,
// so a mid-match change would desynchronise them.
func (arena *Arena) SetAutoWinnerMode(mode AutoWinnerMode) error {
	arena.mu.Lock()
	defer arena.mu.Unlock()

	switch arena.MatchState {
	case PreMatch, PostMatch, FreePractice:
		arena.AutoWinnerMode = mode
		return nil
	default:
		return fmt.Errorf("cannot change AUTO winner mode while a match is in progress")
	}
}

// gameDataForAutoWinner returns the FMS Game Data string for the current AUTO result:
// a single character naming the alliance whose HUB goes inactive first in Shift1.
// Returns the empty string if no winner has been assigned, which is what driver
// stations should see before the teleop transition.
func (arena *Arena) gameDataForAutoWinner() string {
	switch arena.AutoWinner {
	case hardware.AllianceRed:
		return "R"
	case hardware.AllianceBlue:
		return "B"
	default:
		return ""
	}
}

// assignAutoWinner picks which alliance's HUB goes inactive first in Shift1, honouring
// the operator's selected mode. Random always resolves to a concrete alliance: a real
// field breaks an AUTO tie by selecting one, so AllianceNone is never a valid result.
func (arena *Arena) assignAutoWinner() {
	switch arena.AutoWinnerMode {
	case AutoWinnerForceRed:
		arena.AutoWinner = hardware.AllianceRed
	case AutoWinnerForceBlue:
		arena.AutoWinner = hardware.AllianceBlue
	default:
		if rand.Intn(2) == 0 {
			arena.AutoWinner = hardware.AllianceRed
		} else {
			arena.AutoWinner = hardware.AllianceBlue
		}
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

	// Upstream models the shift as spanning the whole match rather than teleop alone,
	// so AUTO and post-match map onto their own shifts and anything that is not a shift
	// (idle, the auto/teleop pause) reports ShiftCount.
	shift := game.ShiftCount
	var warning bool
	switch arena.MatchState {
	case AutoPeriod:
		shift = game.ShiftAuto
	case TeleopPeriod:
		teleopStart := game.GetDurationToTeleopStart().Seconds()
		remaining := int(float64(game.MatchTiming.TeleopDurationSec) - (matchTimeSec - teleopStart))
		shift = teleopShift(remaining)
		warning = shiftWarning(remaining)
	case PostMatch:
		shift = game.ShiftPostMatch
	}

	return hardware.LightingState{
		Phase:        phase,
		Shift:        shift,
		AutoWinner:   arena.AutoWinner,
		ShiftWarning: warning,
	}
}

// teleopShift returns the REBUILT 2026 shift for the given remaining teleop seconds.
// Boundaries are derived from the shift durations rather than hardcoded, so a change to
// those constants moves the boundaries with them.
func teleopShift(remaining int) game.Shift {
	endgame := game.MatchTiming.EndgameDurationSec
	shift := game.MatchTiming.ShiftDurationSec

	switch {
	case remaining > endgame+4*shift:
		return game.ShiftTransition
	case remaining > endgame+3*shift:
		return game.Shift1
	case remaining > endgame+2*shift:
		return game.Shift2
	case remaining > endgame+shift:
		return game.Shift3
	case remaining > endgame:
		return game.Shift4
	default:
		return game.ShiftEndgame
	}
}

// shiftWarning returns true during the 3s window before each HUB deactivation boundary.
func shiftWarning(remaining int) bool {
	return (remaining >= 130 && remaining < 133) || // 3s before Shift1
		(remaining >= 105 && remaining < 108) || // 3s before Shift2
		(remaining >= 80 && remaining < 83) || // 3s before Shift3
		(remaining >= 55 && remaining < 58) // 3s before Shift4
}

// stationDetector abstracts switch-based physical station detection for testability.
type stationDetector interface {
	GetStationForTeamId(teamId int) (string, error)
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
	if _, err := arena.ensureTeamExists(teamId); err != nil {
		log.Printf("Error creating Team %d for auto-assignment: %v", teamId, err)
		return ""
	}

	// Try to detect the physical station via the switch VLAN/ARP table.
	var detector stationDetector = arena.teamNetwork
	if arena.stationDetectorOverride != nil {
		detector = arena.stationDetectorOverride
	}
	station, err := detector.GetStationForTeamId(teamId)
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

	if err := arena.registerTeamAtStation(teamId, station); err != nil {
		log.Printf("Error auto-assigning Team %d to %s: %v", teamId, station, err)
		return ""
	}
	log.Printf("Auto-assigned Team %d to station %s.", teamId, station)
	return station
}

// registerTeamAtStation puts a team in a station and reconfigures the field for it,
// removing the team from any station it already occupied.
//
// The duplicate clearing matters because a team can now arrive at a station on its own: a
// laptop moved from one port to another would otherwise leave the team registered in both,
// and the abandoned station would keep a subnet nobody is using.
func (arena *Arena) registerTeamAtStation(teamId int, station string) error {
	for _, other := range stationOrder {
		if other == station {
			continue
		}
		if as := arena.AllianceStations[other]; as.Team != nil && as.Team.Id == teamId {
			log.Printf("Team %d moved from %s to %s; clearing %s.", teamId, other, station, other)
			if err := arena.assignTeam(0, other); err != nil {
				return err
			}
			arena.setMatchTeam(other, 0)
		}
	}

	if err := arena.assignTeam(teamId, station); err != nil {
		return err
	}
	arena.setMatchTeam(station, teamId)

	arena.setupNetwork(arena.currentTeams(), false)
	arena.MatchLoadNotifier.Notify()
	arena.ArenaStatusNotifier.Notify()
	if arena.CurrentMatch.Type != model.Test {
		arena.Database.UpdateMatch(arena.CurrentMatch)
	}
	return nil
}

// setMatchTeam records a station's team on the current match.
func (arena *Arena) setMatchTeam(station string, teamId int) {
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
}

// registerStagingTeam assigns a team that connected from a staging subnet to the station
// whose port it is plugged into.
//
// The station is known exactly: staging addresses carry the VLAN in their third octet, so
// the address a driver station connects from names its port. This is the only identification
// that survives shared hardware — it comes from the team number configured in the driver
// station software, not from anything about the laptop.
func (arena *Arena) registerStagingTeam(teamId int, stationIndex int) string {
	if !arena.EventSettings.AutoConfigureTeams {
		return ""
	}
	if arena.MatchState != PreMatch && arena.MatchState != FreePractice {
		return ""
	}
	station := stationOrder[stationIndex]

	if as := arena.AllianceStations[station]; as.Team != nil {
		// Already registered here: nothing to do, and the laptop is about to move onto
		// the team subnet anyway.
		return station
	}

	if _, err := arena.ensureTeamExists(teamId); err != nil {
		log.Printf("Error creating Team %d seen on the %s staging network: %v", teamId, station, err)
		return ""
	}
	if err := arena.registerTeamAtStation(teamId, station); err != nil {
		log.Printf("Error registering Team %d at %s: %v", teamId, station, err)
		return ""
	}
	log.Printf("Team %d connected on the %s staging network; registered to %s.", teamId, station, station)
	return station
}

// ensureTeamExists returns the team's record, creating it if the field has not seen it
// before. A team that turns up on a staging network is by definition unexpected.
func (arena *Arena) ensureTeamExists(teamId int) (*model.Team, error) {
	team, err := arena.Database.GetTeamById(teamId)
	if err != nil || team != nil {
		return team, err
	}
	team = &model.Team{Id: teamId, WpaKey: fmt.Sprintf("%08d", teamId)}
	if err := arena.Database.CreateTeam(team); err != nil {
		return nil, err
	}
	return team, nil
}
