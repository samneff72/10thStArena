// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)

package model

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestEventSettingsReadWrite(t *testing.T) {
	db := setupTestDb(t)
	defer db.Close()

	eventSettings, err := db.GetEventSettings()
	assert.Nil(t, err)
	assert.Equal(
		t,
		EventSettings{
			Id:                          1,
			Name:                        "Untitled Event",
			PlayoffType:                 DoubleEliminationPlayoff,
			NumPlayoffAlliances:         8,
			SelectionRound2Order:        "L",
			SelectionRound3Order:        "",
			SelectionShowUnpickedTeams:  true,
			TbaDownloadEnabled:          false,
			ApChannel:                   36,
			WarmupDurationSec:           0,
			AutoDurationSec:             20,
			PauseDurationSec:            3,
			TeleopDurationSec:           140,
			WarningRemainingDurationSec: 30,
			CompanionAddress:            "",
			CompanionPort:               0,

			// Hub LEDs default to the standard arena: upstream's full-field layout,
			// every sequence unmodified. The addresses below are only consulted when
			// HubLedsSimplified is turned on.
			HubLedsSimplified:   false,
			HubLedsFallback:     "full",
			HubLedsRedUniverse:  1,
			HubLedsRedAddress:   1,
			HubLedsBlueUniverse: 1,
			HubLedsBlueAddress:  25,
		},
		*eventSettings,
	)

	eventSettings.Name = "Chezy Champs"
	eventSettings.NumPlayoffAlliances = 6
	eventSettings.SelectionRound2Order = "F"
	eventSettings.SelectionRound3Order = "L"
	err = db.UpdateEventSettings(eventSettings)
	assert.Nil(t, err)
	eventSettings2, err := db.GetEventSettings()
	assert.Nil(t, err)
	assert.Equal(t, eventSettings, eventSettings2)
}
