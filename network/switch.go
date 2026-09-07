// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Methods for configuring a Cisco Catalyst 3560-CX switch for team VLANs.

package network

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"log"

	"github.com/team841/bioarena/model"
	"net"
	"sync"
	"time"
)

const (
	switchConfigBackoffDurationSec = 5
	switchConfigPauseDurationSec   = 2
	switchTeamGatewayAddress       = 4
	switchTelnetPort               = 23
)

const (
	red1Vlan  = 10
	red2Vlan  = 20
	red3Vlan  = 30
	blue1Vlan = 40
	blue2Vlan = 50
	blue3Vlan = 60
)

// switchTrunkInterfaces carry every VLAN: the Pi on the first, the access point on the
// second. Both need all six team VLANs -- the access point tags each team's SSID onto that
// team's VLAN, so an access port there leaves every robot associated and unable to reach
// anything.
var switchTrunkInterfaces = [2]string{"GigabitEthernet0/7", "GigabitEthernet0/8"}

// dmxHubPort is the access port for the field's DMX/Art-Net gateway. It carries VLAN 1, the
// same subnet as the FMS Pi's 10.0.100.5, so the gateway can reach the FMS directly with no
// routing or DHCP pool of its own -- it is given a static address in that subnet from
// Arena -> Settings -> LEDs, the same way the Pi's address is static rather than leased.
//
// Nothing on the field needs to reach the gateway; bioarena only ever sends to it. So unlike
// a driver station port it needs no VLAN of its own and no DHCP pool -- it is set once by
// the baseline and left alone.
const dmxHubPort = "GigabitEthernet0/9"

// vlanNames label the VLAN database, so "show vlan brief" on the switch reads as the field
// rather than as VLAN0010 through VLAN0060.
var vlanNames = [6]string{"Red1", "Red2", "Red3", "Blue1", "Blue2", "Blue3"}

// The Vivid-Hosting access point lives at 10.0.100.2, on the management subnet, which is
// where bioarena talks to it and what the AP Address setting holds. It also answers on
// 192.168.69.1 as a backup, and that is where it turns up after a reset or a failed
// firmware write.
//
// The field VLAN carries the backup subnet as a secondary so that fallback stays reachable
// from the field. Without it an AP that has dropped back is unreachable by everything here
// -- the switch has no interface in that subnet, so there is nothing to route through and
// nothing to ARP for -- and recovering it means cabling a laptop straight to the AP with a
// hand-set static address.
//
// The Pi holds an address there too, set by bioarena.service. This secondary lets anything
// else on the field reach it as well, and gives the AP a gateway if it ever needs one.
const (
	apSubnetMask          = "255.255.255.0"
	switchApSubnetAddress = "192.168.69.2"
)

// dsPortInterfaces is the driver station port for each alliance station, in station order.
// A Catalyst 3560-CX with the stations on its first six ports is the assumed field, so
// wire R1 to Gi0/1 and so on; the trunks to the Pi and the access point go on the ports
// above these.
//
// Used only to build the baseline's access ports. Bioarena no longer shuts and reopens
// them around a VLAN change: the cycling cost seconds on every match load, and a driver
// station that needs a new address gets one by reconnecting.
var dsPortInterfaces = [6]string{
	"GigabitEthernet0/1",
	"GigabitEthernet0/2",
	"GigabitEthernet0/3",
	"GigabitEthernet0/4",
	"GigabitEthernet0/5",
	"GigabitEthernet0/6",
}

type Switch struct {
	address               string
	port                  int
	password              string
	dnsServer             string
	mutex                 sync.Mutex
	configBackoffDuration time.Duration
	configPauseDuration   time.Duration
	Status                string

	// applied is the team per station as the switch is currently configured, by team
	// number, with 0 for an empty station. Only meaningful once synced is true.
	applied [6]int

	// synced records whether the switch's configuration is known. False at startup, since
	// the switch outlives the process and may have been changed by hand, and after any
	// failure, which leaves it half configured. Either makes the next call a full
	// reconciliation rather than a difference.
	synced bool
}

var ServerIpAddress = "10.0.100.5" // The DS will try to connect to this address only.

// SwitchConfig collects the switch settings. A struct rather than positional arguments
// because they are all strings, and a transposed pair would misconfigure a field without
// failing.
type SwitchConfig struct {
	Address   string
	Password  string
	DnsServer string
}

func NewSwitch(config SwitchConfig) *Switch {
	return &Switch{
		address:               config.Address,
		port:                  switchTelnetPort,
		password:              config.Password,
		dnsServer:             config.DnsServer,
		configBackoffDuration: switchConfigBackoffDurationSec * time.Second,
		configPauseDuration:   switchConfigPauseDurationSec * time.Second,
		Status:                "UNKNOWN",
	}
}

func (sw *Switch) GetStatus() string {
	return sw.Status
}

// setStatus records a status change, logging transitions the way the access point does.
// The badge alone cannot distinguish a configuration that failed from one that never ran,
// and a settings save rebuilds this object back to UNKNOWN.
func (sw *Switch) setStatus(status string) {
	if sw.Status != status {
		log.Printf("Switch status changed from %s to %s.", sw.Status, status)
	}
	sw.Status = status
}

// Sets up wired networks for the given set of teams.
func (sw *Switch) ConfigureTeamEthernet(teams [6]*model.Team) error {
	// Make sure multiple configurations aren't being set at the same time.
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	// With no address there is nothing to configure. Without this the Telnet dial fails
	// on every match load and pins the badge red, which reads as a broken switch rather
	// than an absent one.
	if sw.address == "" {
		sw.setStatus("DISABLED")
		return nil
	}

	desired := teamIds(teams)

	// A full reconciliation when the switch's state is unknown: it outlives the process
	// and may have been changed by hand in between.
	full := !sw.synced
	if !full && desired == sw.applied {
		sw.setStatus("ACTIVE")
		return nil
	}

	sw.setStatus("CONFIGURING")

	// The standing configuration goes on once per run, alongside the full reconciliation
	// that happens for the same reason: the switch outlives this process, and what it is
	// holding cannot be assumed.
	if full {
		if err := sw.applyBaseline(); err != nil {
			return sw.fail(err)
		}
	}

	rebuild := [6]bool{}
	for i := range rebuild {
		rebuild[i] = full || desired[i] != sw.applied[i]
	}

	// Remove the old team VLANs to reset the switch state.
	removeTeamVlansCommand := ""
	for i, vlan := range vlanForStation {
		if !rebuild[i] {
			continue
		}
		removeTeamVlansCommand += fmt.Sprintf(
			"interface Vlan%d\nno ip address\nno ip dhcp pool dhcp%d\n", vlan, vlan,
		)
	}
	_, err := sw.runConfigCommand(removeTeamVlansCommand)
	if err != nil {
		return sw.fail(err)
	}
	time.Sleep(sw.configPauseDuration)

	// Create the new team VLANs.
	addTeamVlansCommand := ""
	addTeamVlan := func(team *model.Team, vlan int) {
		if team == nil {
			return
		}
		teamPartialIp := fmt.Sprintf("%d.%d", team.Id/100, team.Id%100)

		// Omitted entirely when unconfigured. Handing out a resolver the team subnet
		// cannot reach makes every lookup wait for a timeout instead of failing fast,
		// which is worse than having no DNS at all.
		dnsServerCommand := ""
		if sw.dnsServer != "" {
			dnsServerCommand = fmt.Sprintf("dns-server %s\n", sw.dnsServer)
		}

		addTeamVlansCommand += fmt.Sprintf(
			"ip dhcp excluded-address 10.%s.1 10.%s.19\n"+
				"ip dhcp excluded-address 10.%s.200 10.%s.254\n"+
				"ip dhcp pool dhcp%d\n"+
				"network 10.%s.0 255.255.255.0\n"+
				"default-router 10.%s.%d\n"+
				"%s"+
				"lease 7\n"+
				"interface Vlan%d\nip address 10.%s.%d 255.255.255.0\n",
			teamPartialIp,
			teamPartialIp,
			teamPartialIp,
			teamPartialIp,
			vlan,
			teamPartialIp,
			teamPartialIp,
			switchTeamGatewayAddress,
			dnsServerCommand,
			vlan,
			teamPartialIp,
			switchTeamGatewayAddress,
		)
	}
	// An empty station simply gets no subnet, as upstream leaves it. Giving one an address
	// so that a laptop plugged into it could announce its team was the staging-subnet
	// scheme behind auto-registration, which this fork no longer does.
	for i, vlan := range vlanForStation {
		if !rebuild[i] {
			continue
		}
		addTeamVlan(teams[i], vlan)
	}
	if len(addTeamVlansCommand) > 0 {
		_, err = sw.runConfigCommand(addTeamVlansCommand)
		if err != nil {
			return sw.fail(err)
		}
	}

	// Give some time for the configuration to take before another one can be attempted.
	time.Sleep(sw.configBackoffDuration)

	log.Printf("Switch configured: %s.", describeStations(desired, rebuild))
	sw.applied = desired
	sw.synced = true
	sw.setStatus("ACTIVE")
	return nil
}

// GetStationPortLinks reports whether each alliance station's driver station port has
// link, in station order.
//
// This is the only way to catch a laptop plugged into the wrong station. Bioarena's other
// checks all run downstream of a working connection: the wrong-station check needs the
// driver station to reach the FMS, which needs an address, which the station cannot get
// when its VLAN was never built. Link state is visible regardless.
func (sw *Switch) GetStationPortLinks() ([6]bool, error) {
	var links [6]bool
	if sw.address == "" {
		return links, errSwitchNotConfigured
	}

	output, err := sw.runCommand("show interfaces status\n")
	if err != nil {
		return links, err
	}
	return parsePortLinks(output), nil
}

var errSwitchNotConfigured = fmt.Errorf("switch address not configured")

// parsePortLinks reads "show interfaces status" output.
//
//	Port      Name    Status       Vlan   Duplex  Speed Type
//	Gi0/1             connected    10     a-full a-1000 10/100/1000BaseTX
//	Gi0/2             notconnect   20       auto   auto 10/100/1000BaseTX
//
// Ports are matched on their numeric suffix because IOS abbreviates the name here --
// "Gi0/1" for the "GigabitEthernet0/1" it accepts in configuration. The status is found by
// looking for the token rather than by column, since a port with a description set shifts
// every column to its right.
func parsePortLinks(output string) [6]bool {
	stationForPort := make(map[string]int, len(dsPortInterfaces))
	for i, name := range dsPortInterfaces {
		stationForPort[portNumericSuffix(name)] = i
	}

	var links [6]bool
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		station, ok := stationForPort[portNumericSuffix(fields[0])]
		if !ok {
			continue
		}
		for _, field := range fields[1:] {
			if field == "connected" {
				links[station] = true
				break
			}
		}
	}
	return links
}

// portNumericSuffix strips an interface name's alphabetic prefix: both "GigabitEthernet0/1"
// and "Gi0/1" reduce to "0/1".
func portNumericSuffix(name string) string {
	if i := strings.IndexAny(name, "0123456789"); i >= 0 {
		return name[i:]
	}
	return ""
}

// baselineCommands builds the switch's standing configuration: the VLAN database, the
// station and trunk ports, and routing between them. Everything a field needs that does
// not change from match to match.
//
// Bioarena applies this itself so that setting up a switch is wiring plus a bootstrap
// script, with nobody composing IOS by hand. It is idempotent -- every line states a
// desired end state rather than a change -- so re-applying it costs nothing.
func baselineCommands() string {
	command := "ip routing\n"

	// Point the switch at the field controller for time. Nothing on a practice field has a
	// battery-backed clock -- a Catalyst comes up believing it is 2004, and with no route to
	// the internet it never corrects itself. The Pi serves time via chrony (deploy-fms.sh
	// installs it), so this is the other half of that: without it the switch's log
	// timestamps disagree with bioarena's by years, and the moment that costs you is the one
	// where a match went wrong and you are trying to line the two up.
	command += fmt.Sprintf("ntp server %s\n", ServerIpAddress)

	// Secondary, so it sits alongside the management address the bootstrap script set as
	// the primary. A secondary with no primary is rejected, which is why this is not the
	// place the field's own address gets configured.
	command += fmt.Sprintf(
		"interface Vlan1\nip address %s %s secondary\nexit\n", switchApSubnetAddress, apSubnetMask,
	)

	for i, vlan := range vlanForStation {
		command += fmt.Sprintf("vlan %d\nname %s\nexit\n", vlan, vlanNames[i])
	}

	// Access ports, one per station. Portfast matters more here than it looks: bioarena
	// shuts and reopens these ports on every reconfiguration, and without it each one
	// walks through spanning-tree convergence on the way back up, so the laptop's DHCP
	// request goes out into a port that is not forwarding yet.
	for i, port := range dsPortInterfaces {
		command += fmt.Sprintf(
			"interface %s\nswitchport mode access\nswitchport access vlan %d\n"+
				"spanning-tree portfast\nno shutdown\nexit\n",
			port, vlanForStation[i],
		)
	}

	allowed := "1"
	for _, vlan := range vlanForStation {
		allowed += fmt.Sprintf(",%d", vlan)
	}
	for _, port := range switchTrunkInterfaces {
		// No "switchport trunk encapsulation dot1q": a 3560-CX speaks only 802.1Q and
		// rejects the command older platforms require.
		command += fmt.Sprintf(
			"interface %s\nswitchport mode trunk\nswitchport trunk allowed vlan %s\nno shutdown\nexit\n",
			port, allowed,
		)
	}

	command += fmt.Sprintf(
		"interface %s\nswitchport mode access\nswitchport access vlan 1\nno shutdown\nexit\n",
		dmxHubPort,
	)

	return command
}

// applyBaseline pushes the standing configuration and saves it, so a power cycle brings the
// switch back ready to run a field without bioarena having to be there.
//
// Saved here and only here. Writing the configuration later would bake in whichever teams
// happened to be on the field at the time, and those pools would return after every reboot
// as stale state -- a DHCP scope for a team that left, surfacing weeks later as an address
// nobody can account for.
func (sw *Switch) applyBaseline() error {
	if _, err := sw.runConfigCommand(baselineCommands()); err != nil {
		return fmt.Errorf("applying switch baseline: %w", err)
	}
	if _, err := sw.runCommand("write memory\n"); err != nil {
		return fmt.Errorf("saving switch baseline: %w", err)
	}
	log.Println("Switch baseline configuration applied and saved.")
	return nil
}

// CycleStationPorts shuts and reopens the selected stations' driver station ports.
//
// Driver station software releases its address at the end of a match, by its own logic, and
// Windows then sits there unaddressed until something changes. Replugging the cable works
// because the link event is what prompts a fresh DHCP request; so does this, without anyone
// walking to six laptops.
//
// Called at the end of a match and nowhere else. Bioarena used to cycle a station's port on
// every VLAN change and again from a background rescue poll, which cost seconds on every
// match load for a renewal that was usually not needed. Once, when the match is already
// over and nobody is driving, it costs nothing anyone is waiting on.
//
// Batched: one shut for every selected port and one reopen, so six stations cost the same
// two Telnet round trips and one pause as one station does.
func (sw *Switch) CycleStationPorts(stations [6]bool) error {
	sw.mutex.Lock()
	defer sw.mutex.Unlock()

	if sw.address == "" {
		return errSwitchNotConfigured
	}

	down := portCommands(stations, "shutdown")
	if down == "" {
		return nil
	}

	if _, err := sw.runConfigCommand(down); err != nil {
		return fmt.Errorf("shutting driver station ports: %w", err)
	}
	time.Sleep(sw.configPauseDuration)
	if _, err := sw.runConfigCommand(portCommands(stations, "no shutdown")); err != nil {
		// Ports left down are worse than ports never touched: every driver station behind
		// them is dead until someone notices. Reported loudly rather than swallowed.
		return fmt.Errorf("reopening driver station ports: %w", err)
	}
	return nil
}

// portCommands builds an interface block applying the given verb to each selected station's
// driver station port.
func portCommands(stations [6]bool, verb string) string {
	command := ""
	for i, selected := range stations {
		if selected {
			command += fmt.Sprintf("interface %s\n%s\n", dsPortInterfaces[i], verb)
		}
	}
	return command
}

// fail marks the configuration as failed. The switch is left half configured, so the
// recorded state is no longer trustworthy and the next call reconciles in full.
func (sw *Switch) fail(err error) error {
	sw.synced = false
	sw.setStatus("ERROR")
	return err
}

// Logs into the switch via Telnet and runs the given command in user exec mode. Reads the output and
// returns it as a string.
func (sw *Switch) runCommand(command string) (string, error) {
	// Open a Telnet connection to the switch.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s:%d", sw.address, sw.port), 10*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	// Set a deadline so the read doesn't block forever if the switch doesn't close the connection.
	if err = conn.SetDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return "", err
	}

	// Login to the AP, send the command, and log out all at once.
	writer := bufio.NewWriter(conn)
	_, err = writer.WriteString(
		fmt.Sprintf(
			"%s\nenable\n%s\nterminal length 0\n%sexit\n", sw.password, sw.password,
			command,
		),
	)
	if err != nil {
		return "", err
	}
	err = writer.Flush()
	if err != nil {
		return "", err
	}

	// Read the response. The switch may not close the connection after exit, so we read
	// until the deadline fires (indicated by a timeout error, which we treat as success).
	var reader bytes.Buffer
	_, err = reader.ReadFrom(conn)
	if err != nil {
		if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
			// Timeout just means the switch kept the connection open — the commands were sent.
			return reader.String(), nil
		}
		return "", err
	}
	return reader.String(), nil
}

// Logs into the switch via Telnet and runs the given command in global configuration mode. Reads the output
// and returns it as a string.
func (sw *Switch) runConfigCommand(command string) (string, error) {
	// Terminate the caller's last line. Without this "end" is appended to it -- the
	// stock driver-station port commands end in "no shutdown", which became
	// "no shutdownend" and was rejected by IOS. The failure is invisible: a Telnet read
	// timeout is treated as success, so the port cycling silently did nothing.
	//
	// Upstream cannot hit this: it has no driver-station port commands, and both of its
	// callers build strings ending in a newline. The guard is still worth sending back,
	// since the precondition is unstated and the failure mode is silent. Tracked in
	// docs/upstream-divergences.md.
	if command != "" && !strings.HasSuffix(command, "\n") {
		command += "\n"
	}
	return sw.runCommand(fmt.Sprintf("config terminal\n%send\n", command))
}
