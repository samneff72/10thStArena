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
	cycled      [][6]bool
	cycleErr    error
}

func (f *fakeTeamNetwork) GetStationPortLinks() ([6]bool, error) {
	return f.links, f.linksErr
}

func (f *fakeTeamNetwork) CycleStationPorts(stations [6]bool) error {
	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.cycled = append(f.cycled, stations)
	return f.cycleErr
}

// cycledPorts reports the port-cycle requests that reached the switch, waiting briefly for
// the goroutine that makes them.
func (f *fakeTeamNetwork) cycledPorts() [][6]bool {
	for i := 0; i < 50; i++ {
		f.mutex.Lock()
		n := len(f.cycled)
		f.mutex.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	return append([][6]bool(nil), f.cycled...)
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

	// PreMatch is when the operator is setting up and the poll is worth having: it is the
	// only thing that sees a cable in a station with no team registered.
	arena.MatchState = PreMatch
	arena.pollStationPortLinks()
	assert.True(t, arena.stationLinksKnown.Load(), "pre-match should poll")
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

	assert.NoError(t, arena.Database.CreateTeam(&model.Team{Id: 841, WpaKey: "key"}))
	assert.NoError(t, arena.assignTeam(841, "R3"))

	teams := arena.currentTeams()
	assert.Equal(t, 841, teams[2].Id, "R3 is the third station")
	assert.Nil(t, teams[0])
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

// --- driver station address renewal ---

// Driver station software releases its address at the end of a match and Windows then sits
// unaddressed. Bouncing the link is what prompts a fresh DHCP request, so clearing the match
// has to do it or the operator waits out a retry before the next round.
func TestClearMatchCyclesOccupiedStationPorts(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.Database.CreateTeam(&model.Team{Id: 841}))
	assert.NoError(t, arena.Database.CreateTeam(&model.Team{Id: 254}))
	assert.NoError(t, arena.assignTeam(841, "R1"))
	assert.NoError(t, arena.assignTeam(254, "B2"))

	arena.MatchState = PostMatch
	assert.NoError(t, arena.ClearMatch())

	cycled := fake.cycledPorts()
	if assert.Len(t, cycled, 1, "one batched cycle, not one per station") {
		// R1 and B2 only. An empty station has no subnet to get an address from, so
		// bouncing it accomplishes nothing and only disturbs whatever is plugged in.
		assert.Equal(t, [6]bool{true, false, false, false, true, false}, cycled[0])
	}
}

// A field with nothing registered has nothing to renew, and should not touch the switch.
func TestClearMatchWithNoTeamsDoesNotCyclePorts(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)

	arena.MatchState = PostMatch
	assert.NoError(t, arena.ClearMatch())

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, fake.cycled, "no teams, so no ports worth cycling")
}

// Network security off means bioarena leaves the hardware alone entirely, port cycling
// included -- otherwise a bench run would sit waiting on a Telnet dial that cannot succeed.
func TestClearMatchSkipsPortCycleWhenSecurityDisabled(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.Database.CreateTeam(&model.Team{Id: 841}))
	assert.NoError(t, arena.assignTeam(841, "R1"))
	arena.EventSettings.NetworkSecurityEnabled = false

	arena.MatchState = PostMatch
	assert.NoError(t, arena.ClearMatch())

	time.Sleep(50 * time.Millisecond)
	assert.Empty(t, fake.cycled)
}

// --- field reset ---

// Reset Field is the heavy option, and the one Clear Match deliberately is not: the lineup
// goes, not just the match. Anything less and the operator has to unregister six stations
// by hand at the end of a session.
func TestResetFieldClearsEverything(t *testing.T) {
	arena, fake := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.Database.CreateTeam(&model.Team{Id: 841}))
	// Through the real registration path, so CurrentMatch carries the lineup too -- that is
	// what a clear reads back, and a station set directly would make this test vacuous.
	assert.NoError(t, arena.SubstituteTeams(841, 0, 0, 0, 0, 0))
	arena.AllianceStations["R2"].Bypass.Store(true)
	arena.AllianceStations["R1"].EStop.Store(true)

	arena.MatchState = PostMatch
	assert.NoError(t, arena.ResetField())

	assert.Equal(t, PreMatch, arena.MatchState)
	for _, station := range stationOrder {
		as := arena.AllianceStations[station]
		assert.Nil(t, as.Team, "%s should be unregistered", station)
		assert.False(t, as.Bypass.Load(), "%s bypass should be cleared", station)
		assert.False(t, as.EStop.Load(), "%s e-stop should be cleared", station)
	}
	assert.Equal(t, 0, arena.CurrentMatch.Red1)

	// The wired side is torn down, not left routable to a team that has gone home.
	assert.Equal(t, [6]*model.Team{}, fake.lastApplied(t))
}

// Clear Match is the opposite choice and must stay that way: the same lineup runs round
// after round on a practice field.
func TestClearMatchKeepsTeamsWhereResetFieldDoesNot(t *testing.T) {
	arena, _ := setupTeamNetworkTestArena(t)
	assert.NoError(t, arena.Database.CreateTeam(&model.Team{Id: 841}))
	assert.NoError(t, arena.SubstituteTeams(841, 0, 0, 0, 0, 0))

	arena.MatchState = PostMatch
	assert.NoError(t, arena.ClearMatch())
	assert.NotNil(t, arena.AllianceStations["R1"].Team, "clearing a match keeps the lineup")

	arena.MatchState = PostMatch
	assert.NoError(t, arena.ResetField())
	assert.Nil(t, arena.AllianceStations["R1"].Team, "resetting the field does not")
}

// A match in progress must not be torn down underneath the robots driving in it.
func TestResetFieldRejectedDuringMatch(t *testing.T) {
	arena, _ := setupTeamNetworkTestArena(t)
	for _, state := range []MatchState{StartMatch, WarmupPeriod, AutoPeriod, PausePeriod, TeleopPeriod} {
		arena.MatchState = state
		assert.ErrorContains(t, arena.ResetField(), "while a match is in progress", "state %d", state)
	}
}
