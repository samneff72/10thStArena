// Copyright 2026 Team 841. All Rights Reserved.

package led

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// listen returns a UDP socket and a controller already pointed at it.
//
// The connection is set directly rather than through SetAddress, which always dials the
// Art-Net port; TestArtNetUsesTheArtNetPort covers that separately.
func listen(t *testing.T) (*net.UDPConn, *ArtNetController) {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	assert.Nil(t, err)
	t.Cleanup(func() { listener.Close() })

	controller := NewArtNetController()
	controller.conn, err = net.Dial("udp4", listener.LocalAddr().String())
	assert.Nil(t, err)
	t.Cleanup(func() { controller.conn.Close() })

	return listener, controller
}

// 6454, not the 5568 the sACN controller uses. A node listening on one never sees the
// other, and the failure is simply darkness.
func TestArtNetUsesTheArtNetPort(t *testing.T) {
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: artNetPort})
	if err != nil {
		t.Skip("something else is bound to the Art-Net port on this machine")
	}
	defer listener.Close()

	controller := NewArtNetController()
	assert.Nil(t, controller.SetAddress("127.0.0.1"))
	controller.packet = createBlankArtNetPacket()
	assert.Nil(t, controller.sendArtNetPacket(1, &universe{}))

	received := make([]byte, 600)
	listener.SetReadDeadline(time.Now().Add(time.Second))
	n, err := listener.Read(received)
	assert.Nil(t, err)
	assert.Equal(t, artNetHeaderBytes+universeChannelCount, n)
}

func TestArtNetPacketHeader(t *testing.T) {
	packet := createBlankArtNetPacket()

	assert.Equal(t, artNetHeaderBytes+universeChannelCount, len(packet))
	assert.Equal(t, "Art-Net\x00", string(packet[0:8]))

	// Opcode 0x5000, little-endian -- the one field in the header that is.
	assert.Equal(t, byte(0x00), packet[8])
	assert.Equal(t, byte(0x50), packet[9])

	// Protocol version 14, big-endian.
	assert.Equal(t, byte(0), packet[10])
	assert.Equal(t, byte(14), packet[11])

	// A full universe, big-endian: 512 is 0x0200.
	assert.Equal(t, byte(0x02), packet[16])
	assert.Equal(t, byte(0x00), packet[17])
}

// The configured number goes on the wire unchanged, so a gateway is set to universe 1
// whichever protocol it speaks. Anything else is silent when wrong: the node receives
// packets addressed to a universe it is not listening on and shows nothing.
func TestArtNetUniverseNumbering(t *testing.T) {
	conn, controller := listen(t)
	controller.packet = createBlankArtNetPacket()

	for _, testCase := range []struct {
		configured int
		low, high  byte
	}{
		{1, 1, 0},
		{2, 2, 0},
		{255, 255, 0},
		{256, 0, 1},
	} {
		assert.Nil(t, controller.sendArtNetPacket(testCase.configured, &universe{}))
		received := make([]byte, 600)
		conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := conn.Read(received)
		assert.Nil(t, err)
		assert.Equal(t, artNetHeaderBytes+universeChannelCount, n)
		assert.Equal(t, testCase.low, received[14], "universe %d low byte", testCase.configured)
		assert.Equal(t, testCase.high, received[15], "universe %d high byte", testCase.configured)
	}
}

// Universes start at 1, and sending 0 as 65535 would be worse than refusing.
func TestArtNetRejectsUniverseZero(t *testing.T) {
	controller := NewArtNetController()
	controller.packet = createBlankArtNetPacket()
	assert.NotNil(t, controller.sendArtNetPacket(0, &universe{}))
}

// Zero means "sequencing disabled" in Art-Net, so a wrapping counter must skip it rather
// than telling the node to stop checking order.
func TestArtNetSequenceSkipsZero(t *testing.T) {
	conn, controller := listen(t)
	controller.packet = createBlankArtNetPacket()

	sent := &universe{sequence: 255}
	assert.Nil(t, controller.sendArtNetPacket(1, sent))
	assert.Equal(t, byte(1), sent.sequence)

	received := make([]byte, 600)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	_, err := conn.Read(received)
	assert.Nil(t, err)
	assert.Equal(t, byte(1), received[12])
}

// The pixels are the sACN controller's, unchanged: only the packet around them differs.
func TestArtNetCarriesTheSamePixels(t *testing.T) {
	conn, controller := listen(t)
	assert.Nil(t, controller.SetLayout(
		[]FixtureSpec{{Universe: 1, StartAddress: 1}},
		[]FixtureSpec{{Universe: 1, StartAddress: 25}},
	))
	controller.SetMode(RedMode, BlueMode)
	assert.Nil(t, controller.Update())

	received := make([]byte, 600)
	conn.SetReadDeadline(time.Now().Add(time.Second))
	n, err := conn.Read(received)
	assert.Nil(t, err)
	assert.Equal(t, artNetHeaderBytes+universeChannelCount, n)

	// Something was lit: an all-zero universe would mean the rendering never ran.
	data := received[artNetHeaderBytes:n]
	lit := false
	for _, channel := range data {
		if channel != 0 {
			lit = true
			break
		}
	}
	assert.True(t, lit, "expected pixel data in the packet")
}

// Blank address disables output rather than erroring, matching the sACN controller.
func TestArtNetBlankAddressDisablesOutput(t *testing.T) {
	controller := NewArtNetController()
	assert.Nil(t, controller.SetAddress(""))
	assert.Nil(t, controller.Update())
}
