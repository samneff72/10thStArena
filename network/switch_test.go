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
		"interface Vlan10\nno ip address\nno ip dhcp pool dhcp10\nno ip dhcp pool staging10\n" +
		"interface Vlan20\nno ip address\nno ip dhcp pool dhcp20\nno ip dhcp pool staging20\n" +
		"interface Vlan30\nno ip address\nno ip dhcp pool dhcp30\nno ip dhcp pool staging30\n" +
		"interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\nno ip dhcp pool staging40\n" +
		"interface Vlan50\nno ip address\nno ip dhcp pool dhcp50\nno ip dhcp pool staging50\n" +
		"interface Vlan60\nno ip address\nno ip dhcp pool dhcp60\nno ip dhcp pool staging60\n" +
		"end\nexit\n"

	// First configuration of a run: the baseline and its save come first, then every port
	// is cycled, every VLAN removed, and every station given a staging subnet so a laptop
	// plugged into it can still say which team it is.
	commands := mockTelnetMulti(t, sw.port, 6)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(0), "ip routing\n")
	assert.Contains(t, commands.at(1), "write memory")
	assert.Contains(t, commands.at(2), "interface GigabitEthernet0/1\nshutdown\n")
	assert.Equal(t, expectedResetCommand, commands.at(3))
	assert.Contains(t, commands.at(4), "ip dhcp pool staging10\nnetwork 172.16.10.0 255.255.255.0\n")

	// "lease 0 0 5" is five minutes. IOS reads a bare number as days, so "lease 5" would
	// hold an address for five days that a laptop should keep for seconds.
	assert.Contains(t, commands.at(4), "lease 0 0 5\n")
	assert.Contains(t, commands.at(4), "interface Vlan60\nip address 172.16.60.1 255.255.255.0\n")
	assert.Contains(t, commands.at(5), "interface GigabitEthernet0/1\nno shutdown\n")
	assert.Equal(t, "ACTIVE", sw.Status)

	// Should configure one team if only one is present. Only B2 changed, so only its port
	// is cycled and only its VLAN rebuilt -- the other five are already as wanted.
	sw.port += 1
	commands = mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{nil, nil, nil, nil, {Id: 254}, nil}))
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/5\nshutdown\n")
	assert.NotContains(t, commands.at(0), "GigabitEthernet0/1")
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"interface Vlan50\nno ip address\nno ip dhcp pool dhcp50\nno ip dhcp pool staging50\n"+
			"end\nexit\n",
		commands.at(1),
	)
	assert.Equal(
		t,
		"password\nenable\npassword\nterminal length 0\nconfig terminal\n"+
			"ip dhcp excluded-address 10.2.54.1 10.2.54.19\nip dhcp excluded-address 10.2.54.200 10.2.54.254\nip dhcp pool dhcp50\n"+
			"network 10.2.54.0 255.255.255.0\ndefault-router 10.2.54.4\nlease 7\n"+
			"interface Vlan50\nip address 10.2.54.4 255.255.255.0\n"+
			"end\nexit\n",
		commands.at(2),
	)
	assert.Contains(t, commands.at(3), "interface GigabitEthernet0/5\nno shutdown\n")

	// Should configure all teams if all are present. Every station changes here -- B2
	// swaps 254 for 1678 -- so this is a full six-VLAN rebuild again.
	sw.port += 1
	commands = mockTelnetMulti(t, sw.port, 4)
	assert.Nil(
		t,
		sw.ConfigureTeamEthernet([6]*model.Team{{Id: 1114}, {Id: 254}, {Id: 296}, {Id: 1503}, {Id: 1678}, {Id: 1538}}),
	)
	assert.Equal(t, expectedResetCommand, commands.at(1))
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
		commands.at(2),
	)
}

// An unset switch address means no switch, not a broken one. Dialing it on every match
// load fails and pins the badge red, which reads as a fault rather than an absence.
func TestConfigureSwitchWithoutAddress(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "", Password: "password"})
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "DISABLED", sw.Status)
}

func TestGetStationForTeamId(t *testing.T) {
	sw := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	sw.port = 9060

	ciscoArpOutput := "password\nenable\npassword\nterminal length 0\n" +
		"Protocol  Address     Age(min)  Hardware Addr   Type   Interface\n" +
		"Internet  10.2.54.5       2     0050.b6ff.ee5   ARPA   Vlan20\n" +
		"exit\n"

	// Returns correct station when switch ARP table has an entry.
	var command string
	mockTelnetSingleWithResponse(t, sw.port, ciscoArpOutput, &command)
	station, err := sw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "R2", station)

	// Returns "" when ARP table has no Vlan entry.
	sw.port++
	noArpOutput := "password\nenable\npassword\nterminal length 0\n% IP ARP table is empty\nexit\n"
	mockTelnetSingleWithResponse(t, sw.port, noArpOutput, &command)
	station, err = sw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "", station)

	// Returns "" when VLAN is not in the known map.
	sw.port++
	unknownVlanOutput := "password\nenable\npassword\nterminal length 0\nInternet  10.2.54.5  2  0050.b6ff.ee5  ARPA  Vlan99\nexit\n"
	mockTelnetSingleWithResponse(t, sw.port, unknownVlanOutput, &command)
	station, err = sw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "", station)

	// Returns "" when switch address is empty.
	emptySw := NewSwitch(SwitchConfig{Address: "", Password: "password"})
	station, err = emptySw.GetStationForTeamId(254)
	assert.Nil(t, err)
	assert.Equal(t, "", station)
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

	// First call reconciles in full: the switch's state is unknown at startup, so every
	// station's port is cycled and every VLAN rebuilt.
	commands := mockTelnetMulti(t, sw.port, 6) // baseline and its save precede the first configuration
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Equal(t, "ACTIVE", sw.Status)
	assert.Contains(t, commands.at(0), "ip routing\n")
	assert.Contains(t, commands.at(2), "interface GigabitEthernet0/1\nshutdown\n")
	assert.Contains(t, commands.at(2), "interface GigabitEthernet0/6\nshutdown\n")
	assert.Contains(t, commands.at(3), "interface Vlan60\nno ip address")

	// Second call adds B1 only.
	sw.port++
	commands = mockTelnetMulti(t, sw.port, 4)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	// Only B1's port is cycled, and only its VLAN rebuilt.
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/4\nshutdown\n")
	assert.NotContains(t, commands.at(0), "GigabitEthernet0/1")
	assert.NotContains(t, commands.at(0), "range")
	assert.Contains(t, commands.at(1), "interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n")
	assert.NotContains(t, commands.at(1), "Vlan10")
	assert.Contains(t, commands.at(2), "interface Vlan40\nip address 10.2.54.4 255.255.255.0\n")
	assert.NotContains(t, commands.at(2), "10.8.41")
	assert.Contains(t, commands.at(3), "interface GigabitEthernet0/4\nno shutdown\n")
	assert.NotContains(t, commands.at(3), "GigabitEthernet0/1")
}

// Clearing a station removes its VLAN and cycles its port, and nothing else.
func TestConfigureSwitchClearsOneStation(t *testing.T) {
	sw := newIncrementalTestSwitch(9110)
	mockTelnetMulti(t, sw.port, 6) // baseline and its save precede the first configuration
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, {Id: 254}, nil, nil}))

	// Clearing B1 replaces its team subnet with a staging one rather than leaving the
	// station dead, so there is still a third command to add.
	sw.port++
	commands := mockTelnetMulti(t, sw.port, 6) // baseline and its save precede the first configuration
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(0), "interface GigabitEthernet0/4\nshutdown\n")
	assert.Contains(t, commands.at(1), "interface Vlan40\nno ip address\nno ip dhcp pool dhcp40\n")
	assert.NotContains(t, commands.at(1), "Vlan10")
	assert.Contains(t, commands.at(2), "ip dhcp pool staging40\nnetwork 172.16.40.0 255.255.255.0\n")
	assert.NotContains(t, commands.at(2), "10.8.41")
	assert.Contains(t, commands.at(3), "interface GigabitEthernet0/4\nno shutdown\n")
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

// A staging address names the port it came from: the VLAN is its third octet, so the
// station a driver station is plugged into follows from the address alone.
func TestStagingStationForAddress(t *testing.T) {
	for _, testCase := range []struct {
		address string
		station int
		staging bool
	}{
		{"172.16.10.25", 0, true},
		{"172.16.40.199", 3, true},
		{"172.16.60.20", 5, true},
		{"172.16.99.20", 0, false}, // not a station VLAN
		{"10.8.41.5", 0, false},    // a team subnet
		{"10.0.100.5", 0, false},   // the FMS itself
		{"172.16.10", 0, false},
		{"", 0, false},
	} {
		station, staging := StagingStationForAddress(testCase.address)
		assert.Equal(t, testCase.staging, staging, testCase.address)
		if testCase.staging {
			assert.Equal(t, testCase.station, station, testCase.address)
		}
	}
}

// An unchanged team list touches the switch not at all -- no Telnet session, so nothing to
// mock. Anything else would cycle ports for no reason.
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
func TestSwitchPortCommands(t *testing.T) {
	assert.Equal(
		t,
		"interface GigabitEthernet0/1\nshutdown\ninterface GigabitEthernet0/4\nshutdown\n",
		portCommands([6]bool{true, false, false, true, false, false}, "shutdown"),
	)
	assert.Equal(t, "", portCommands([6]bool{}, "shutdown"))
}

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

	// Six sessions: the baseline and its save, then ports down, VLANs removed, VLANs
	// added, ports up.
	commands := mockTelnetMulti(t, sw.port, 6)
	assert.Nil(t, sw.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.Contains(t, commands.at(4), "default-router 10.8.41.4\ndns-server 10.0.100.5\nlease 7\n")

	// Blank: no dns-server line at all, and the surrounding pool is unchanged.
	swNoDns := NewSwitch(SwitchConfig{Address: "127.0.0.1", Password: "password"})
	swNoDns.port = 9092
	swNoDns.configBackoffDuration = time.Millisecond
	swNoDns.configPauseDuration = time.Millisecond

	commands = mockTelnetMulti(t, swNoDns.port, 6)
	assert.Nil(t, swNoDns.ConfigureTeamEthernet([6]*model.Team{{Id: 841}, nil, nil, nil, nil, nil}))
	assert.NotContains(t, commands.at(4), "dns-server")
	assert.Contains(t, commands.at(4), "default-router 10.8.41.4\nlease 7\n")
}
