// Copyright 2019 Team 254. All Rights Reserved.
// Portions Copyright Team 841. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Game-specific audience sound timings.

package game

type MatchSound struct {
	Name          string
	FileExtension string
	MatchTimeSec  float64
}

// List of sounds and how many seconds into the match they are played. A negative time indicates that the sound can only
// be triggered explicitly.
//
// "shift_change" is deliberately absent. Upstream schedules it here, at four timestamps
// computed from the shift constants. Bioarena sounds it from the lighting transition in
// field/arena.go instead, so the cue and the HUB lights come from one computed boundary
// rather than two that could disagree. Adding it back here would play it twice.
var MatchSounds []*MatchSound

func UpdateMatchSounds() {
	MatchSounds = []*MatchSound{
		{
			"start",
			"wav",
			0,
		},
		{
			"end",
			"wav",
			float64(MatchTiming.AutoDurationSec),
		},
		{
			"resume",
			"wav",
			float64(MatchTiming.AutoDurationSec + MatchTiming.PauseDurationSec),
		},
		{
			"warning",
			"wav",
			float64(
				MatchTiming.AutoDurationSec + MatchTiming.PauseDurationSec + MatchTiming.TeleopDurationSec -
					MatchTiming.WarningRemainingDurationSec,
			),
		},
		{
			"end",
			"wav",
			float64(MatchTiming.AutoDurationSec + MatchTiming.PauseDurationSec + MatchTiming.TeleopDurationSec),
		},
		{
			"abort",
			"wav",
			-1,
		},
		{
			"match_result",
			"wav",
			-1,
		},
		{
			"pick_clock",
			"wav",
			-1,
		},
		{
			"pick_clock_expired",
			"wav",
			-1,
		},
	}
}
