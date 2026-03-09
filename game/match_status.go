// Copyright 2022 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Match status type and winner determination.

package game

type MatchStatus int

const (
	MatchScheduled MatchStatus = iota
	MatchHidden
	RedWonMatch
	BlueWonMatch
	TieMatch
)

func (t MatchStatus) Get() MatchStatus {
	return t
}
