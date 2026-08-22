// Copyright 2026 Team 841. All Rights Reserved.
//
// Art-Net output, as an alternative wire protocol to the E1.31 sACN that controller.go
// speaks. Same pixels, same layout, same modes -- only the packets on the wire differ.
//
// A separate file and a separate type on purpose. The six ported led/ files are kept
// byte-identical to upstream so their lighting changes can be taken with a checkout rather
// than a merge (see docs/upstream-divergences.md), and a protocol switch inside
// controller.go would end that. Being in the same package, this can still reuse the zone
// rendering and fixture layout rather than duplicating them; only the encoding and the
// send are new.

package led

import (
	"fmt"
	"net"
)

const (
	// Art-Net listens on 6454, where sACN listens on 5568. A node configured for the wrong
	// one simply never receives anything.
	artNetPort = 6454

	artNetHeaderBytes     = 18
	artNetProtocolVersion = 14
	artNetOpDmx           = 0x5000
)

// ArtNetController drives the same fixtures as Controller, over Art-Net.
//
// It embeds Controller for everything that is not protocol-specific -- layout, modes,
// pixels -- and replaces the two methods that touch the wire.
type ArtNetController struct {
	*Controller
	packet []byte
}

func NewArtNetController() *ArtNetController {
	return &ArtNetController{Controller: NewController()}
}

// SetAddress sets the node address, or disables output if the address is blank. Identical
// to the sACN version but for the port.
func (artNet *ArtNetController) SetAddress(address string) error {
	if artNet.conn != nil {
		_ = artNet.conn.Close()
		artNet.conn = nil
	}

	if address == "" {
		return nil
	}

	var err error
	artNet.conn, err = net.Dial("udp4", fmt.Sprintf("%s:%d", address, artNetPort))
	return err
}

// Update renders the current modes and sends each universe that has changed.
//
// The body mirrors Controller.Update because the rendering is the same; what differs is
// the packet it hands to the wire.
func (artNet *ArtNetController) Update() error {
	if artNet.conn == nil {
		// This controller is not configured; do nothing.
		return nil
	}

	artNet.redZone.updatePixels(Red)
	artNet.blueZone.updatePixels(Blue)

	if len(artNet.packet) == 0 {
		artNet.packet = createBlankArtNetPacket()
	}

	for _, universe := range artNet.universes {
		universe.currentData = [universeChannelCount]byte{}
	}

	if err := artNet.populateFixtureData(&artNet.redZone, artNet.fixtures.red); err != nil {
		return err
	}
	if err := artNet.populateFixtureData(&artNet.blueZone, artNet.fixtures.blue); err != nil {
		return err
	}

	for dmxUniverse, universe := range artNet.universes {
		if universe.shouldSendPacket() {
			if err := artNet.sendArtNetPacket(dmxUniverse, universe); err != nil {
				return err
			}
		}
	}

	return nil
}

// createBlankArtNetPacket builds the fixed part of an ArtDmx packet, reused for every send
// with only the sequence, universe and data updated.
func createBlankArtNetPacket() []byte {
	packet := make([]byte, artNetHeaderBytes+universeChannelCount)

	copy(packet, "Art-Net\x00")

	// Opcode, little-endian, unlike everything else in the header.
	packet[8] = byte(artNetOpDmx & 0xff)
	packet[9] = byte(artNetOpDmx >> 8)

	// Protocol version, big-endian.
	packet[10] = 0
	packet[11] = artNetProtocolVersion

	// Sequence and physical, filled per packet and unused respectively.
	packet[12] = 0
	packet[13] = 0

	// Universe, filled per packet.
	packet[14] = 0
	packet[15] = 0

	// Data length, big-endian. Always a full universe: a node that expects 512 channels
	// and receives fewer leaves the remainder at whatever it last held.
	packet[16] = byte(universeChannelCount >> 8)
	packet[17] = byte(universeChannelCount & 0xff)

	return packet
}

// sendArtNetPacket writes one universe.
//
// Art-Net numbers universes from 0 where sACN numbers them from 1, so the configured
// universe is shifted down by one: universe 1 in Settings is what a node displays as
// Art-Net universe 0. Getting this wrong is silent -- the node receives packets addressed
// to a universe it is not listening on and shows nothing.
func (artNet *ArtNetController) sendArtNetPacket(dmxUniverse int, universe *universe) error {
	artNetUniverse := dmxUniverse - 1
	if artNetUniverse < 0 {
		return fmt.Errorf("invalid universe %d: Art-Net universes start at 1 in settings", dmxUniverse)
	}

	universe.sequence++
	if universe.sequence == 0 {
		// Zero means "sequencing disabled" in Art-Net, so it is skipped on wrap.
		universe.sequence = 1
	}
	artNet.packet[12] = universe.sequence

	// Low byte is sub-universe and universe; high byte is net.
	artNet.packet[14] = byte(artNetUniverse & 0xff)
	artNet.packet[15] = byte(artNetUniverse >> 8 & 0x7f)

	copy(artNet.packet[artNetHeaderBytes:], universe.currentData[:])

	_, err := artNet.conn.Write(artNet.packet)
	universe.markSent()
	return err
}
