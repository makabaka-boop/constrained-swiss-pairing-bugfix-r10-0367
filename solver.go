package main

// solver exhaustively enumerates every legal next-round schedule:
// the choice of bye (when odd), every perfect matching that avoids
// re-matches, and every white/black orientation per pair that keeps each
// player's post-round color imbalance within 2.
//
// Among all complete schedules it minimizes, in order:
//  1. sum over games of |scoreA-scoreB| (scores in half-points);
//  2. sum over all players of |post-round white-minus-black count|;
//  3. the canonical schedule sequence: games ordered by the smaller
//     player ID in the game, each game encoded as (whiteID, blackID),
//     followed by the bye player ID (or -1 when nobody has a bye) —
//     lexicographically smallest wins.
//
// With at most 12 players the search is small (at most ~10400 perfect
// matchings on 12 players, each with independent 2-way color choices).
type solver struct {
	n       int
	players []Player
	played  [][]bool // played[i][j]: i and j have met before
	diff    []int    // current white-minus-black count per player

	found   bool
	bestSP  int
	bestCP  int
	bestBye int
	bestKey []int // canonical tie-break sequence
	bestP   [][2]int
	bestO   []int
}

func newSolver(players []Player) *solver {
	n := len(players)
	s := &solver{
		n:       n,
		players: players,
		played:  make([][]bool, n),
		diff:    make([]int, n),
		bestBye: -1,
	}
	for i := range s.played {
		s.played[i] = make([]bool, n)
	}
	for i, p := range players {
		for _, h := range p.History {
			j := indexOf(players, h.Opponent)
			if j >= 0 {
				s.played[i][j] = true
				s.played[j][i] = true
			}
			if h.Color == White {
				s.diff[i]++
			} else {
				s.diff[i]--
			}
		}
	}
	return s
}

func indexOf(players []Player, id string) int {
	for i := range players {
		if players[i].ID == id {
			return i
		}
	}
	return -1
}

// solve returns ErrNoPairing if no complete legal schedule exists.
func (s *solver) solve() (*PairResult, error) {
	// |imbalance| >= 5 can never return to within 2 after one game
	// (a game changes the balance by at most 1) or a bye (no change).
	for i := range s.diff {
		if absInt(s.diff[i]) >= 5 {
			return nil, ErrNoPairing
		}
	}

	free := make([]bool, s.n)
	for i := range free {
		free[i] = true
	}
	pairs := make([][2]int, 0, s.n/2)
	post := make([]int, s.n)

	if s.n%2 == 0 {
		s.match(free, pairs, -1, post)
	} else {
		for b := 0; b < s.n; b++ {
			// A player who already had a bye cannot take another.
			if len(s.players[b].Byes) > 0 {
				continue
			}
			free[b] = false
			post[b] = s.diff[b]
			s.match(free, pairs, b, post)
			post[b] = 0
			free[b] = true
		}
	}

	if !s.found {
		return nil, ErrNoPairing
	}

	res := &PairResult{
		Games:        make([]Game, 0, len(s.bestP)),
		ScorePenalty: s.bestSP,
		ColorPenalty: s.bestCP,
	}
	for k, pr := range s.bestP {
		lo, hi := pr[0], pr[1]
		w, b := lo, hi
		if s.bestO[k] == 1 {
			w, b = hi, lo
		}
		res.Games = append(res.Games, Game{White: s.players[w].ID, Black: s.players[b].ID})
	}
	if s.bestBye >= 0 {
		res.Bye = s.players[s.bestBye].ID
	}
	return res, nil
}

func bIdx(i int) int { return i }

// match enumerates perfect matchings over the free players. Because the
// lowest-index free player is always paired first, pairs are appended in
// increasing order of their smaller endpoint — the canonical game order.
func (s *solver) match(free []bool, pairs [][2]int, bye int, post []int) {
	i := -1
	for k := 0; k < s.n; k++ {
		if free[k] {
			i = k
			break
		}
	}
	if i == -1 {
		s.assignColors(pairs, bye, post, 0, 0, 0, nil, nil)
		return
	}
	free[i] = false
	for j := i + 1; j < s.n; j++ {
		if !free[j] || s.played[i][j] {
			continue
		}
		free[j] = false
		s.match(free, append(pairs, [2]int{i, j}), bye, post)
		free[j] = true
	}
	free[i] = true
}

// assignColors enumerates white/black orientations pair by pair.
// sp accumulates the score-difference penalty (it is identical for every
// orientation of a matching, computed once while walking the pairs);
// cp accumulates the post-round color-imbalance penalty.
func (s *solver) assignColors(pairs [][2]int, bye int, post []int, k, sp, cp int, key []int, orient []int) {
	if k == len(pairs) {
		// The bye player does not play; their imbalance is unchanged and
		// must also be within 2 after the round.
		if bye >= 0 {
			if absInt(post[bye]) > 2 {
				return
			}
			cp += absInt(post[bye])
		}
		fullKey := append(append([]int(nil), key...), bye)
		s.consider(sp, cp, bye, pairs, orient, fullKey)
		return
	}

	lo, hi := pairs[k][0], pairs[k][1]
	spPair := absInt(s.players[lo].Score - s.players[hi].Score)

	// Orientation 0: lo white, hi black.
	dLo := s.diff[lo] + 1
	dHi := s.diff[hi] - 1
	if absInt(dLo) <= 2 && absInt(dHi) <= 2 {
		post[lo], post[hi] = dLo, dHi
		s.assignColors(pairs, bye, post, k+1, sp+spPair, cp+absInt(dLo)+absInt(dHi),
			appendKey(key, lo, hi), appendOrient(orient, 0))
	}

	// Orientation 1: hi white, lo black.
	dLo = s.diff[lo] - 1
	dHi = s.diff[hi] + 1
	if absInt(dLo) <= 2 && absInt(dHi) <= 2 {
		post[lo], post[hi] = dLo, dHi
		s.assignColors(pairs, bye, post, k+1, sp+spPair, cp+absInt(dLo)+absInt(dHi),
			appendKey(key, hi, lo), appendOrient(orient, 1))
	}
}

func appendKey(key []int, w, b int) []int {
	out := make([]int, 0, len(key)+2)
	out = append(out, key...)
	return append(out, w, b)
}

func appendOrient(o []int, v int) []int {
	out := make([]int, 0, len(o)+1)
	out = append(out, o...)
	return append(out, v)
}

// consider applies the lexicographic objective ordering.
func (s *solver) consider(sp, cp, bye int, pairs [][2]int, orient, key []int) {
	if !s.found ||
		sp < s.bestSP ||
		(sp == s.bestSP && cp < s.bestCP) ||
		(sp == s.bestSP && cp == s.bestCP && lexLess(key, s.bestKey)) {
		s.found = true
		s.bestSP = sp
		s.bestCP = cp
		s.bestBye = bye
		s.bestKey = key
		s.bestP = append([][2]int(nil), pairs...)
		s.bestO = append([]int(nil), orient...)
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// lexLess compares integer sequences lexicographically.
func lexLess(a, b []int) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}
