package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The whole point is to exclude the addresses that cannot be used to reach the controller.
// Every field is 10.0.100.0/24, so the management address identifies nothing; the team and
// staging subnets are routed by the switch and are no more reachable from a laptop.
func TestFieldAddressesAreExcluded(t *testing.T) {
	for _, address := range []string{
		"10.0.100.5",   // the controller's own field address
		"10.0.100.2",   // the access point
		"10.8.41.4",    // a team subnet gateway
		"172.16.40.1",  // a staging subnet
		"192.168.69.5", // the access point's backup subnet
	} {
		assert.True(t, isFieldAddress(net.ParseIP(address).To4()), "%s should be excluded", address)
	}

	for _, address := range []string{
		"192.168.1.193", // a home or bench network
		"192.168.4.224", // the bench network this field is deployed over
		"100.64.0.7",    // a VPN
	} {
		assert.False(t, isFieldAddress(net.ParseIP(address).To4()), "%s should be reportable", address)
	}
}

// An empty result is the normal state of a controller wired only into its field, and must
// read as such rather than as a blank in the header.
func TestFormatEmptyAddressesIsExplicit(t *testing.T) {
	assert.Equal(t, "field only", FormatOffFieldAddresses(nil))
	assert.Equal(
		t,
		"wlan0 192.168.1.5, wlan1 10.1.2.3",
		FormatOffFieldAddresses([]HostAddress{
			{Interface: "wlan0", Address: "192.168.1.5"},
			{Interface: "wlan1", Address: "10.1.2.3"},
		}),
	)
}
