// Copyright 2014 Team 254. All Rights Reserved.
// Author: pat@patfairbank.com (Patrick Fairbank)
//
// Model and datastore CRUD methods for the results (score and fouls) from a match at an event.

package model

type MatchResult struct {
	Id         int `db:"id"`
	MatchId    int
	PlayNumber int
	MatchType  MatchType
	RedCards   map[string]string
	BlueCards  map[string]string
}

// Returns a new match result object with empty maps instead of nil.
func NewMatchResult() *MatchResult {
	matchResult := new(MatchResult)
	matchResult.RedCards = make(map[string]string)
	matchResult.BlueCards = make(map[string]string)
	return matchResult
}

func (database *Database) CreateMatchResult(matchResult *MatchResult) error {
	return database.matchResultTable.create(matchResult)
}

func (database *Database) GetMatchResultForMatch(matchId int) (*MatchResult, error) {
	matchResults, err := database.matchResultTable.getAll()
	if err != nil {
		return nil, err
	}

	var mostRecentMatchResult *MatchResult
	for i, matchResult := range matchResults {
		if matchResult.MatchId == matchId &&
			(mostRecentMatchResult == nil || matchResult.PlayNumber > mostRecentMatchResult.PlayNumber) {
			mostRecentMatchResult = &matchResults[i]
		}
	}
	return mostRecentMatchResult, nil
}

func (database *Database) UpdateMatchResult(matchResult *MatchResult) error {
	return database.matchResultTable.update(matchResult)
}

func (database *Database) DeleteMatchResult(id int) error {
	return database.matchResultTable.delete(id)
}

func (database *Database) TruncateMatchResults() error {
	return database.matchResultTable.truncate()
}
