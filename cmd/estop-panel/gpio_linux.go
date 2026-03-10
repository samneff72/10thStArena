//go:build linux

package main

import (
	"fmt"
	"log"

	"github.com/team841/bioarena/hardware"
	"github.com/warthog618/go-gpiocdev"
)

type linuxGpioReader struct {
	entries []linuxPinEntry
}

type linuxPinEntry struct {
	line  *gpiocdev.Line
	event hardware.EStopEvent
}

// openGPIO opens GPIO lines for each non-zero pin in the config.
// All lines use active-low, internal pull-up convention.
func openGPIO(chip string, pins PinConfig, alliance string) (gpioReader, error) {
	stations := stationNames(alliance)
	type pinDef struct {
		pin     int
		station string
		isAStop bool
	}
	defs := []pinDef{
		{pins.Station1EStop, stations[0], false},
		{pins.Station1AStop, stations[0], true},
		{pins.Station2EStop, stations[1], false},
		{pins.Station2AStop, stations[1], true},
		{pins.Station3EStop, stations[2], false},
		{pins.Station3AStop, stations[2], true},
		{pins.FieldEStop, "all", false},
	}
	var entries []linuxPinEntry
	for _, d := range defs {
		if d.pin == 0 {
			continue
		}
		line, err := gpiocdev.RequestLine(chip, d.pin, gpiocdev.AsInput, gpiocdev.WithPullUp)
		if err != nil {
			for _, e := range entries {
				_ = e.line.Close()
			}
			return nil, fmt.Errorf("open GPIO chip %q pin %d: %w", chip, d.pin, err)
		}
		log.Printf("estop-panel: opened %s pin %d (station=%s aStop=%v)", chip, d.pin, d.station, d.isAStop)
		entries = append(entries, linuxPinEntry{
			line:  line,
			event: hardware.EStopEvent{Station: d.station, IsAStop: d.isAStop},
		})
	}
	return &linuxGpioReader{entries: entries}, nil
}

func (r *linuxGpioReader) Read() []hardware.EStopEvent {
	var events []hardware.EStopEvent
	for _, e := range r.entries {
		val, err := e.line.Value()
		if err != nil {
			log.Printf("estop-panel: GPIO read error: %v", err)
			continue
		}
		if val == 0 { // active-low: LOW = button pressed
			events = append(events, e.event)
		}
	}
	return events
}

func (r *linuxGpioReader) Close() {
	for _, e := range r.entries {
		_ = e.line.Close()
	}
}
