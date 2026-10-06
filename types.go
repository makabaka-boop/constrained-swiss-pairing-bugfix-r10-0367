package main

import "errors"

// Color of a game, recorded from the reporting player's perspective.
type Color string

const (
	White Color = "white"
	Black Color = "black"
)

// Record is one player's report of a single previously played round.
// Opponent and Color describe that player's opponent and the player's own color.
type Record struct {
	Round    int    `json:"round"`
	Opponent string `json:"opponent"`
	Color    Color  `json:"color"`
}

// Player is the current state of one competitor.
// Scores are expressed in half-points (integers; e.g. 1 point == 2).
type Player struct {
	ID      string   `json:"id"`
	Score   int      `json:"score"`
	History []Record `json:"history"`
	Byes    []int    `json:"byes"`
}

// PairRequest is the service input.
type PairRequest struct {
	Players []Player `json:"players"`
}

// Game is one paired game, listing the player who plays white first.
type Game struct {
	White string `json:"white"`
	Black string `json:"black"`
}

// PairResult is a complete next-round schedule.
// Bye is "" when the number of players is even.
type PairResult struct {
	Games        []Game `json:"games"`
	Bye          string `json:"bye"`
	ScorePenalty int    `json:"scorePenalty"`
	ColorPenalty int    `json:"colorPenalty"`
}

// PairResponse wraps the result for the HTTP API.
type PairResponse struct {
	Status string      `json:"status"`
	Result *PairResult `json:"result,omitempty"`
}

// ErrNoPairing is returned when the input is consistent but no complete,
// legal pairing (including bye and colors) exists. A partial schedule is
// never returned.
var ErrNoPairing = errors.New("NO_PAIRING")
