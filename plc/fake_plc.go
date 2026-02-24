// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// No-op PLC implementation for use when no physical PLC is present (e.g. practice/Pi builds).

package plc

import (
	"time"

	"github.com/Team254/cheesy-arena/websocket"
)

type FakePlc struct {
	ioChangeNotifier *websocket.Notifier
	cycleCounter     int
}

func (plc *FakePlc) SetAddress(address string) {
	if plc.ioChangeNotifier == nil {
		plc.ioChangeNotifier = websocket.NewNotifier("plcIoChange", plc.generateIoChangeMessage)
	}
}

// IsEnabled always returns false so the arena skips all PLC-gated checks.
func (plc *FakePlc) IsEnabled() bool {
	return false
}

func (plc *FakePlc) IsHealthy() bool {
	return false
}

func (plc *FakePlc) IoChangeNotifier() *websocket.Notifier {
	return plc.ioChangeNotifier
}

// Run loops at the same cadence as ModbusPlc so timing behaviour is consistent.
func (plc *FakePlc) Run() {
	for {
		startTime := time.Now()
		plc.cycleCounter++
		if plc.cycleCounter == cycleCounterMax {
			plc.cycleCounter = 0
		}
		time.Sleep(time.Until(startTime.Add(time.Millisecond * plcLoopPeriodMs)))
	}
}

func (plc *FakePlc) GetArmorBlockStatuses() map[string]bool {
	return map[string]bool{}
}

func (plc *FakePlc) GetFieldEStop() bool {
	return false
}

func (plc *FakePlc) GetTeamEStops() ([3]bool, [3]bool) {
	return [3]bool{}, [3]bool{}
}

func (plc *FakePlc) GetTeamAStops() ([3]bool, [3]bool) {
	return [3]bool{}, [3]bool{}
}

func (plc *FakePlc) GetEthernetConnected() ([3]bool, [3]bool) {
	return [3]bool{}, [3]bool{}
}

func (plc *FakePlc) ResetMatch() {}

func (plc *FakePlc) SetStackLights(red, blue, orange, green bool) {}

func (plc *FakePlc) SetStackBuzzer(state bool) {}

func (plc *FakePlc) SetFieldResetLight(state bool) {}

func (plc *FakePlc) GetCycleState(max, index, duration int) bool {
	return plc.cycleCounter/duration%max == index
}

func (plc *FakePlc) GetInputNames() []string {
	return []string{}
}

func (plc *FakePlc) GetRegisterNames() []string {
	return []string{}
}

func (plc *FakePlc) GetCoilNames() []string {
	return []string{}
}

func (plc *FakePlc) GetProcessorCounts() (int, int) {
	return 0, 0
}

func (plc *FakePlc) SetTrussLights(redLights, blueLights [3]bool) {}

func (plc *FakePlc) generateIoChangeMessage() any {
	return &struct {
		Inputs    []bool
		Registers []uint16
		Coils     []bool
	}{[]bool{}, []uint16{}, []bool{}}
}
