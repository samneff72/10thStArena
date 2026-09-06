// Copyright 2026 Team 841. All Rights Reserved.
//
// Reporting the controller's own addresses outside the field network.

package network

import (
	"fmt"
	"net"
	"sort"
	"strings"
)

// HostAddress is one of this machine's IPv4 addresses that is not part of the field.
type HostAddress struct {
	Interface string
	Address   string
}

func (a HostAddress) String() string {
	return fmt.Sprintf("%s %s", a.Interface, a.Address)
}

// OffFieldAddresses lists this machine's IPv4 addresses that are not the field's own.
//
// The point is the address someone needs in order to reach this Pi: to ssh in, to deploy
// to it, or to open the web UI from a laptop. That address is never the field one. Every
// field uses 10.0.100.0/24 with the controller fixed at 10.0.100.5, so it identifies
// nothing and is not a route anyone's laptop has -- the useful address is whatever the Pi
// picked up on the bench or workshop network, which changes and which nobody can guess.
//
// Found by elimination rather than by interface name. "wlan0" is the usual answer but not a
// promise: a USB adapter, a renamed predictable interface, or a second wired network all
// count, and all are equally reachable.
func OffFieldAddresses() []HostAddress {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var found []HostAddress
	for _, iface := range interfaces {
		// Down interfaces hold stale addresses that cannot be reached.
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addresses {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipNet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			if isFieldAddress(ip) {
				continue
			}
			found = append(found, HostAddress{Interface: iface.Name, Address: ip.String()})
		}
	}

	// Stable order, so a header does not reshuffle itself between renders.
	sort.Slice(found, func(i, j int) bool {
		if found[i].Interface != found[j].Interface {
			return found[i].Interface < found[j].Interface
		}
		return found[i].Address < found[j].Address
	})
	return found
}

// isFieldAddress reports whether an address belongs to the field rather than to a network
// someone could reach this machine from.
//
// The management subnet and the access point's backup subnet are the controller's own, set
// by bioarena.service. Everything in 10/8 beyond that is a team subnet the switch routes,
// which is no more use for reaching this Pi than the management address is.
func isFieldAddress(ip net.IP) bool {
	switch {
	case ip[0] == 10:
		return true
	case ip[0] == 192 && ip[1] == 168 && ip[2] == 69:
		return true
	case ip[0] == 172 && ip[1] >= 16 && ip[1] <= 31:
		// Staging subnets, 172.16.<vlan>.0/24.
		return true
	default:
		return false
	}
}

// FormatOffFieldAddresses renders the addresses for a status line, or a plain note when
// there are none -- which is the normal state of a controller wired only into its field.
func FormatOffFieldAddresses(addresses []HostAddress) string {
	if len(addresses) == 0 {
		return "field only"
	}
	parts := make([]string, 0, len(addresses))
	for _, address := range addresses {
		parts = append(parts, address.String())
	}
	return strings.Join(parts, ", ")
}
