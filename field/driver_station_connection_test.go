// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package field

import (
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/team841/bioarena/model"
	"github.com/team841/bioarena/network"
	"github.com/stretchr/testify/assert"
)

func TestEncodeControlPacket(t *testing.T) {
	arena := setupTestArena(t)

	tcpConn := setupFakeTcpConnection(t)
	defer tcpConn.Close()
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, false, 0)
	assert.Nil(t, err)
	defer dsConn.close()

	data, _ := dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(0), data[5])
	assert.Equal(t, byte(0), data[6])
	assert.Equal(t, byte(0), data[20])
	assert.Equal(t, byte(20), data[21])

	// Check the different alliance station values.
	dsConn.AllianceStation = "R2"
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(1), data[5])
	dsConn.AllianceStation = "R3"
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(2), data[5])
	dsConn.AllianceStation = "B1"
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(3), data[5])
	dsConn.AllianceStation = "B2"
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(4), data[5])
	dsConn.AllianceStation = "B3"
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(5), data[5])

	// Check packet count rollover.
	dsConn.packetCount = 255
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(0), data[0])
	assert.Equal(t, byte(255), data[1])
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(1), data[0])
	assert.Equal(t, byte(0), data[1])
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(1), data[0])
	assert.Equal(t, byte(1), data[1])
	dsConn.packetCount = 65535
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(255), data[0])
	assert.Equal(t, byte(255), data[1])
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(0), data[0])
	assert.Equal(t, byte(0), data[1])

	// Check different robot statuses.
	dsConn.Auto = true
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(2), data[3])

	dsConn.Enabled = true
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(6), data[3])

	dsConn.Auto = false
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(4), data[3])

	dsConn.EStop = true
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(132), data[3])

	dsConn.AStop = true
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(196), data[3])

	// Check different match types.
	arena.CurrentMatch.Type = model.Practice
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(1), data[6])
	arena.CurrentMatch.Type = model.Qualification
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(2), data[6])
	arena.CurrentMatch.Type = model.Playoff
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(3), data[6])

	// Check match numbers.
	arena.CurrentMatch.Type = model.Practice
	arena.CurrentMatch.TypeOrder = 42
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(0), data[7])
	assert.Equal(t, byte(42), data[8])
	arena.CurrentMatch.Type = model.Qualification
	arena.CurrentMatch.TypeOrder = 258
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(1), data[7])
	assert.Equal(t, byte(2), data[8])
	arena.CurrentMatch.Type = model.Playoff
	arena.CurrentMatch.TypeOrder = 13
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(0), data[7])
	assert.Equal(t, byte(13), data[8])

	// Check the countdown at different points during the match.
	arena.MatchState = AutoPeriod
	arena.MatchStartTime = time.Now().Add(-time.Duration(4 * time.Second))
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(16), data[21])
	arena.MatchState = PausePeriod
	arena.MatchStartTime = time.Now().Add(-time.Duration(16 * time.Second))
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(140), data[21])
	arena.MatchState = TeleopPeriod
	arena.MatchStartTime = time.Now().Add(-time.Duration(33 * time.Second))
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(129), data[21])
	arena.MatchStartTime = time.Now().Add(-time.Duration(150 * time.Second))
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(12), data[21])
	arena.MatchState = PostMatch
	arena.MatchStartTime = time.Now().Add(-time.Duration(180 * time.Second))
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte(0), data[21])
}

func TestSendControlPacket(t *testing.T) {
	arena := setupTestArena(t)

	tcpConn := setupFakeTcpConnection(t)
	defer tcpConn.Close()
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, false, 0)
	assert.Nil(t, err)
	defer dsConn.close()

	// No real way of checking this since the destination IP is remote, so settle for there being no errors.
	err = dsConn.sendControlPacket(arena)
	assert.Nil(t, err)
}

func TestEncodeControlPacketGameDataNewDs(t *testing.T) {
	arena := setupTestArena(t)

	tcpConn := setupFakeTcpConnection(t)
	defer tcpConn.Close()
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, true, 0)
	assert.Nil(t, err)
	defer dsConn.close()

	// With no game data the packet is the base length and carries no trailing field.
	arena.GameData = ""
	data, length := dsConn.encodeControlPacket(arena)
	assert.Equal(t, baseControlPacketBytes, length)
	assert.Equal(t, byte(0), data[22])

	// With game data it is appended as a tagged field.
	arena.GameData = "R"
	data, length = dsConn.encodeControlPacket(arena)
	assert.Equal(t, baseControlPacketBytes+3, length)
	assert.Equal(t, byte(2), data[22]) // Length of the field plus one.
	assert.Equal(t, byte(gameDataTag), data[23])
	assert.Equal(t, byte('R'), data[24])

	arena.GameData = "B"
	data, _ = dsConn.encodeControlPacket(arena)
	assert.Equal(t, byte('B'), data[24])

	// Over-long game data is truncated rather than overflowing the packet.
	arena.GameData = "ABCDEFGHIJKL"
	_, length = dsConn.encodeControlPacket(arena)
	assert.Equal(t, baseControlPacketBytes+2+maxGameDataBytes, length)
	assert.LessOrEqual(t, length, maxControlPacketBytes)
}

func TestEncodeControlPacketGameDataOmittedForLegacyDs(t *testing.T) {
	arena := setupTestArena(t)

	tcpConn := setupFakeTcpConnection(t)
	defer tcpConn.Close()
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, false, 0)
	assert.Nil(t, err)
	defer dsConn.close()

	// Legacy driver stations get game data over TCP, never in the control packet.
	arena.GameData = "R"
	data, length := dsConn.encodeControlPacket(arena)
	assert.Equal(t, baseControlPacketBytes, length)
	assert.Equal(t, byte(0), data[22])
}

func TestCheckGameDataSendsOnceOnChange(t *testing.T) {
	tcpConn, receivedAtLeast := setupLiveTcpConnection(t)
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, false, 0)
	assert.Nil(t, err)

	// Empty game data matches the initial state, so nothing is sent.
	assert.Nil(t, dsConn.checkGameData(""))
	assert.Equal(t, "", dsConn.SentGameData)
	assert.Len(t, receivedAtLeast(0), 0)

	// A new value goes out as a TCP packet of type 28 and is recorded as sent.
	assert.Nil(t, dsConn.checkGameData("R"))
	assert.Equal(t, "R", dsConn.SentGameData)
	assert.Equal(t, []byte{0, 3, 28, 1, 'R'}, receivedAtLeast(5))

	// Repeats of the same value are suppressed — nothing further on the wire.
	assert.Nil(t, dsConn.checkGameData("R"))
	assert.Nil(t, dsConn.checkGameData("R"))
	assert.Len(t, receivedAtLeast(5), 5)

	// A changed value is sent again.
	assert.Nil(t, dsConn.checkGameData("B"))
	assert.Equal(t, "B", dsConn.SentGameData)
	assert.Equal(t, []byte{0, 3, 28, 1, 'R', 0, 3, 28, 1, 'B'}, receivedAtLeast(10))
}

func TestCheckGameDataSkippedForNewDs(t *testing.T) {
	tcpConn, receivedAtLeast := setupLiveTcpConnection(t)
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, true, 0)
	assert.Nil(t, err)

	// Newer driver stations read game data from the control packet, so the TCP path is
	// never taken and SentGameData stays empty.
	assert.Nil(t, dsConn.checkGameData("B"))
	assert.Equal(t, "", dsConn.SentGameData)
	assert.Len(t, receivedAtLeast(0), 0)
}

func TestListenForDriverStations(t *testing.T) {
	arena := setupTestArena(t)

	oldAddress := network.ServerIpAddress
	network.ServerIpAddress = "127.0.0.1"
	go arena.listenForDriverStations()
	time.Sleep(time.Millisecond * 10)
	network.ServerIpAddress = oldAddress // Put it back to avoid affecting other tests.

	// Connect with an invalid initial packet.
	tcpConn, err := net.Dial("tcp", "127.0.0.1:1750")
	if assert.Nil(t, err) {
		dataSend := [5]byte{0, 3, 29, 0, 0}
		tcpConn.Write(dataSend[:])
		var dataReceived [100]byte
		_, err = tcpConn.Read(dataReceived[:])
		assert.NotNil(t, err)
		tcpConn.Close()
	}

	// Connect as a team not in the current match. It is turned away, as upstream does --
	// bioarena no longer registers an unexpected team by guessing its station.
	tcpConn, err = net.Dial("tcp", "127.0.0.1:1750")
	if assert.Nil(t, err) {
		dataSend := [5]byte{0, 3, 24, 5, 223}
		tcpConn.Write(dataSend[:])
		var dataReceived [5]byte
		_, err = tcpConn.Read(dataReceived[:])
		assert.NotNil(t, err)
		tcpConn.Close()
	}

	// Connect as a team in the current match.
	arena.assignTeam(1503, "B2")

	// Connect as a team in the current match with a fragmented initial packet.
	tcpConn, err = net.Dial("tcp", "127.0.0.1:1750")
	if assert.Nil(t, err) {
		dataSend := [5]byte{0, 3, 24, 5, 223}
		tcpConn.Write(dataSend[:1])
		tcpConn.Write(dataSend[1:5])
		var dataReceived [5]byte
		_, err := tcpConn.Read(dataReceived[:])
		assert.Nil(t, err)
		tcpConn.Close()
	}

	tcpConn, err = net.Dial("tcp", "127.0.0.1:1750")
	if assert.Nil(t, err) {
		defer tcpConn.Close()
		dataSend := [5]byte{0, 3, 24, 5, 223}
		tcpConn.Write(dataSend[:])
		var dataReceived [5]byte
		_, err = tcpConn.Read(dataReceived[:])
		assert.Nil(t, err)
		assert.Equal(t, [5]byte{0, 3, 25, 4, 0}, dataReceived)

		time.Sleep(time.Millisecond * 10)
		dsConn := arena.AllianceStations["B2"].DsConn
		if assert.NotNil(t, dsConn) {
			assert.Equal(t, 1503, dsConn.TeamId)
			assert.Equal(t, "B2", dsConn.AllianceStation)

			// Check that an unknown packet type gets ignored and a status packet gets decoded.
			dataSend = [5]byte{0, 3, 37, 0, 0}
			tcpConn.Write(dataSend[:])
			time.Sleep(time.Millisecond * 10)
		}
	}
}

func TestNewDriverStationConnection_UdpPortSelection(t *testing.T) {
	tcpConn := setupFakeTcpConnection(t)
	defer tcpConn.Close()

	// Test with default settings (FMS port).
	dsConn, err := newDriverStationConnection(254, "R1", tcpConn, false, false, 0)
	assert.Nil(t, err)
	defer dsConn.close()
	assert.Contains(t, dsConn.udpConn.RemoteAddr().String(), fmt.Sprintf(":%d", driverStationUdpSendPort))

	tcpConnLite := setupFakeTcpConnection(t)
	defer tcpConnLite.Close()

	// Test with FMS Lite port enabled.
	dsConnLite, err := newDriverStationConnection(254, "R1", tcpConnLite, true, false, 0)
	assert.Nil(t, err)
	defer dsConnLite.close()
	assert.Contains(t, dsConnLite.udpConn.RemoteAddr().String(), fmt.Sprintf(":%d", driverStationUdpSendPortLite))

	tcpConnNew := setupFakeTcpConnection(t)
	defer tcpConnNew.Close()

	// A port nominated by a newer driver station wins over the Lite setting.
	dsConnNew, err := newDriverStationConnection(254, "R1", tcpConnNew, true, true, 1234)
	assert.Nil(t, err)
	defer dsConnNew.close()
	assert.Contains(t, dsConnNew.udpConn.RemoteAddr().String(), ":1234")
	assert.True(t, dsConnNew.newDs)
}

func TestParseInitialPacketLegacyDs(t *testing.T) {
	// Tag 24, team number 254 big-endian across bytes 3-4.
	packet := []byte{0, 3, 24, 0, 254}
	teamId, newDs, udpSendPort, err := parseInitialPacket(packet)
	assert.Nil(t, err)
	assert.Equal(t, 254, teamId)
	assert.False(t, newDs)
	assert.Equal(t, 0, udpSendPort)

	// Team numbers above 255 span both bytes.
	packet = []byte{0, 3, 24, 3, 73} // 3<<8 + 73 = 841
	teamId, _, _, err = parseInitialPacket(packet)
	assert.Nil(t, err)
	assert.Equal(t, 841, teamId)
}

func TestParseInitialPacketNewDs(t *testing.T) {
	// Tag 30, UDP port 1121 in bytes 3-4, flags byte, then ASCII "841".
	packet := []byte{0, 8, 30, 0x04, 0x61, 0, 3, '8', '4', '1'}
	teamId, newDs, udpSendPort, err := parseInitialPacket(packet)
	assert.Nil(t, err)
	assert.Equal(t, 841, teamId)
	assert.True(t, newDs)
	assert.Equal(t, 1121, udpSendPort)

	// A four-digit team number, to confirm the length byte is honoured.
	packet = []byte{0, 9, 30, 0x04, 0x61, 0, 4, '9', '9', '9', '4'}
	teamId, _, _, err = parseInitialPacket(packet)
	assert.Nil(t, err)
	assert.Equal(t, 9994, teamId)
}

func TestParseInitialPacketRejectsMalformed(t *testing.T) {
	cases := []struct {
		name   string
		packet []byte
	}{
		{"unknown tag", []byte{0, 3, 99, 0, 254}},
		{"legacy too short", []byte{0, 3, 24, 0}},
		{"new ds too short", []byte{0, 5, 30, 0, 0, 0}},
		{"team number length overruns packet", []byte{0, 8, 30, 0x04, 0x61, 0, 9, '8', '4', '1'}},
		{"team number not numeric", []byte{0, 8, 30, 0x04, 0x61, 0, 3, 'a', 'b', 'c'}},
		{"empty", []byte{}},
	}

	for _, c := range cases {
		_, _, _, err := parseInitialPacket(c.packet)
		assert.NotNil(t, err, "expected rejection for %s", c.name)
	}
}

// setupLiveTcpConnection returns a client connection whose peer stays open and drains
// everything written to it, unlike setupFakeTcpConnection which closes its listener
// immediately and so cannot be written to. The returned function waits until at least
// want bytes have arrived (or times out) and returns everything received so far.
func setupLiveTcpConnection(t *testing.T) (net.Conn, func(want int) []byte) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.Nil(t, err)

	var mu sync.Mutex
	var received []byte
	accepted := make(chan struct{})

	go func() {
		conn, err := l.Accept()
		close(accepted)
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 256)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				mu.Lock()
				received = append(received, buf[:n]...)
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()

	tcpConn, err := net.Dial("tcp", l.Addr().String())
	assert.Nil(t, err)
	<-accepted

	t.Cleanup(func() {
		tcpConn.Close()
		l.Close()
	})

	return tcpConn, func(want int) []byte {
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			mu.Lock()
			enough := len(received) >= want
			mu.Unlock()
			if enough {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}

		mu.Lock()
		defer mu.Unlock()
		return append([]byte(nil), received...)
	}
}

func setupFakeTcpConnection(t *testing.T) net.Conn {
	// Set up a fake TCP endpoint and connection to it.
	l, err := net.Listen("tcp", ":9999")
	assert.Nil(t, err)
	defer l.Close()
	tcpConn, err := net.Dial("tcp", "127.0.0.1:9999")
	assert.Nil(t, err)
	return tcpConn
}
