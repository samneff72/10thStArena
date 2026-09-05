// Copyright 2026 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Game-specific control of the 2026 Hub DMX lighting.
//
// Ported from upstream cheesy-arena's field/arena_leds.go. The mode selection is
// unchanged; the only adaptation is the source of the AUTO result. Upstream reads
// WonAuto off each alliance's realtime score, which bioarena does not have, so the
// Hubs are constructed from arena.AutoWinner instead.

package field

import (
	"log"
	"time"

	"github.com/team841/bioarena/game"
	"github.com/team841/bioarena/hardware"
	"github.com/team841/bioarena/led"
	"github.com/team841/bioarena/model"
)

const (
	hubLightWarningSec           = 3
	hubLightScoringAssessmentSec = 3
)

// ledController is the Hub LED surface the arena uses. Two implementations: led.Controller
// speaks E1.31 sACN, led.ArtNetController speaks Art-Net, and the checkbox under
// Arena > Settings > LEDs picks between them. Everything above the wire is identical.
type ledController interface {
	SetAddress(address string) error
	SetLayout(red, blue []led.FixtureSpec) error
	UseDefaultLayout()
	SetMode(redMode, blueMode led.Mode)
	GetModes() (led.Mode, led.Mode)
	GetPixels() ([64]led.Color, [64]led.Color)
	Update() error
}

// newLedController builds the controller for the configured protocol.
func newLedController(artNet bool) ledController {
	if artNet {
		return led.NewArtNetController()
	}
	return led.NewController()
}

// applyHubLedSettings pushes the stored Hub LED configuration into the controller.
// Called on every settings load, so changes take effect without restarting bioarena.
//
// Configuration errors are logged and leave the previous layout in place rather than
// returning: a bad fixture address should not stop the arena from loading.
func (arena *Arena) applyHubLedSettings(settings *model.EventSettings) {
	// A settings save is a fresh chance to report, so a failure that persists across one is
	// said again rather than staying suppressed from before the change.
	arena.hubLedsFailing = false

	// Rebuilt when the protocol changes: the two speak different ports, so switching means
	// a new connection rather than a new field on the old one.
	if settings.HubLedsArtNet != arena.hubLedsArtNet {
		// Release the outgoing controller's socket before the reference is dropped.
		// SetAddress("") is how: it closes the connection and is already on the interface.
		// The led package is kept byte-identical to upstream, so there is no Close to add
		// there -- see docs/upstream-divergences.md.
		if arena.Leds != nil {
			_ = arena.Leds.SetAddress("")
		}
		arena.hubLedsArtNet = settings.HubLedsArtNet
		arena.Leds = newLedController(settings.HubLedsArtNet)
		if settings.HubLedsArtNet {
			log.Println("Hub LEDs: sending Art-Net.")
		} else {
			log.Println("Hub LEDs: sending E1.31 sACN.")
		}
	}

	if err := arena.Leds.SetAddress(settings.HubLedsAddress); err != nil {
		log.Printf("Hub LEDs: could not set address %q: %v", settings.HubLedsAddress, err)
	}

	fallback, err := led.ParseFallback(settings.HubLedsFallback)
	if err != nil {
		log.Printf("Hub LEDs: %v; falling back to full", err)
	}
	arena.hubLedFallback = fallback

	if !settings.HubLedsSimplified {
		arena.Leds.UseDefaultLayout()
		return
	}

	err = arena.Leds.SetLayout(
		[]led.FixtureSpec{{Universe: settings.HubLedsRedUniverse, StartAddress: settings.HubLedsRedAddress}},
		[]led.FixtureSpec{{Universe: settings.HubLedsBlueUniverse, StartAddress: settings.HubLedsBlueAddress}},
	)
	if err != nil {
		log.Printf("Hub LEDs: simplified layout rejected (%v); keeping previous layout", err)
	}
}

// setHubLedModes applies the configured fallback and sends the modes to the controller.
// Every mode change goes through here so a practice field's fixtures never receive a
// sequence they cannot render.
func (arena *Arena) setHubLedModes(redMode, blueMode led.Mode) {
	arena.Leds.SetMode(
		led.ApplyFallback(arena.hubLedFallback, redMode, led.RedMode),
		led.ApplyFallback(arena.hubLedFallback, blueMode, led.BlueMode),
	)
}

// redWonAuto reports whether the red alliance is the AUTO winner.
func (arena *Arena) redWonAuto() bool {
	return arena.AutoWinner == hardware.AllianceRed
}

// hubs returns the red and blue Hubs for the current AUTO result.
func (arena *Arena) hubs() (*game.Hub, *game.Hub) {
	redWon := arena.redWonAuto()
	return &game.Hub{WonAuto: redWon}, &game.Hub{WonAuto: !redWon}
}

// updateHubLeds updates Hub LEDs based on the current match state and active scoring shift.
func (arena *Arena) updateHubLeds(currentTime time.Time) {
	switch arena.MatchState {
	case AutoPeriod:
		arena.setHubLedModes(led.RedStartupMode, led.BlueStartupMode)
	case PausePeriod:
		arena.setHubLedModes(led.RedMode, led.BlueMode)
	case TeleopPeriod:
		arena.updateTeleopHubLeds(currentTime)
	case PostMatch:
		if arena.lastMatchState != PostMatch {
			// Set the Hub LEDs to white at the end of the match, and then turn them off when the referees are supposed
			// to assess tower climbs.
			arena.setHubLedModes(led.WhiteMode, led.WhiteMode)
			go func() {
				time.Sleep(hubLightScoringAssessmentSec * time.Second)
				arena.setHubLedModes(led.OffMode, led.OffMode)
			}()
		}
	}

	// Logged on the transition into failure and on recovery, not on every attempt. This runs
	// on the arena loop at 100 Hz, so an unreachable receiver -- a gateway that is unplugged,
	// or an address left pointing at hardware that is not there -- otherwise writes a hundred
	// identical lines a second and buries everything else in the journal.
	if err := arena.Leds.Update(); err != nil {
		if !arena.hubLedsFailing {
			arena.hubLedsFailing = true
			log.Printf("Failed to update hub LEDs: %s", err)
			log.Println(
				"Hub LEDs: suppressing further failures until they recover. " +
					"Clear the LED Receiver Address under Setup > Settings to stop trying.",
			)
		}
	} else if arena.hubLedsFailing {
		arena.hubLedsFailing = false
		log.Println("Hub LEDs: updating again.")
	}
}

// updateTeleopHubLeds updates teleop LED modes using the active Hub shift, auto winner, and warning window.
func (arena *Arena) updateTeleopHubLeds(currentTime time.Time) {
	redHub, blueHub := arena.hubs()

	shift, remaining, _, ok := redHub.GetCurrentShiftTiming(arena.MatchStartTime, currentTime)
	if !ok {
		return
	}

	redRemaining, _ := redHub.GetActiveShiftTiming(arena.MatchStartTime, currentTime)
	blueRemaining, _ := blueHub.GetActiveShiftTiming(arena.MatchStartTime, currentTime)

	redMode := led.OffMode
	if redRemaining > 0 {
		redMode = led.RedMode
	}
	blueMode := led.OffMode
	if blueRemaining > 0 {
		blueMode = led.BlueMode
	}

	// Pulse the LEDs when the Hub is about to go inactive.
	if remaining <= time.Duration(hubLightWarningSec)*time.Second {
		switch shift {
		case game.ShiftTransition:
			if arena.redWonAuto() {
				redMode = led.RedPulseMode
			} else {
				blueMode = led.BluePulseMode
			}
		case game.Shift1, game.Shift3:
			if arena.redWonAuto() {
				blueMode = led.BluePulseMode
			} else {
				redMode = led.RedPulseMode
			}
		case game.Shift2:
			if arena.redWonAuto() {
				redMode = led.RedPulseMode
			} else {
				blueMode = led.BluePulseMode
			}
		case game.ShiftEndgame:
			redMode = led.RedPulseMode
			blueMode = led.BluePulseMode
		default:
		}
	} else if shift == game.ShiftTransition {
		if arena.redWonAuto() {
			redMode = led.RedAdvantageMode
		} else {
			blueMode = led.BlueAdvantageMode
		}
	}
	arena.setHubLedModes(redMode, blueMode)
}
