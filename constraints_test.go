package main

import (
	"bytes"
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Fixed / forbidden / bye-eligibility library-level regression cases
// ---------------------------------------------------------------------------

func sc(fixed []Game, forbidden [][2]string, byeCand []string) scheduleConstraints {
	return scheduleConstraints{Fixed: fixed, Forbidden: forbidden, ByeCandidates: byeCand}
}

func cpPair(t *testing.T, ps []Player, c scheduleConstraints) (*PairResult, error) {
	t.Helper()
	return constrainedPair(constrainedRequest{
		Players:     ps,
		Constraints: c,
	})
}

func gamesMap(gs []Game) map[[2]string]bool {
	m := map[[2]string]bool{}
	for _, g := range gs {
		m[[2]string{g.White, g.Black}] = true
	}
	return m
}

// Forbidding the unconstrained optimum must not yield NO_PAIRING while an
// alternative complete schedule exists.
func TestCForbiddenDropsOptimumAlternativeExists(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].Score, ps[1].Score = 10, 10
	res, err := cpPair(t, ps, sc(nil, [][2]string{{"A", "B"}, {"C", "D"}}, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ScorePenalty != 20 {
		t.Fatalf("scorePenalty = %d, want 20; games=%v", res.ScorePenalty, res.Games)
	}
	// Two matchings share sp=20/cp=4; canonical key (A,C)(B,D) beats (A,D)(B,C).
	want := []Game{{White: "A", Black: "C"}, {White: "B", Black: "D"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
}

// A fixed game outside the unconstrained optimum must still be served.
func TestCFixedForcesDifferentMatching(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].Score, ps[1].Score = 10, 10
	// Fix B-D with D taking white (the reverse of the canonical orientation);
	// A-C is then forced as the remaining pair.
	res, err := cpPair(t, ps, sc([]Game{{White: "D", Black: "B"}}, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Game{{White: "A", Black: "C"}, {White: "D", Black: "B"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
	if res.Bye != "" {
		t.Fatalf("unexpected bye %q", res.Bye)
	}
}

// Multiple fixed games cover the field and output stays canonically ordered.
func TestCFixedAllGamesCanonicalOrder(t *testing.T) {
	ps := players("A", "B", "C", "D")
	res, err := cpPair(t, ps, sc([]Game{
		{White: "D", Black: "C"},
		{White: "B", Black: "A"},
	}, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Game{{White: "B", Black: "A"}, {White: "D", Black: "C"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
}

// Fixed rematch: consistent input but the only demanded game is historically
// impossible => NO_PAIRING, never a partial schedule.
func TestCFixedRematchIsNoPairing(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	_, err := cpPair(t, ps, sc([]Game{{White: "A", Black: "B"}}, nil, nil))
	if err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

// Fixed orientation that pushes a player past the color bound is infeasible.
func TestCFixedColorBeyondBalanceIsNoPairing(t *testing.T) {
	ps := players("A", "B", "C", "D")
	// A has twice played white (diff = +2) and only C is unplayed.
	ps[0].History = []Record{hist(1, "B", White), hist(2, "D", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	ps[3].History = []Record{hist(2, "A", Black)}
	_, err := cpPair(t, ps, sc([]Game{{White: "A", Black: "C"}}, nil, nil))
	if err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
	// The opposite orientation is feasible.
	res, err := cpPair(t, ps, sc([]Game{{White: "C", Black: "A"}}, nil, nil))
	if err != nil {
		t.Fatalf("feasible orientation rejected: %v", err)
	}
	if !gamesMap(res.Games)[[2]string{"C", "A"}] {
		t.Fatalf("fixed orientation missing: %v", res.Games)
	}
}

// Forbidding every opponent of a player leaves no schedule.
func TestCForbiddenAllOpponentsIsNoPairing(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White), hist(2, "C", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	ps[2].History = []Record{hist(2, "A", Black)}
	// A can only play D unconstrained; block that too.
	_, err := cpPair(t, ps, sc(nil, [][2]string{{"A", "D"}}, nil))
	if err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

// --- bye eligibility ------------------------------------------------------

func TestCByeCandidatesRestrictsOddField(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	// Unconstrained canonical winner gives E the bye; force it onto A.
	res, err := cpPair(t, ps, sc(nil, nil, []string{"A"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye != "A" {
		t.Fatalf("bye = %q, want A", res.Bye)
	}
	want := []Game{{White: "B", Black: "C"}, {White: "D", Black: "E"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
}

func TestCByeCandidatesNullMeansAllAllowed(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	res, err := cpPair(t, ps, sc(nil, nil, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye != "E" {
		t.Fatalf("bye = %q, want E (canonical unrestricted choice)", res.Bye)
	}
}

func TestCByeCandidatesEmptyMeansNobodyOdd(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	_, err := cpPair(t, ps, sc(nil, nil, []string{}))
	if err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing for empty candidate list on odd field", err)
	}
}

func TestCByeCandidatesEmptyEvenFieldOK(t *testing.T) {
	ps := players("A", "B", "C", "D")
	res, err := cpPair(t, ps, sc(nil, nil, []string{}))
	if err != nil {
		t.Fatalf("empty candidate list must be harmless on even fields, got %v", err)
	}
	if len(res.Games) != 2 || res.Bye != "" {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestCByeCandidatesIntersectPriorBye(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	ps[0].Byes = []int{1}
	_, err := cpPair(t, ps, sc(nil, nil, []string{"A"}))
	if err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing (sole candidate already had a bye)", err)
	}
}

func TestCByeCandidateLockedByFixedGame(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	// Four players are locked into games; only E is free but the allowed
	// bye list names only fixed players.
	fixed := []Game{{White: "A", Black: "B"}, {White: "C", Black: "D"}}
	_, err := cpPair(t, ps, sc(fixed, nil, []string{"A", "B", "C", "D"}))
	if err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
	// Allowing E makes it feasible.
	res, err := cpPair(t, ps, sc(fixed, nil, []string{"E"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye != "E" || !gamesMap(res.Games)[[2]string{"A", "B"}] {
		t.Fatalf("unexpected result %+v", res)
	}
}

func TestCByeCandidatesDedup(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	res, err := cpPair(t, ps, sc(nil, nil, []string{"A", "A"}))
	if err != nil {
		t.Fatalf("repeated candidate ID should be tolerated, got %v", err)
	}
	if res.Bye != "A" {
		t.Fatalf("bye = %q, want A", res.Bye)
	}
}

// --- structural validation ------------------------------------------------

func TestCInvalidConstraints(t *testing.T) {
	ps := players("A", "B", "C", "D")
	cases := []struct {
		name string
		c    scheduleConstraints
	}{
		{"fixed unknown white", sc([]Game{{White: "Z", Black: "B"}}, nil, nil)},
		{"fixed unknown black", sc([]Game{{White: "A", Black: "Z"}}, nil, nil)},
		{"fixed missing id", sc([]Game{{White: "", Black: "B"}}, nil, nil)},
		{"fixed self", sc([]Game{{White: "A", Black: "A"}}, nil, nil)},
		{"fixed overlapping player", sc([]Game{{White: "A", Black: "B"}, {White: "A", Black: "C"}}, nil, nil)},
		{"fixed duplicated pair", sc([]Game{{White: "A", Black: "B"}, {White: "B", Black: "A"}}, nil, nil)},
		{"forbidden unknown", sc(nil, [][2]string{{"A", "Z"}}, nil)},
		{"forbidden self", sc(nil, [][2]string{{"A", "A"}}, nil)},
		{"forbidden missing", sc(nil, [][2]string{{"", "B"}}, nil)},
		{"forbidden duplicated", sc(nil, [][2]string{{"A", "B"}, {"B", "A"}}, nil)},
		{"fixed vs forbidden overlap", sc([]Game{{White: "A", Black: "B"}}, [][2]string{{"B", "A"}}, nil)},
		{"bye candidate unknown", sc(nil, nil, []string{"Z"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cpPair(t, ps, tc.c)
			if !IsValidation(err) {
				t.Fatalf("err = %v, want validation error", err)
			}
		})
	}
}

func TestCScoreBound(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].Score = 1000001
	if _, err := cpPair(t, ps, sc(nil, nil, nil)); !IsValidation(err) {
		t.Fatalf("score above 1000000 must be rejected, got %v", err)
	}
	ps[0].Score = 1000000
	if _, err := cpPair(t, ps, sc(nil, nil, nil)); err != nil {
		t.Fatalf("score exactly 1000000 must be accepted, got %v", err)
	}
	// The original endpoint stays unbounded.
	ps[0].Score = 1000001
	if _, err := Pair(PairRequest{Players: ps}); err != nil {
		t.Fatalf("/pair must remain unbounded, got %v", err)
	}
}

// Requests without new constraints produce exactly the original pairing,
// including on NO_PAIRING inputs.
func TestCNoConstraintsMatchesOriginal(t *testing.T) {
	rng := rand.New(rand.NewSource(7777))
	for it := 0; it < 300; it++ {
		req := genRandomInstance(rng)
		orig, errOrig := Pair(req)
		// Empty constraints object: nil slices => everything unrestricted.
		got, errGot := constrainedPair(constrainedRequest{Players: req.Players})
		if (errOrig == nil) != (errGot == nil) {
			t.Fatalf("case %d: orig err %v, constrained err %v", it, errOrig, errGot)
		}
		if errOrig != nil {
			if errOrig != errGot {
				t.Fatalf("case %d: errors %v vs %v", it, errOrig, errGot)
			}
			continue
		}
		if !reflect.DeepEqual(got, orig) {
			t.Fatalf("case %d: %+v != %+v", it, got, orig)
		}
	}
}

// ---------------------------------------------------------------------------
// Independent brute-force oracle for constrained rounds + differential fuzz
// ---------------------------------------------------------------------------

type constrOracleResult struct {
	ok     bool
	sp, cp int
	key    []int
	games  []Game
	bye    string
}

// bruteForceConstrained enumerates every schedule satisfying the same
// constraints independently of the production solver. Fixed/forbidden pairs
// are passed by ID; byeCand == nil means unrestricted eligibility, an empty
// slice means nobody is eligible.
func bruteForceConstrained(t *testing.T, req PairRequest, fixed []Game, forbidden [][2]string, byeCand []string) constrOracleResult {
	t.Helper()
	ids := make([]string, len(req.Players))
	score := map[string]int{}
	histMap := map[string][]Record{}
	byes := map[string][]int{}
	for i, p := range req.Players {
		ids[i] = p.ID
		score[p.ID] = p.Score
		histMap[p.ID] = p.History
		byes[p.ID] = p.Byes
	}
	sort.Strings(ids)
	rank := map[string]int{}
	for i, id := range ids {
		rank[id] = i
	}
	n := len(ids)

	played := make([][]bool, n)
	blocked := make([][]bool, n)
	forced := make([]int, n)
	forcedW := make([]bool, n)
	for i := range played {
		played[i] = make([]bool, n)
		blocked[i] = make([]bool, n)
		forced[i] = -1
	}
	diff := make([]int, n)
	hadBye := make([]bool, n)
	for i, id := range ids {
		for _, h := range histMap[id] {
			played[i][rank[h.Opponent]] = true
			if h.Color == White {
				diff[i]++
			} else {
				diff[i]--
			}
		}
		if len(byes[id]) > 0 {
			hadBye[i] = true
		}
	}
	for _, f := range forbidden {
		a, b := rank[f[0]], rank[f[1]]
		blocked[a][b] = true
		blocked[b][a] = true
	}
	for _, g := range fixed {
		a, b := rank[g.White], rank[g.Black]
		forced[a], forced[b] = b, a
		forcedW[a], forcedW[b] = true, false
	}
	var eligible []bool
	if byeCand != nil {
		eligible = make([]bool, n)
		for _, id := range byeCand {
			eligible[rank[id]] = true
		}
	}
	mayBye := func(i int) bool {
		if hadBye[i] || forced[i] >= 0 {
			return false
		}
		return eligible == nil || eligible[i]
	}

	best := constrOracleResult{ok: false}
	all := (1 << n) - 1

	considerBye := func(bye int) {
		mask := all
		if bye >= 0 {
			mask ^= 1 << bye
		}
		var pairs [][2]int
		var enumerateMatch func(int) bool
		enumerateMatch = func(m int) bool {
			if m == 0 {
				post := make([]int, n)
				if bye >= 0 {
					post[bye] = diff[bye]
				}
				foundLeaf := false
				var orient func(int, int, int, []int, []Game)
				orient = func(k, spAcc, cpAcc int, key []int, games []Game) {
					if k == len(pairs) {
						if bye >= 0 && absInt(post[bye]) > 2 {
							return
						}
						if bye >= 0 {
							cpAcc += absInt(post[bye])
						}
						fullKey := append(append([]int(nil), key...), bye)
						cand := constrOracleResult{
							ok:    true,
							sp:    spAcc,
							cp:    cpAcc,
							key:   fullKey,
							games: append([]Game(nil), games...),
							bye:   "",
						}
						if bye >= 0 {
							cand.bye = ids[bye]
						}
						foundLeaf = true
						if !best.ok || tupleLess(cand.sp, cand.cp, cand.key, best.sp, best.cp, best.key) {
							best = cand
						}
						return
					}
					lo, hi := pairs[k][0], pairs[k][1]
					spPair := absInt(score[ids[lo]] - score[ids[hi]])
					try := func(w, b int) {
						dw, db := diff[w]+1, diff[b]-1
						if absInt(dw) > 2 || absInt(db) > 2 {
							return
						}
						post[w], post[b] = dw, db
						orient(k+1, spAcc+spPair, cpAcc+absInt(dw)+absInt(db),
							append(append([]int(nil), key...), w, b),
							append(append([]Game(nil), games...), Game{White: ids[w], Black: ids[b]}))
					}
					if forced[lo] == hi {
						if forcedW[lo] {
							try(lo, hi)
						} else {
							try(hi, lo)
						}
					} else {
						try(lo, hi)
						try(hi, lo)
					}
				}
				orient(0, 0, 0, nil, nil)
				return foundLeaf
			}
			i := bitIndex(m)
			rem := m ^ (1 << i)
			if fp := forced[i]; fp >= 0 {
				if rem&(1<<fp) == 0 || played[i][fp] || blocked[i][fp] {
					return false
				}
				pairs = append(pairs, pairCanon(i, fp))
				ok := enumerateMatch(rem ^ (1 << fp))
				pairs = pairs[:len(pairs)-1]
				return ok
			}
			anyBranch := false
			bits := rem
			for bits != 0 {
				jb := bits & -bits
				j := bitIndex(jb)
				bits ^= jb
				if played[i][j] || blocked[i][j] || forced[j] >= 0 {
					continue
				}
				pairs = append(pairs, [2]int{i, j})
				if enumerateMatch(rem ^ jb) {
					anyBranch = true
				}
				pairs = pairs[:len(pairs)-1]
			}
			return anyBranch
		}
		enumerateMatch(mask)
	}

	if n%2 == 0 {
		considerBye(-1)
	} else {
		for b := 0; b < n; b++ {
			if mayBye(b) {
				considerBye(b)
			}
		}
	}
	return best
}

// genValidConstraints builds structurally valid random constraints for a
// request (history-consistent). Returns fixed games, forbidden pairs and the
// candidate list (nil = unrestricted).
func genValidConstraints(rng *rand.Rand, req PairRequest) ([]Game, [][2]string, []string) {
	ids := make([]string, len(req.Players))
	played := map[[2]string]bool{}
	for i, p := range req.Players {
		ids[i] = p.ID
		for _, h := range p.History {
			a, b := p.ID, h.Opponent
			if a > b {
				a, b = b, a
			}
			played[[2]string{a, b}] = true
		}
	}
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)

	// Random fixed matching over a shuffled prefix, skipping played pairs.
	perm := rng.Perm(len(sorted))
	used := map[string]bool{}
	var fixed []Game
	for k := 0; k+1 < len(perm); k += 2 {
		if rng.Intn(2) == 0 {
			continue
		}
		a, b := sorted[perm[k]], sorted[perm[k+1]]
		lo, hi := a, b
		if lo > hi {
			lo, hi = hi, lo
		}
		if played[[2]string{lo, hi}] {
			continue
		}
		if rng.Intn(2) == 0 {
			fixed = append(fixed, Game{White: a, Black: b})
		} else {
			fixed = append(fixed, Game{White: b, Black: a})
		}
		used[a], used[b] = true, true
	}
	fixedSet := map[[2]string]bool{}
	for _, g := range fixed {
		a, b := g.White, g.Black
		if a > b {
			a, b = b, a
		}
		fixedSet[[2]string{a, b}] = true
	}

	var forbidden [][2]string
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			pair := [2]string{sorted[i], sorted[j]}
			if played[pair] || fixedSet[pair] {
				continue
			}
			if rng.Intn(3) == 0 {
				forbidden = append(forbidden, pair)
			}
		}
	}

	var byeCand []string
	if rng.Intn(3) == 0 {
		byeCand = nil // unrestricted
	} else {
		byeCand = []string{}
		for _, id := range sorted {
			// Sometimes only free players, sometimes anyone, so the
			// fixed/bye and prior-bye intersections get exercised too.
			if used[id] && rng.Intn(2) == 0 {
				continue
			}
			if rng.Intn(2) == 0 {
				byeCand = append(byeCand, id)
			}
		}
	}
	return fixed, forbidden, byeCand
}

func TestCFuzzAgainstOracle(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	rng := rand.New(rand.NewSource(909090))
	for it := 0; it < 4000; it++ {
		req := genRandomInstance(rng)
		fixed, forbidden, byeCand := genValidConstraints(rng, req)
		oracle := bruteForceConstrained(t, req, fixed, forbidden, byeCand)
		res, err := constrainedPair(constrainedRequest{
			Players:     req.Players,
			Constraints: sc(fixed, forbidden, byeCand),
		})
		if !oracle.ok {
			if err != ErrNoPairing {
				t.Fatalf("case %d: oracle NO_PAIRING, service gave %v / %v (fixed=%v forb=%v bye=%v)",
					it, res, err, fixed, forbidden, byeCand)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: unexpected %v (fixed=%v forb=%v bye=%v)", it, err, fixed, forbidden, byeCand)
		}
		if res.ScorePenalty != oracle.sp || res.ColorPenalty != oracle.cp {
			t.Fatalf("case %d: penalties (%d,%d) want (%d,%d)",
				it, res.ScorePenalty, res.ColorPenalty, oracle.sp, oracle.cp)
		}
		if !reflect.DeepEqual(res.Games, oracle.games) || res.Bye != oracle.bye {
			t.Fatalf("case %d: %v/%q want %v/%q (fixed=%v forb=%v bye=%v)",
				it, res.Games, res.Bye, oracle.games, oracle.bye, fixed, forbidden, byeCand)
		}
		// Every fixed game must appear verbatim; no forbidden pair may meet.
		got := gamesMap(res.Games)
		for _, g := range fixed {
			if !got[[2]string{g.White, g.Black}] {
				t.Fatalf("case %d: fixed game %v missing from %v", it, g, res.Games)
			}
		}
		for _, f := range forbidden {
			if got[[2]string{f[0], f[1]}] || got[[2]string{f[1], f[0]}] {
				t.Fatalf("case %d: forbidden pair %v present in %v", it, f, res.Games)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP regression
// ---------------------------------------------------------------------------

func postConstraints(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/pair/constraints", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	constraintsHandler(rec, req)
	return rec
}

func TestHTTPConstrainedOK(t *testing.T) {
	ps := players("A", "B", "C", "D")
	rec := postConstraints(t, constrainedRequest{
		Players:     ps,
		Constraints: sc([]Game{{White: "D", Black: "B"}}, nil, nil),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" || resp.Result == nil {
		t.Fatalf("unexpected %s", rec.Body.String())
	}
	if !gamesMap(resp.Result.Games)[[2]string{"D", "B"}] {
		t.Fatalf("fixed game missing: %v", resp.Result.Games)
	}
}

func TestHTTPConstrainedNoPairing(t *testing.T) {
	ps := players("A", "B", "C", "D")
	rec := postConstraints(t, constrainedRequest{
		Players:     ps,
		Constraints: sc(nil, [][2]string{{"A", "B"}, {"A", "C"}, {"A", "D"}}, nil),
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "NO_PAIRING" || resp.Result != nil {
		t.Fatalf("unexpected %s", rec.Body.String())
	}
}

func TestHTTPConstrainedInvalid(t *testing.T) {
	ps := players("A", "B", "C", "D")
	rec := postConstraints(t, constrainedRequest{
		Players:     ps,
		Constraints: sc([]Game{{White: "A", Black: "Z"}}, nil, nil),
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "INVALID_INPUT" {
		t.Fatalf("unexpected %s", rec.Body.String())
	}
}

func TestHTTPConstrainedByeNullVsEmpty(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	// null: unrestricted, canonical bye E.
	rec := postConstraintsRaw(t, map[string]any{
		"players":     ps,
		"constraints": map[string]any{"byeCandidates": nil},
	})
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" || resp.Result.Bye != "E" {
		t.Fatalf("null candidates: %s", rec.Body.String())
	}
	// []: nobody allowed on an odd field => NO_PAIRING.
	rec = postConstraintsRaw(t, map[string]any{
		"players":     ps,
		"constraints": map[string]any{"byeCandidates": []string{}},
	})
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "NO_PAIRING" {
		t.Fatalf("empty candidates: %s", rec.Body.String())
	}
}

func postConstraintsRaw(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/pair/constraints", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	constraintsHandler(rec, req)
	return rec
}

func TestHTTPConstrainedMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/pair/constraints", nil)
	rec := httptest.NewRecorder()
	constraintsHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestHTTPConstrainedUnknownField(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/pair/constraints",
		bytes.NewReader([]byte(`{"players":[],"constraints":{},"extra":1}`)))
	rec := httptest.NewRecorder()
	constraintsHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
