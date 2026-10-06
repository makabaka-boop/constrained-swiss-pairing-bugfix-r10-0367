package main

import (
	"errors"
	"fmt"
)

// ValidationError marks structurally inconsistent input (as opposed to a
// consistent input for which no legal pairing exists).
type ValidationError struct{ msg string }

func (e *ValidationError) Error() string { return e.msg }

func validationf(format string, args ...any) error {
	return &ValidationError{msg: fmt.Sprintf(format, args...)}
}

// IsValidation reports whether err is a structural input error.
func IsValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// validate performs all structural checks on the request:
//   - 4..12 players, unique non-empty IDs, non-negative integer half-point scores;
//   - every history entry refers to a known opponent on a positive round,
//     with a legal color, and no player reports twice in the same round;
//   - history is mutual and consistent: if A reports playing B with color C
//     in round r, B must report playing A with the opposite color in round r;
//   - bye round numbers are positive, unique per player, and never overlap
//     with a round in which that player actually played.
//
// It returns players sorted by ID and a per-player index lookup.
func validate(req PairRequest) ([]Player, map[string]int, error) {
	n := len(req.Players)
	if n < 4 || n > 12 {
		return nil, nil, validationf("expected 4..12 players, got %d", n)
	}

	players := make([]Player, n)
	copy(players, req.Players)
	byID := make(map[string]int, n)

	// Sort by ID for a canonical order (insertion sort is fine for n<=12).
	for i := 1; i < n; i++ {
		for j := i; j > 0 && players[j-1].ID > players[j].ID; j-- {
			players[j-1], players[j] = players[j], players[j-1]
		}
	}
	for i, p := range players {
		if p.ID == "" {
			return nil, nil, validationf("player at position %d has an empty ID", i)
		}
		if _, dup := byID[p.ID]; dup {
			return nil, nil, validationf("duplicate player ID %q", p.ID)
		}
		if p.Score < 0 {
			return nil, nil, validationf("player %q has negative score %d", p.ID, p.Score)
		}
		byID[p.ID] = i
	}

	for _, p := range players {
		rounds := make(map[int]bool, len(p.History))
		for _, h := range p.History {
			if h.Round < 1 {
				return nil, nil, validationf("player %q: round numbers must be positive, got %d", p.ID, h.Round)
			}
			if h.Opponent == "" {
				return nil, nil, validationf("player %q, round %d: empty opponent ID", p.ID, h.Round)
			}
			if h.Opponent == p.ID {
				return nil, nil, validationf("player %q cannot play against itself in round %d", p.ID, h.Round)
			}
			if _, ok := byID[h.Opponent]; !ok {
				return nil, nil, validationf("player %q, round %d: unknown opponent %q", p.ID, h.Round, h.Opponent)
			}
			if h.Color != White && h.Color != Black {
				return nil, nil, validationf("player %q, round %d: color must be %q or %q, got %q",
					p.ID, h.Round, White, Black, h.Color)
			}
			// Same player appearing twice in one round (two reported games).
			if rounds[h.Round] {
				return nil, nil, validationf("player %q appears twice in round %d", p.ID, h.Round)
			}
			rounds[h.Round] = true
		}

		byeSet := make(map[int]bool, len(p.Byes))
		for _, r := range p.Byes {
			if r < 1 {
				return nil, nil, validationf("player %q: bye round numbers must be positive, got %d", p.ID, r)
			}
			if byeSet[r] {
				return nil, nil, validationf("player %q has a repeated bye in round %d", p.ID, r)
			}
			byeSet[r] = true
			if rounds[r] {
				return nil, nil, validationf("player %q both plays and has a bye in round %d", p.ID, r)
			}
		}
	}

	// Reciprocal consistency: both sides must report the game, same round,
	// opposite colors. Any missing or conflicting record rejects the input.
	for _, p := range players {
		for _, h := range p.History {
			opp := players[byID[h.Opponent]]
			var reciprocal *Record
			for k := range opp.History {
				if opp.History[k].Round == h.Round {
					reciprocal = &opp.History[k]
					break
				}
			}
			if reciprocal == nil {
				return nil, nil, validationf("player %q reports game vs %q in round %d but %q has no matching record",
					p.ID, h.Opponent, h.Round, h.Opponent)
			}
			if reciprocal.Opponent != p.ID {
				return nil, nil, validationf("round %d: %q lists opponent %q but %q lists opponent %q",
					h.Round, p.ID, h.Opponent, h.Opponent, reciprocal.Opponent)
			}
			if reciprocal.Color == h.Color {
				return nil, nil, validationf("round %d: %q and %q both claim color %q",
					h.Round, p.ID, h.Opponent, h.Color)
			}
			if reciprocal.Color != White && reciprocal.Color != Black {
				return nil, nil, validationf("player %q, round %d: unknown color %q",
					h.Opponent, h.Round, reciprocal.Color)
			}
		}
	}

	return players, byID, nil
}
