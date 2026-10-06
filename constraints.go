package main

import (
	"encoding/json"
	"net/http"
)

// maxConstrainedScore is the per-player score ceiling (half-points) for
// the venue-constrained endpoint.
const maxConstrainedScore = 1_000_000

// scheduleConstraints carries venue-level restrictions for the next round.
// ByeCandidates is a pointer so the wire format can distinguish "field
// absent / null" (every player may take the bye, subject to prior-bye
// rules) from an explicit empty list (nobody may take the bye).
type scheduleConstraints struct {
	Fixed         []Game      `json:"fixed"`
	Forbidden     [][2]string `json:"forbidden"`
	ByeCandidates *[]string   `json:"byeCandidates"`
}

type constrainedRequest struct {
	Players     []Player            `json:"players"`
	Constraints scheduleConstraints `json:"constraints"`
}

// parsedConstraints is the index-resolved, structurally validated form of
// scheduleConstraints, ready to be installed onto a solver.
type parsedConstraints struct {
	// fixedMate[i] = j when i must play j (set on both endpoints); -1 when
	// i has no fixed game.
	fixedMate []int
	// fixedOrient[{lo,hi}] = 0: lo plays white; 1: hi plays white.
	fixedOrient map[[2]int]int
	// forbidden[i][j] marks a blocked undirected pair.
	forbidden [][]bool
	// byeOK == nil means the bye is unrestricted; otherwise only players
	// with byeOK[i] == true may take it.
	byeOK []bool
}

// constrainedPair validates players and constraints together, then solves
// the next round with every restriction (fixed games, forbidden pairs,
// bye eligibility, history rematches, color balance, prior byes) acting
// simultaneously. Structural problems return a *ValidationError; a
// consistent request without any complete schedule returns ErrNoPairing.
func constrainedPair(req constrainedRequest) (*PairResult, error) {
	players, byID, err := validateMaxScore(PairRequest{Players: req.Players}, maxConstrainedScore)
	if err != nil {
		return nil, err
	}
	c, err := parseConstraints(req.Constraints, players, byID)
	if err != nil {
		return nil, err
	}
	s := newSolver(players)
	s.apply(c)
	return s.solve()
}

// parseConstraints performs all structural checks on the venue
// constraints themselves:
//   - every fixed game names two known, distinct players, and no player
//     appears in more than one fixed game;
//   - every forbidden pair names two known, distinct players and is not
//     duplicated (the pair is undirected);
//   - a fixed pair and a forbidden pair must not name the same two
//     players (mutually conflicting arrangements);
//   - every bye candidate is a known player (duplicates are harmless).
//
// Whether an arrangement can actually be realized together with the
// reported history and color balances is a feasibility question and is
// left to the solver (ErrNoPairing), not validation.
func parseConstraints(c scheduleConstraints, players []Player, byID map[string]int) (*parsedConstraints, error) {
	n := len(players)
	out := &parsedConstraints{
		fixedMate:   make([]int, n),
		fixedOrient: make(map[[2]int]int),
	}
	for i := range out.fixedMate {
		out.fixedMate[i] = -1
	}

	// Fixed games. Repeating the same pair twice is player reuse, so the
	// generic occupancy check rejects it as well.
	for _, g := range c.Fixed {
		wi, ok := byID[g.White]
		if !ok {
			return nil, validationf("fixed game names unknown player %q", g.White)
		}
		bi, ok := byID[g.Black]
		if !ok {
			return nil, validationf("fixed game names unknown player %q", g.Black)
		}
		if wi == bi {
			return nil, validationf("fixed game pairs player %q with itself", g.White)
		}
		reused := ""
		if out.fixedMate[wi] >= 0 {
			reused = g.White
		} else if out.fixedMate[bi] >= 0 {
			reused = g.Black
		}
		if reused != "" {
			return nil, validationf("fixed games reuse player %q", reused)
		}
		lo, hi := wi, bi
		if lo > hi {
			lo, hi = hi, lo
		}
		// lo plays white exactly when the fixed white is the lower index.
		orient := 0
		if wi == hi {
			orient = 1
		}
		out.fixedMate[wi] = bi
		out.fixedMate[bi] = wi
		out.fixedOrient[[2]int{lo, hi}] = orient
	}

	// Forbidden undirected pairs.
	out.forbidden = make([][]bool, n)
	for i := range out.forbidden {
		out.forbidden[i] = make([]bool, n)
	}
	seenPair := make(map[[2]int]bool)
	for _, f := range c.Forbidden {
		i, ok := byID[f[0]]
		if !ok {
			return nil, validationf("forbidden pair names unknown player %q", f[0])
		}
		j, ok := byID[f[1]]
		if !ok {
			return nil, validationf("forbidden pair names unknown player %q", f[1])
		}
		if i == j {
			return nil, validationf("forbidden pair lists player %q against itself", f[0])
		}
		lo, hi := i, j
		if lo > hi {
			lo, hi = hi, lo
		}
		key := [2]int{lo, hi}
		if seenPair[key] {
			return nil, validationf("forbidden pair %q/%q is listed more than once", players[lo].ID, players[hi].ID)
		}
		seenPair[key] = true
		out.forbidden[lo][hi] = true
		out.forbidden[hi][lo] = true
	}

	// A pair cannot be both fixed and forbidden.
	for pair := range out.fixedOrient {
		if seenPair[pair] {
			return nil, validationf("pair is both fixed and forbidden: %q / %q",
				players[pair[0]].ID, players[pair[1]].ID)
		}
	}

	// Bye candidates. nil pointer: unrestricted. Empty list: nobody may bye.
	if c.ByeCandidates != nil {
		out.byeOK = make([]bool, n)
		for _, id := range *c.ByeCandidates {
			i, ok := byID[id]
			if !ok {
				return nil, validationf("bye candidate %q is not a known player", id)
			}
			out.byeOK[i] = true
		}
	}

	return out, nil
}

func constraintsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req constrainedRequest
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"INVALID_INPUT", err.Error()})
		return
	}
	result, err := constrainedPair(req)
	if err != nil {
		if IsValidation(err) {
			writeJSON(w, http.StatusBadRequest, errorResponse{"INVALID_INPUT", err.Error()})
		} else {
			writeJSON(w, http.StatusOK, PairResponse{Status: "NO_PAIRING"})
		}
		return
	}
	writeJSON(w, http.StatusOK, PairResponse{Status: "OK", Result: result})
}
