package main

import "encoding/json"
import "net/http"

// constrainedMaxScore is the per-player score bound (half-points) accepted by
// POST /pair/constraints. The original /pair endpoint keeps no upper bound.
const constrainedMaxScore = 1000000

// scheduleConstraints restricts the next round.
//
//   - Fixed: games already decided, with explicit white/black players.
//   - Forbidden: unordered player pairs that must not meet this round.
//   - ByeCandidates: players allowed to take the bye. nil (JSON null or
//     omitted) means every player is eligible; an explicit empty list means
//     nobody may take the bye (odd number of players => NO_PAIRING).
type scheduleConstraints struct {
	Fixed         []Game      `json:"fixed"`
	Forbidden     [][2]string `json:"forbidden"`
	ByeCandidates []string    `json:"byeCandidates"`
}

type constrainedRequest struct {
	Players     []Player            `json:"players"`
	Constraints scheduleConstraints `json:"constraints"`
}

// constrainedPair validates players and constraints together and then runs
// the exhaustive solver with the restrictions participating in the search.
// Structural problems (unknown IDs, self pairs, overlapping fixtures,
// contradictory conventions, scores out of range) are *ValidationError;
// consistent input with no complete schedule yields ErrNoPairing.
func constrainedPair(req constrainedRequest) (*PairResult, error) {
	players, byID, err := validateBounded(PairRequest{Players: req.Players}, constrainedMaxScore)
	if err != nil {
		return nil, err
	}
	sc, err := buildSolverConstraints(req.Constraints, players, byID)
	if err != nil {
		return nil, err
	}
	return newSolver(players).withConstraints(sc).solve()
}

// buildSolverConstraints performs the constraint-specific structural checks:
//   - fixed games: known, distinct players; nobody occupied twice; no pair
//     (in either orientation) repeated;
//   - forbidden pairs: known, distinct players; no repeated pair; must not
//     overlap a fixed pair;
//   - bye candidates: every ID must be known (repeats are tolerated and
//     collapsed; an explicit empty list means "nobody").
func buildSolverConstraints(c scheduleConstraints, players []Player, byID map[string]int) (solverConstraints, error) {
	n := len(players)
	sc := solverConstraints{
		ForcedWith: make([]int, n),
		ForcedW:    make([]bool, n),
		Blocked:    make([][]bool, n),
	}
	for i := range sc.ForcedWith {
		sc.ForcedWith[i] = -1
		sc.Blocked[i] = make([]bool, n)
	}

	for k, g := range c.Fixed {
		if g.White == "" || g.Black == "" {
			return sc, validationf("fixed game at index %d: both white and black player IDs are required", k)
		}
		wi, ok1 := byID[g.White]
		bi, ok2 := byID[g.Black]
		if !ok1 || !ok2 {
			return sc, validationf("fixed game %q vs %q references an unknown player", g.White, g.Black)
		}
		if wi == bi {
			return sc, validationf("fixed game at index %d pairs player %q against itself", k, g.White)
		}
		// Same player must not occupy two fixtures.
		if sc.ForcedWith[wi] >= 0 {
			return sc, validationf("fixed games overlap: player %q appears more than once", g.White)
		}
		if sc.ForcedWith[bi] >= 0 {
			return sc, validationf("fixed games overlap: player %q appears more than once", g.Black)
		}
		// The pair (in either orientation) must not have been fixed before:
		// a same-direction repeat is a duplicate, a reversed one contradicts
		// the agreed colors.
		sc.ForcedWith[wi] = bi
		sc.ForcedWith[bi] = wi
		sc.ForcedW[wi] = true
		sc.ForcedW[bi] = false
	}

	for k, f := range c.Forbidden {
		a, b := f[0], f[1]
		if a == "" || b == "" {
			return sc, validationf("forbidden pair at index %d: both player IDs are required", k)
		}
		ai, ok1 := byID[a]
		bi, ok2 := byID[b]
		if !ok1 || !ok2 {
			return sc, validationf("forbidden pair %q vs %q references an unknown player", a, b)
		}
		if ai == bi {
			return sc, validationf("forbidden pair at index %d lists player %q against itself", k, a)
		}
		if sc.Blocked[ai][bi] {
			return sc, validationf("forbidden pair %q vs %q is listed more than once", a, b)
		}
		// A pair cannot be both mandatory and forbidden.
		if sc.ForcedWith[ai] == bi {
			return sc, validationf("pair %q vs %q is both fixed and forbidden", a, b)
		}
		sc.Blocked[ai][bi] = true
		sc.Blocked[bi][ai] = true
	}

	if c.ByeCandidates != nil {
		sc.ByeAllowed = make([]bool, n)
		seen := make(map[string]bool, len(c.ByeCandidates))
		for _, id := range c.ByeCandidates {
			i, ok := byID[id]
			if !ok {
				return sc, validationf("bye candidate %q is not a known player", id)
			}
			if !seen[id] {
				sc.ByeAllowed[i] = true
				seen[id] = true
			}
		}
	}

	return sc, nil
}

func constraintsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var req constrainedRequest
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		writeJSON(w, 400, errorResponse{"INVALID_INPUT", err.Error()})
		return
	}
	result, err := constrainedPair(req)
	if err != nil {
		if IsValidation(err) {
			writeJSON(w, 400, errorResponse{"INVALID_INPUT", err.Error()})
		} else {
			writeJSON(w, 200, PairResponse{Status: "NO_PAIRING"})
		}
		return
	}
	writeJSON(w, 200, PairResponse{Status: "OK", Result: result})
}
