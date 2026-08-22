// Copyright 2014 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Model and datastore read/write methods for event-level configuration.

package model

import (
	"github.com/team841/bioarena/game"
)

type PlayoffType int

const (
	DoubleEliminationPlayoff PlayoffType = iota
	SingleEliminationPlayoff
)

type EventSettings struct {
	Id                         int `db:"id"`
	Name                       string
	PlayoffType                PlayoffType
	NumPlayoffAlliances        int
	SelectionRound2Order       string
	SelectionRound3Order       string
	SelectionShowUnpickedTeams bool
	TbaDownloadEnabled         bool
	TbaPublishingEnabled       bool
	TbaEventCode               string
	TbaSecretId                string
	TbaSecret                  string
	NexusEnabled               bool
	NetworkSecurityEnabled     bool
	ApAddress                  string
	ApPassword                 string
	ApChannel                  int
	SwitchAddress              string
	SwitchPassword             string
	// SwitchDnsServer is handed to team subnets in the per-match DHCP pools. Blank omits
	// the option entirely, which is correct for a field with no upstream resolver: a
	// DNS server clients cannot reach makes lookups time out rather than fail fast.
	SwitchDnsServer       string
	RedEStopPanelAddress  string
	BlueEStopPanelAddress string
	// FieldEStopPin is the BCM pin carrying the field e-stop's NO contact, and
	// FieldEStopNcPin the NC contact. Both set, the two channels are compared and
	// a disagreement is reported as a wiring fault; FieldEStopNcPin left at 0
	// falls back to single-channel monitoring with no fault detection.
	FieldEStopPin                    int
	FieldEStopNcPin                  int
	AdminPassword                    string
	TeamSignRed1Id                   int
	TeamSignRed2Id                   int
	TeamSignRed3Id                   int
	TeamSignRedTimerId               int
	TeamSignBlue1Id                  int
	TeamSignBlue2Id                  int
	TeamSignBlue3Id                  int
	TeamSignBlueTimerId              int
	AutoConfigureTeams               bool
	UseLiteUdpPort                   bool
	BlackmagicAddresses              string
	CompanionAddress                 string
	CompanionPort                    int
	CompanionMatchPreviewPage        int
	CompanionMatchPreviewRow         int
	CompanionMatchPreviewColumn      int
	CompanionSetAudiencePage         int
	CompanionSetAudienceRow          int
	CompanionSetAudienceColumn       int
	CompanionMatchStartPage          int
	CompanionMatchStartRow           int
	CompanionMatchStartColumn        int
	CompanionTeleopStartPage         int
	CompanionTeleopStartRow          int
	CompanionTeleopStartColumn       int
	CompanionEndgameStartPage        int
	CompanionEndgameStartRow         int
	CompanionEndgameStartColumn      int
	CompanionMatchEndPage            int
	CompanionMatchEndRow             int
	CompanionMatchEndColumn          int
	CompanionPostResultPage          int
	CompanionPostResultRow           int
	CompanionPostResultColumn        int
	CompanionAllianceSelectionPage   int
	CompanionAllianceSelectionRow    int
	CompanionAllianceSelectionColumn int
	CompanionMatchAbortPage          int
	CompanionMatchAbortRow           int
	CompanionMatchAbortColumn        int
	WarmupDurationSec                int
	AutoDurationSec                  int
	PauseDurationSec                 int
	TeleopDurationSec                int
	WarningRemainingDurationSec      int

	// Hub LED settings. Stored here rather than in config.yaml so they survive a power
	// cycle and can be changed from the Settings page without a restart.
	//
	// HubLedsSimplified selects the practice-field layout below instead of upstream's
	// full-field one (eight fixtures per alliance across four sides of each goal).
	// HubLedsFallback names how much the fixtures can render: "full", "solid", or
	// "binary".
	HubLedsAddress string
	// HubLedsArtNet sends Art-Net instead of E1.31 sACN. Same pixels and layout; a
	// different packet on a different port, for a node that speaks only Art-Net.
	HubLedsArtNet       bool
	HubLedsSimplified   bool
	HubLedsFallback     string
	HubLedsRedUniverse  int
	HubLedsRedAddress   int
	HubLedsBlueUniverse int
	HubLedsBlueAddress  int
}

// applyHubLedDefaults fills in Hub LED settings that a database predating them has no
// value for. Without this an existing install shows zeroes on the Settings page and
// cannot enable the simplified layout without first typing valid addresses. Values are
// only supplied where the stored one is unset, so a configured field is never
// overwritten; they persist on the next save.
func (settings *EventSettings) applyHubLedDefaults() {
	if settings.HubLedsFallback == "" {
		settings.HubLedsFallback = "full"
	}
	if settings.HubLedsRedUniverse == 0 {
		settings.HubLedsRedUniverse = 1
	}
	if settings.HubLedsRedAddress == 0 {
		settings.HubLedsRedAddress = 1
	}
	if settings.HubLedsBlueUniverse == 0 {
		settings.HubLedsBlueUniverse = 1
	}
	if settings.HubLedsBlueAddress == 0 {
		settings.HubLedsBlueAddress = 25
	}
}

func (database *Database) GetEventSettings() (*EventSettings, error) {
	allEventSettings, err := database.eventSettingsTable.getAll()
	if err != nil {
		return nil, err
	}
	if len(allEventSettings) == 1 {
		settings := &allEventSettings[0]
		settings.applyHubLedDefaults()
		return settings, nil
	}

	// Database record doesn't exist yet; create it now.
	eventSettings := EventSettings{
		Name:                        "Untitled Event",
		PlayoffType:                 DoubleEliminationPlayoff,
		NumPlayoffAlliances:         8,
		SelectionRound2Order:        "L",
		SelectionRound3Order:        "",
		SelectionShowUnpickedTeams:  true,
		TbaDownloadEnabled:          false,
		AutoConfigureTeams:          true,
		ApChannel:                   36,
		CompanionAddress:            "",
		WarmupDurationSec:           game.MatchTiming.WarmupDurationSec,
		AutoDurationSec:             game.MatchTiming.AutoDurationSec,
		PauseDurationSec:            game.MatchTiming.PauseDurationSec,
		TeleopDurationSec:           game.MatchTiming.TeleopDurationSec,
		WarningRemainingDurationSec: game.MatchTiming.WarningRemainingDurationSec,

		// Default to the standard arena: upstream's full-field fixture layout, running
		// its sequences unchanged. A practice field opts out of both.
		HubLedsSimplified:   false,
		HubLedsFallback:     "full",
		HubLedsRedUniverse:  1,
		HubLedsRedAddress:   1,
		HubLedsBlueUniverse: 1,
		HubLedsBlueAddress:  25,
	}

	if err := database.eventSettingsTable.create(&eventSettings); err != nil {
		return nil, err
	}
	return &eventSettings, nil
}

func (database *Database) UpdateEventSettings(eventSettings *EventSettings) error {
	return database.eventSettingsTable.update(eventSettings)
}
