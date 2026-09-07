// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package network

import (
	"bytes"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/team841/bioarena/model"
	"net"
	"sync"
	"testing"
	"time"
)

func TestConfigureSwitch(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	assert.Equal(t, "UNKNOWN", sw.Status)
	sw.port = 9050
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond
	expectedResetCommand := "password\nenable\npassword\nterminal length 0\nconfig terminal\n" +
		"interface Vlan10\nno ip address\nno ip dhcp pool dhcp10\n" +
		"interface Vlan20\nno ip address\nno ip dhcp pool dhcp20\n" +
		"interface Vlan30\nno ip address\nno ip dhcp pool dhcp30\n" +
		"interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n" +
		"interface Vlan50\nno ip address\nno ip dhcp pool dhcp50\n" +
		"interface Vlan60\nno ip address\nno ip dhcp pool dhcp60\n" +
		"end\nexit\n"

	// First configuration of a run, with no teams: the baseline and its save, then the VLAN
	// removal. Three connections and no more -- an empty station gets no subnet at all, so
	// there is nothing to add, and no driver station port is touched.
	commands := mockTelnetMulti(t, sw.port, 3)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(0), "ip routing\n")
	assert.Contains(t, commands.at(1), "write memory")
	assert.Equal(t, expectedResetCommand, commands.at(2))
	assert.Equal(t, "ACTIVE", sw.Status)

	// One team present. Only B2 changed, so only its VLAN is rebuilt: one removal and one
	// add, and nothing at all for the five stations already as wanted.
	sw.port += 1
	commands = mockTelnetMulti(t, sw.port, 2)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, {Id: 254}, nil}))
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"interface Vlan50\nno ip address\nno ip dhcp pool dhcp50\n"+
			"end\nexit\n",
		commands.at(0),
	)
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"ip dhcp excluded-address 10.2.54.1 10.2.54.19\nip dhcp excluded-address 10.2.54.200 10.2.54.254\nip dhcp pool dhcp50\n"+
			"network 10.2.54.0 255.255.255.0\ndefault-router 10.2.54.4\nlease 7\n"+
			"interface Vlan50\nip address 10.2.54.4 255.255.255.0\n"+
			"end\nexit\n",
		commands.at(1),
	)

	// Should configure all teams if all are present. Every station changes here -- B2
	// swaps 254 for 1678 -- so this is a full six-VLAN rebuild again.
	sw.port += 1
	commands = mockTelnetMulti(t, sw.port, 2)
	assert.Nil(
		t,
		sw.ConfigureTeamEthernet([6]*model.Team{{Id: 1114}, {Id: 254}, {Id: 296}, {Id: 1503}, {Id: 1678}, {Id: 1538}}),
	)
	assert.Equal(t, expectedResetCommand, commands.at(0))
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"ip dhcp excluded-address 10.11.14.1 10.11.14.19\nip dhcp excluded-address 10.11.14.200 10.11.14.254\nip dhcp pool dhcp10\n"+
			"network 10.11.14.0 255.255.255.0\ndefault-router 10.11.14.4\nlease 7\n"+
			"interface Vlan10\nip address 10.11.14.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.2.54.1 10.2.54.19\nip dhcp excluded-address 10.2.54.200 10.2.54.254\nip dhcp pool dhcp20\n"+
			"network 10.2.54.0 255.255.255.0\ndefault-router 10.2.54.4\nlease 7\n"+
			"interface Vlan20\nip address 10.2.54.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.2.96.1 10.2.96.19\nip dhcp excluded-address 10.2.96.200 10.2.96.254\nip dhcp pool dhcp30\n"+
			"network 10.2.96.0 255.255.255.0\ndefault-router 10.2.96.4\nlease 7\n"+
			"interface Vlan30\nip address 10.2.96.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.15.3.1 10.15.3.19\nip dhcp excluded-address 10.15.3.200 10.15.3.254\nip dhcp pool dhcp40\n"+
			"network 10.15.3.0 255.255.255.0\ndefault-router 10.15.3.4\nlease 7\n"+
			"interface Vlan40\nip address 10.15.3.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.16.78.1 10.16.78.19\nip dhcp excluded-address 10.16.78.200 10.16.78.254\nip dhcp pool dhcp50\n"+
			"network 10.16.78.0 255.255.255.0\ndefault-router 10.16.78.4\nlease 7\n"+
			"interface Vlan50\nip address 10.16.78.4 255.255.255.0\n"+
			"ip dhcp excluded-address 10.15.38.1 10.15.38.19\nip dhcp excluded-address 10.15.38.200 10.15.38.254\nip dhcp pool dhcp60\n"+
			"network 10.15.38.0 255.255.255.0\ndefault-router 10.15.38.4\nlease 7\n"+
			"interface Vlan60\nip address 10.15.38.4 255.255.255.0\n"+
			"end\nexit\n",
		commands.at(1),
	)
}

// An unset switch address means no switch, not a broken one. Dialing it on every match
// load fails and pins the badge red, which reads as a fault rather than an absence.
func TestConfigureSwitchWithoutAddress(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "", Password: "password"})
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "DISABLED", sw.Status)
}

func newIncrementalTestSwitch(port int) *Switch {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	sw.port = port
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond
	return sw
}

// Registering one team must not disturb a robot being driven from another station. The
// full rebuild shuts every driver station port, which disconnects all six for as long as
// the reconfiguration takes -- tolerable between matches, not during free practice.
func TestConfigureSwitchOnlyTouchesChangedStations(t *testing.T) {
	sw := newIncrementalTestSwitch(9100)

	// First call reconciles in full: the switch's state is unknown at startup, so every VLAN
	// is rebuilt. Baseline, its save, the removal, then the one add.
	commands := mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "ACTIVE", sw.Status)
	assert.Contains(t, commands.at(0), "ip routing\n")
	assert.Contains(t, commands.at(2), "interface Vlan60\nno ip address")
	assert.Contains(t, commands.at(3), "interface Vlan10\nip address 10.8.41.4 255.255.255.0\n")

	// Second call adds B1 only: one removal and one add, naming nothing but Vlan40.
	sw.port++
	commands = mockTelnetMulti(t, sw.port, 2)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))
	assert.Contains(t, commands.at(0), "interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n")
	assert.NotContains(t, commands.at(0), "Vlan10")
	assert.Contains(t, commands.at(1), "interface Vlan40\nip address 10.2.54.4 255.255.255.0\n")
	assert.NotContains(t, commands.at(1), "10.8.41")

	// Nothing shuts a driver station port any more. That cycling was what made a laptop
	// re-request an address on its new subnet, and it cost seconds on every match load.
	for i := 0; i < 2; i++ {
		assert.NotContains(t, commands.at(i), "shutdown")
		assert.NotContains(t, commands.at(i), "GigabitEthernet")
	}
}

// Clearing a station removes its VLAN and nothing else. Upstream leaves an empty station
// with no subnet; the staging subnet that used to replace it existed only so an unregistered
// laptop could announce itself, which is the auto-registration this fork no longer does.
func TestConfigureSwitchClearsOneStation(t *testing.T) {
	sw := newIncrementalTestSwitch(9110)
	mockTelnetMulti(t, sw.port, 4) // baseline and its save precede the first configuration
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	// Only the removal is left: with B1 empty there is nothing to add, so the add command
	// is never sent at all.
	sw.port++
	commands := mockTelnetMulti(t, sw.port, 1)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(0), "interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n")
	assert.NotContains(t, commands.at(0), "Vlan10")
	assert.NotContains(t, commands.at(0), "staging")
}

// Bioarena applies the switch's standing configuration itself, so that setting up a field
// is wiring plus the bootstrap script and nobody composes IOS by hand.
func TestBaselineCommands(t *testing.T) {
	command := baselineCommands()

	assert.Contains(t, command, "ip routing\n")

	// The switch takes its time from the field controller. Without this its log timestamps
	// sit years away from bioarena's, since a Catalyst boots believing it is 2004 and an
	// isolated field gives it no way to find out otherwise.
	assert.Contains(t, command, "ntp server 10.0.100.5\n")

	// The access point normally sits at 10.0.100.2, but falls back to 192.168.69.1 after a
	// reset, so the field VLAN carries that subnet as a secondary. Without an interface
	// there, an AP that has dropped back is unreachable from the field entirely: nothing to
	// route through and nothing to ARP for.
	assert.Contains(
		t,
		command,
		"interface Vlan1\nip address 192.168.69.2 255.255.255.0 secondary\n",
	)

	assert.Contains(t, command, "vlan 10\nname Red1\n")
	assert.Contains(t, command, "vlan 60\nname Blue3\n")

	// Access ports, one per station, with portfast -- these are shut and reopened on
	// every reconfiguration, and without portfast each return walks spanning tree while
	// the laptop is trying to get an address.
	assert.Contains(
		t,
		command,
		"interface GigabitEthernet0/1\nswitchport mode access\nswitchport access vlan 10\n"+
			"spanning-tree portfast\nno shutdown\n",
	)
	assert.Contains(t, command, "interface GigabitEthernet0/6\nswitchport mode access\nswitchport access vlan 60\n")

	// Trunks carry every VLAN: the access point tags each team's SSID onto that team's
	// VLAN, so an access port there strands every robot.
	assert.Contains(
		t,
		command,
		"interface GigabitEthernet0/7\nswitchport mode trunk\n"+
			"switchport trunk allowed vlan 1,10,20,30,40,50,60\n",
	)
	assert.Contains(t, command, "interface GigabitEthernet0/8\nswitchport mode trunk\n")

	// A 3560-CX speaks only 802.1Q and rejects the encapsulation command that older
	// platforms require before "switchport mode trunk" is accepted.
	assert.NotContains(t, command, "encapsulation")

	// The DMX/Art-Net gateway is an access port on VLAN 1, the FMS Pi's own subnet, so it
	// can reach the FMS directly. Unlike a driver station port it carries no VLAN of its
	// own and is never shut and reopened, since nothing else on the field needs to reach it.
	assert.Contains(
		t,
		command,
		"interface GigabitEthernet0/9\nswitchport mode access\nswitchport access vlan 1\nno shutdown\n",
	)
}

// The baseline goes on with the first configuration of a run, and is saved -- so a power
// cycle brings the switch back able to run a field on its own.
func TestConfigureSwitchAppliesBaseline(t *testing.T) {
	sw := newIncrementalTestSwitch(9140)

	// Baseline, write memory, ports down, VLANs removed, VLANs added, ports up.
	commands := mockTelnetMulti(t, sw.port, 6)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(0), "ip routing\n")
	assert.Contains(t, commands.at(0), "spanning-tree portfast\n")
	assert.Contains(t, commands.at(1), "write memory")

	// Not again on the next configuration: the switch's state is known by then, and
	// saving later would bake whichever teams are on the field into the startup config.
	sw.port++
	commands = mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, {Id: 254}, nil, nil, nil, nil}))
	assert.NotContains(t, commands.at(0), "ip routing")
	assert.NotContains(t, commands.at(0), "write memory")
	assert.NotContains(t, commands.at(1), "write memory")
}

func TestConfigureSwitchSkipsUnchangedTeams(t *testing.T) {
	sw := newIncrementalTestSwitch(9120)
	mockTelnetMulti(t, sw.port, 6) // baseline and its save precede the first configuration
	teams := [6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}
	assert.Nil(t, sw.ConfigureTeamEthernet(teams))

	sw.port++ // nothing listening here; a connection attempt would fail the call
	assert.Nil(t, sw.ConfigureTeamEthernet(teams))
	assert.Equal(t, "ACTIVE", sw.Status)

	// Identity is the team number, so a fresh record for the same team is unchanged too.
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841, WpaKey: "x"}, nil, nil, nil, nil, nil}))
}

// Link state is the only way to catch a laptop plugged into the wrong station: every other
// check runs downstream of a driver station connection that such a laptop can never make.
func TestParsePortLinks(t *testing.T) {
	// IOS abbreviates the interface name here -- Gi0/1 for the GigabitEthernet0/1 it
	// accepts in configuration -- so the parse matches on the numeric suffix.
	output := "Port      Name               Status       Vlan       Duplex  Speed Type\n" +
		"Gi0/1                        connected    10         a-full a-1000 10/100/1000BaseTX\n" +
		"Gi0/2                        notconnect   20           auto   auto 10/100/1000BaseTX\n" +
		"Gi0/3                        err-disabled 30           auto   auto 10/100/1000BaseTX\n" +
		"Gi0/4                        connected    40         a-full a-1000 10/100/1000BaseTX\n" +
		"Gi0/7                        connected    trunk      a-full a-1000 10/100/1000BaseTX\n"

	assert.Equal(t, [6]bool{true, false, false, true, false, false}, parsePortLinks(output))
}

// A port description shifts every column to its right, so the status cannot be read by
// position.
func TestParsePortLinksWithDescriptions(t *testing.T) {
	output := "Port      Name               Status       Vlan       Duplex  Speed Type\n" +
		"Gi0/1     red 1 station        connected    10         a-full a-1000 10/100/1000BaseTX\n" +
		"Gi0/2     red 2 station        notconnect   20           auto   auto 10/100/1000BaseTX\n"

	assert.Equal(t, [6]bool{true, false, false, false, false, false}, parsePortLinks(output))
}

func TestParsePortLinksIgnoresJunk(t *testing.T) {
	assert.Equal(t, [6]bool{}, parsePortLinks(""))
	assert.Equal(t, [6]bool{}, parsePortLinks("% Invalid input detected at '^' marker.\n"))
}

// An unconfigured switch reports an error rather than six unplugged stations, which would
// otherwise read as a field with every cable out.
func TestGetStationPortLinksWithoutAddress(t *testing.T) {
	sw := NewSwitch(SwitchConfig{})
	links, err := sw.GetStationPortLinks()
	assert.NotNil(t, err)
	assert.Equal(t, [6]bool{}, links)
}

// Success is otherwise silent, so the log line is what distinguishes a working field from
// one where the configuration never ran -- both show the same badge.
func TestDescribeStations(t *testing.T) {
	assert.Equal(
		t,
		"R1 841, B1 254",
		describeStations([6]int{841, 0, 0, 254, 0, 0}, [6]bool{true, false, false, true, false, false}),
	)

	// A station emptied this reconfiguration is worth naming; one that was already empty
	// and unchanged is not.
	assert.Equal(
		t,
		"R2 cleared",
		describeStations([6]int{841, 0, 0, 0, 0, 0}, [6]bool{false, true, false, false, false, false}),
	)

	assert.Equal(t, "no stations", describeStations([6]int{}, [6]bool{}))
}

// The port map is fixed by the reference field wiring: station N on GigabitEthernet0/N.

// commandLog collects the commands received across several Telnet sessions.
type commandLog struct {
	mutex    sync.Mutex
	commands []string
}

func (log *commandLog) append(command string) {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	log.commands = append(log.commands, command)
}

func (log *commandLog) at(i int) string {
	log.mutex.Lock()
	defer log.mutex.Unlock()
	if i >= len(log.commands) {
		return ""
	}
	return log.commands[i]
}

// mockTelnetMulti accepts the given number of connections, recording each in order.
func mockTelnetMulti(t *testing.T, port int, connections int) *commandLog {
	log := &commandLog{}
	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		assert.Nil(t, err)
		defer ln.Close()

		for i := 0; i < connections; i++ {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
			var reader bytes.Buffer
			reader.ReadFrom(conn)
			log.append(reader.String())
			conn.Close()
		}
	}()
	time.Sleep(100 * time.Millisecond) // Give it some time to open the socket.
	return log
}

func mockTelnetSingleWithResponse(t *testing.T, port int, response string, command *string) {
	go func() {
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		assert.Nil(t, err)
		defer ln.Close()
		*command = ""

		conn, err := ln.Accept()
		assert.Nil(t, err)
		conn.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
		var reader bytes.Buffer
		reader.ReadFrom(conn)
		*command = reader.String()
		conn.Write([]byte(response))
		conn.Close()
	}()
	time.Sleep(100 * time.Millisecond)
}

// The stock driver-station port commands do not end in a newline, so "end" was appended
// to the last line and IOS rejected "no shutdownend". A Telnet read timeout counts as
// success, so the port cycling failed silently on every match load.
func TestConfigCommandTerminatesLastLine(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	sw.port = 9080
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond

	var command1, command2 string
	mockTelnetSingleWithResponse(t, sw.port, "", &command1)
	_, err := sw.runConfigCommand("interface range GigabitEthernet0/1-6\nshutdown")
	assert.Nil(t, err)
	assert.Contains(t, command1, "shutdown\nend\n")
	assert.NotContains(t, command1, "shutdownend")

	sw.port++
	mockTelnetSingleWithResponse(t, sw.port, "", &command2)
	_, err = sw.runConfigCommand("interface range GigabitEthernet0/1-6\nno shutdown")
	assert.Nil(t, err)
	assert.Contains(t, command2, "no shutdown\nend\n")
	assert.NotContains(t, command2, "no shutdownend")
}

// A command already ending in a newline must not gain a second one.
func TestConfigCommandDoesNotDoubleTerminate(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	sw.port = 9082

	var command string
	mockTelnetSingleWithResponse(t, sw.port, "", &command)
	_, err := sw.runConfigCommand("interface Vlan10\nno ip address\n")
	assert.Nil(t, err)
	assert.Contains(t, command, "no ip address\nend\n")
	assert.NotContains(t, command, "no ip address\n\nend")
}

// The DHCP pools carry a DNS server only when one is configured. An unreachable
// resolver makes every lookup wait for a timeout, so blank must omit the option rather
// than emit an empty or placeholder value.
func TestConfigureSwitchDnsServer(t *testing.T) {
	// Configured: the option appears in the pool, after default-router.
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password", DnsServer: "10.0.100.5"})
	sw.port = 9090
	sw.configBackoffDuration = time.Millisecond
	sw.configPauseDuration = time.Millisecond

	// Four sessions: the baseline and its save, then the VLANs removed and added.
	commands := mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(3), "default-router 10.8.41.4\ndns-server 10.0.100.5\nlease 7\n")

	// Blank: no dns-server line at all, and the surrounding pool is unchanged.
	swNoDns := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	swNoDns.port = 9092
	swNoDns.configBackoffDuration = time.Millisecond
	swNoDns.configPauseDuration = time.Millisecond

	commands = mockTelnetMulti(t, swNoDns.port, 4)
	assert.Nil(t, swNoDns.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.NotContains(t, commands.at(3), "dns-server")
	assert.Contains(t, commands.at(3), "default-router 10.8.41.4\nlease 7\n")
}

// One shut for every selected port and one reopen, so six stations cost the same two round
// trips and one pause as one station does. The old per-station cycle did this six times.
func TestCycleStationPortsBatchesEveryPort(t *testing.T) {
	sw := newIncrementalTestSwitch(9200)

	commands := mockTelnetMulti(t, sw.port, 2)
	assert.Nil(t, sw.CycleStationPorts([6]bool{true, false, false, true, false, true}))

	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/1\nshutdown\n")
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/4\nshutdown\n")
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/6\nshutdown\n")
	assert.NotContains(t, commands.at(0), "GigabitEthernet0/2")

	assert.Contains(t, commands.at(1), "interface GigabitEthernet0/1\nno shutdown\n")
	assert.Contains(t, commands.at(1), "interface GigabitEthernet0/6\nno shutdown\n")
}

// Nothing selected must not open a Telnet session at all.
func TestCycleStationPortsWithNoStationsIsANoOp(t *testing.T) {
	sw := newIncrementalTestSwitch(9210)
	assert.Nil(t, sw.CycleStationPorts([6]bool{}))
}

// An absent switch is not a broken one, and a port cycle it cannot do must say so rather
// than hanging the caller on a dial that cannot succeed.
func TestCycleStationPortsWithoutAddress(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "", Password: "password"})
	assert.ErrorIs(t, sw.CycleStationPorts([6]bool{true}), errSwitchNotConfigured)
}
