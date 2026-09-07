// Copyright 2025 Team 841. All Rights Reserved.
//
// Tests that the wired team networks are configured everywhere the wireless ones are.

package field

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/team841/bioarena/model"
)

// fakeTeamNetwork records what would have been applied to the switch.
type fakeTeamNetwork struct {
	mutex       sync.Mutex
	applied     [][6]*model.Team
	entered     chan struct{} // signalled on entry, to observe a call reaching the hardware
	block       chan struct{} // when non-nil, held until closed, to keep a call in flight
	statusValue string
	links       [6]bool
	linksErr    error
}

func (f *fakeTeamNetwork) GetStationPortLinks() ([6]bool, error) {
	return f.links, f.linksErr
}

func (f *fakeTeamNetwork) ConfigureTeamEthernet(teams [6]*model.Team) error {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.applied = append(f.applied, teams)
	return nil
}

func (f *fakeTeamNetwork) GetStatus() string {
	return f.statusValue
}

// A laptop in the wrong station never gets an address, so it never connects, so no
// wrong-station check can fire. Link state is what catches it.
func TestPollStationPortLinks(t *testing.T) {
	arena := setupTestArena(t)
	reporter := &fakeTeamNetwork{links: [6]bool{true, false, false, true, false, false}}
	arena.teamNetwork = reporter

	arena.pollStationPortLinks()

	assert.True(t, arena.stationLinksKnown.Load())
	assert.True(t, arena.AllianceStations["R1"].PortLinked.Load())
	assert.False(t, arena.AllianceStations["R2"].PortLinked.Load())
	assert.True(t, arena.AllianceStations["B1"].PortLinked.Load())
}

// Not during a match: this reads the switch every thirty seconds and can cycle a port,
// neither of which belongs anywhere near a running match. Free practice does poll, because
// that is where a driver station most often needs rescuing.
func TestPollStationPortLinksSkippedDuringMatch(t *testing.T) {
	arena := setupTestArena(t)
	arena.teamNetwork = &fakeTeamNetwork{links: [6]bool{true, true, true, true, true, true}}

	for _, state := range []MatchState{StartMatch, WarmupPeriod, AutoPeriod, PausePeriod, TeleopPeriod, PostMatch} {
		arena.MatchState = state
		arena.pollStationPortLinks()
		assert.False(t, arena.stationLinksKnown.Load(), "state %d", state)
	}

	arena.MatchState = FreePractice
	arena.pollStationPortLinks()
	assert.True(t, arena.stationLinksKnown.Load(), "free practice should poll")
}

// An unreadable switch must not leave stale link state on display, and must not log the
// same complaint every thirty seconds for as long as the field runs.
func TestPollStationPortLinksForgetsOnError(t *testing.T) {
	arena := setupTestArena(t)
	reporter := &fakeTeamNetwork{links: [6]bool{true, false, false, false, false, false}}
	arena.teamNetwork = reporter
	arena.pollStationPortLinks()
	assert.True(t, arena.stationLinksKnown.Load())

	reporter.linksErr = fmt.Errorf("connection refused")
	arena.pollStationPortLinks()
	assert.False(t, arena.stationLinksKnown.Load())
}

// A controller that has just started is otherwise inert: nothing configures the field
// until someone loads a match, so the switch has no VLANs, a driver station plugged into
// it gets no address, and it cannot register itself.
func TestStartupConfiguresTheField(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)

	// What Run does before entering its loop, which a test cannot call directly.
	arena.setupNetwork(arena.currentTeams(), false)

	assert.Eventually(
		t,
		func() bool { return fake.applyCount() > 0 },
		2*time.Second,
		5*time.Millisecond,
		"the field should be configured at startup, not on the first match load",
	)
}

func TestCurrentTeams(t *testing.T) {
	arena, _ := setupTeamNetworkTestArena(t)
	assert.Equal(t, [6]*model.Team{}, arena.currentTeams())

	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R3", 841, "key"))

	teams := arena.currentTeams()
	assert.Equal(t, 841, teams[2].Id, "R3 is the third station")
	assert.Nil(t, teams[0])
}

// A free practice slot used to get an SSID but no VLAN subinterface and no DHCP scope, so
// a driver station wired to that station's port never received an address.
func TestSetFreePracticeSlotConfiguresTeamEthernet(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("B1", 841, "key"))

	teams := fake.lastApplied(t)
	assert.NotNil(t, teams[3], "B1 should have been configured")
	assert.Equal(t, 841, teams[3].Id)
	assert.Nil(t, teams[0], "R1 has no team and should not be configured")
}

func TestClearFreePracticeSlotConfiguresTeamEthernet(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("B1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[3].Id)

	assert.NoError(t, arena.ClearFreePracticeSlot("B1"))
	assert.Eventually(
		t,
		func() bool { return fake.lastApplied(t)[3] == nil },
		2*time.Second,
		5*time.Millisecond,
		"clearing a slot should remove its subnet",
	)
}

// Leaving free practice must take the subnets down with it; a station left routable to a
// team that has gone home is a subnet handing out leases nobody owns.
func TestExitFreePracticeTearsDownTeamEthernet(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[0].Id)

	assert.NoError(t, arena.ExitFreePractice())
	assert.Eventually(
		t,
		func() bool {
			teams := fake.lastApplied(t)
			return teams == [6]*model.Team{}
		},
		2*time.Second,
		5*time.Millisecond,
		"exiting free practice should tear every subnet down",
	)
}

// With network security off nothing is touched, matching how the wireless side behaves.
func TestFreePracticeSkipsTeamEthernetWhenSecurityDisabled(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	arena.EventSettings.NetworkSecurityEnabled = false

	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))

	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 0, fake.applyCount())
}

// DISABLE FIELD halts robots and nothing else: teams stay registered, the AP keeps its
// SSIDs, and the team subnets stay configured, so ENABLE FIELD resumes without anyone
// re-registering or re-connecting.
func TestDisableFieldLeavesNetworkingIntact(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[0].Id)

	appliedBefore := fake.applyCount()
	arena.DisableField()

	assert.True(t, arena.IsFieldDisabled())
	assert.Equal(t, FreePractice, arena.MatchState, "the field stays in free practice")
	assert.NotNil(t, arena.AllianceStations["R1"].Team, "the team stays registered")
	assert.Equal(t, appliedBefore, fake.applyCount(), "the wired network is not touched")

	arena.EnableField()
	assert.False(t, arena.IsFieldDisabled())
	assert.Equal(t, appliedBefore, fake.applyCount(), "resuming does not reconfigure either")
	assert.Equal(t, 841, arena.AllianceStations["R1"].Team.Id)
}

// Reset Field is the heavy option, and remains so.
func TestResetFieldTearsEverythingDown(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	assert.NoError(t, arena.SetFreePracticeSlot("R1", 841, "key"))
	assert.Equal(t, 841, fake.lastApplied(t)[0].Id)

	assert.NoError(t, arena.ExitFreePractice())
	assert.Equal(t, PreMatch, arena.MatchState)
	assert.Nil(t, arena.AllianceStations["R1"].Team)
	assert.Eventually(
		t,
		func() bool { return fake.lastApplied(t) == [6]*model.Team{} },
		2*time.Second,
		5*time.Millisecond,
		"reset should tear the subnets down",
	)
}

// A halt must not survive into the next session, in either direction.
func TestFieldDisableClearedOnEnterAndExit(t *testing.T) {
	arena, _ := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.EnterFreePractice())
	arena.DisableField()
	assert.NoError(t, arena.ExitFreePractice())
	assert.False(t, arena.IsFieldDisabled(), "reset should clear the halt")

	assert.NoError(t, arena.EnterFreePractice())
	arena.DisableField()
	assert.NoError(t, arena.ExitFreePractice())
	assert.NoError(t, arena.EnterFreePractice())
	assert.False(t, arena.IsFieldDisabled(), "entering free practice should start live")
}

// Registering teams one at a time queues a configuration per registration. Whichever
// goroutine reached the hardware last used to decide the field's state, which need not be
// the most recent team list; superseded requests must drop out instead.
func TestConfigureTeamEthernetAppliesOnlyTheLatestRequest(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	fake.entered = make(chan struct{}, 8)
	fake.block = make(chan struct{})

	// First request: hold it inside the hardware call so the rest queue behind it.
	var teams [6]*model.Team
	teams[0] = &model.Team{Id: 100}
	arena.configureTeamEthernet(teams)
	<-fake.entered

	// Three more arrive while the first is still in flight.
	for i := 1; i <= 3; i++ {
		teams[i] = &model.Team{Id: 100 + i}
		arena.configureTeamEthernet(teams)
	}
	close(fake.block)

	// The one in flight completes and the newest queued request applies. The two
	// superseded in between never reach the hardware.
	assert.Eventually(
		t,
		func() bool { return fake.applyCount() == 2 },
		2*time.Second,
		5*time.Millisecond,
		fmt.Sprintf("expected exactly two applications, got %d", fake.applyCount()),
	)

	last := fake.lastApplied(t)
	for i := 0; i <= 3; i++ {
		assert.NotNil(t, last[i], fmt.Sprintf("station %d missing from the final configuration", i))
	}
}

// setupTeamNetworkTestArena returns an arena with the wired network faked and network
// security on. The access point is left as built by LoadSettings, which has security off
// and so returns without attempting any HTTP.
func setupTeamNetworkTestArena(t *testing.T) (*Arena, *fakeTeamNetwork) {
	arena := setupTestArena(t)
	fake := &fakeTeamNetwork{}
	arena.teamNetwork = fake
	arena.EventSettings.NetworkSecurityEnabled = true
	return arena, fake
}

func (f *fakeTeamNetwork) applyCount() int {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return len(f.applied)
}

// lastApplied reports the most recent configuration, waiting briefly for the background
// goroutine to run.
func (f *fakeTeamNetwork) lastApplied(t *testing.T) [6]*model.Team {
	t.Helper()
	var last [6]*model.Team
	assert.Eventually(
		t,
		func() bool {
			f.mutex.Lock()
			defer f.mutex.Unlock()
			if len(f.applied) == 0 {
				return false
			}
			last = f.applied[len(f.applied)-1]
			return true
		},
		2*time.Second,
		5*time.Millisecond,
		"expected the wired network to be configured",
	)
	return last
}
