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
// Forbidden pairs
// ---------------------------------------------------------------------------

func TestForbiddenFindsAlternativeMatching(t *testing.T) {
	// Equal scores, no history: unconstrained canonical winner is A-B, C-D.
	// Forbid A-B and the solver must still find the next canonical
	// schedule (A-C, B-D), not report NO_PAIRING.
	req := constrainedRequest{
		Players: players("A", "B", "C", "D"),
		Constraints: scheduleConstraints{
			Forbidden: [][2]string{{"A", "B"}},
		},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Game{{White: "A", Black: "C"}, {White: "B", Black: "D"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
}

func TestForbiddenChangesScoreOptimum(t *testing.T) {
	// A,B score 0; C,D score 10. The cheap matching (sp=0) is AB + CD.
	// Forbidding both forces the cross matching with sp=20; the global
	// score objective must still be honored over the restricted space.
	ps := players("A", "B", "C", "D")
	ps[0].Score, ps[1].Score = 0, 0
	ps[2].Score, ps[3].Score = 10, 10
	req := constrainedRequest{
		Players: ps,
		Constraints: scheduleConstraints{
			Forbidden: [][2]string{{"A", "B"}, {"C", "D"}},
		},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ScorePenalty != 20 {
		t.Fatalf("scorePenalty = %d, want 20; games %v", res.ScorePenalty, res.Games)
	}
}

func TestForbiddenUndirected(t *testing.T) {
	// Listing the pair backwards must block the meeting just the same.
	req := constrainedRequest{
		Players: players("A", "B", "C", "D"),
		Constraints: scheduleConstraints{
			Forbidden: [][2]string{{"B", "A"}},
		},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, g := range res.Games {
		if (g.White == "A" || g.Black == "A") && (g.White == "B" || g.Black == "B") {
			t.Fatalf("forbidden undirected pair A/B was paired: %v", res.Games)
		}
	}
}

func TestForbiddenLeavesNoOpponent(t *testing.T) {
	// 4 players, forbid every opponent of A => no complete schedule.
	req := constrainedRequest{
		Players: players("A", "B", "C", "D"),
		Constraints: scheduleConstraints{
			Forbidden: [][2]string{{"A", "B"}, {"A", "C"}, {"A", "D"}},
		},
	}
	if _, err := constrainedPair(req); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

// ---------------------------------------------------------------------------
// Fixed games
// ---------------------------------------------------------------------------

func TestFixedGameForcesMatching(t *testing.T) {
	// A,C score 10; B,D score 0. Unconstrained optimum is AC + BD (sp 0).
	// Fixing A-B forces AB + CD even though it costs score penalty.
	ps := players("A", "B", "C", "D")
	ps[0].Score, ps[2].Score = 10, 10
	req := constrainedRequest{
		Players: ps,
		Constraints: scheduleConstraints{
			Fixed: []Game{{White: "A", Black: "B"}},
		},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []Game{{White: "A", Black: "B"}, {White: "C", Black: "D"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
	if res.ScorePenalty != 20 {
		t.Fatalf("scorePenalty = %d, want 20", res.ScorePenalty)
	}
}

func TestFixedGamePinsColorOrientation(t *testing.T) {
	// Fix the reverse of the canonical orientation: B plays white vs A.
	// The result must carry exactly that color assignment.
	req := constrainedRequest{
		Players: players("A", "B", "C", "D"),
		Constraints: scheduleConstraints{
			Fixed: []Game{{White: "B", Black: "A"}},
		},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, g := range res.Games {
		if g.White == "B" && g.Black == "A" {
			found = true
		}
		if g.White == "A" && g.Black == "B" {
			t.Fatalf("fixed color B-white/A-black violated: %v", res.Games)
		}
	}
	if !found {
		t.Fatalf("fixed game B-white/A-black missing: %v", res.Games)
	}
}

func TestFixedColorConflictWithHistoryIsNoPairing(t *testing.T) {
	// A already played white twice (diff = +2), so A must play black next.
	// Fixing A-white vs B is in conflict with the color limit: the fixed
	// pair cannot be realized, and the conflict must not be accepted.
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "C", White), hist(2, "D", White)}
	ps[2].History = []Record{hist(1, "A", Black)}
	ps[3].History = []Record{hist(2, "A", Black)}
	req := constrainedRequest{
		Players: ps,
		Constraints: scheduleConstraints{
			Fixed: []Game{{White: "A", Black: "B"}},
		},
	}
	if _, err := constrainedPair(req); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
	// The reverse fixed color is feasible and must be accepted.
	req.Constraints.Fixed = []Game{{White: "B", Black: "A"}}
	if _, err := constrainedPair(req); err != nil {
		t.Fatalf("feasible reversed color rejected: %v", err)
	}
}

func TestFixedRematchIsNoPairing(t *testing.T) {
	// A-B already met; fixing their rematch is structurally fine but
	// infeasible against the no-rematch rule => NO_PAIRING.
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	req := constrainedRequest{
		Players: ps,
		Constraints: scheduleConstraints{
			Fixed: []Game{{White: "A", Black: "B"}},
		},
	}
	if _, err := constrainedPair(req); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

func TestFixedPlayerNeverTakesBye(t *testing.T) {
	// 5 players; B-C fixed and the bye list contains B. B must play;
	// the bye falls to the other listed candidate E.
	nil2 := func(ids ...string) *[]string { return &ids }
	req := constrainedRequest{
		Players: players("A", "B", "C", "D", "E"),
		Constraints: scheduleConstraints{
			Fixed:         []Game{{White: "B", Black: "C"}},
			ByeCandidates: nil2("B", "E"),
		},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye != "E" {
		t.Fatalf("bye = %q, want E (fixed player B cannot bye)", res.Bye)
	}
	seen := map[string]bool{}
	for _, g := range res.Games {
		seen[g.White], seen[g.Black] = true, true
	}
	if !seen["B"] || !seen["C"] {
		t.Fatalf("fixed game missing from %v", res.Games)
	}
}

// ---------------------------------------------------------------------------
// Bye candidates: absent / null / empty list have distinct meanings
// ---------------------------------------------------------------------------

func TestByeCandidatesRestrictsOddBye(t *testing.T) {
	// 5 equal players: canonical unrestricted bye is E. Restricting the
	// list to A forces the bye onto A instead.
	listA := []string{"A"}
	req := constrainedRequest{
		Players:     players("A", "B", "C", "D", "E"),
		Constraints: scheduleConstraints{ByeCandidates: &listA},
	}
	res, err := constrainedPair(req)
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

func TestEmptyByeCandidatesMeansNobody(t *testing.T) {
	empty := []string{}
	req := constrainedRequest{
		Players:     players("A", "B", "C", "D", "E"),
		Constraints: scheduleConstraints{ByeCandidates: &empty},
	}
	if _, err := constrainedPair(req); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing for empty bye list", err)
	}
}

func TestNilByeCandidatesMeansAnybody(t *testing.T) {
	// Absent (nil) bye list keeps the unrestricted behavior: bye E.
	for _, c := range []scheduleConstraints{
		{},
		{ByeCandidates: nil},
	} {
		req := constrainedRequest{Players: players("A", "B", "C", "D", "E"), Constraints: c}
		res, err := constrainedPair(req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Bye != "E" {
			t.Fatalf("bye = %q, want E for unrestricted list", res.Bye)
		}
	}
}

func TestByeCandidatesIntersectPriorByes(t *testing.T) {
	// Only A may bye, but A already had a bye => intersection empty.
	ps := players("A", "B", "C", "D", "E")
	ps[0].Byes = []int{1}
	listA := []string{"A"}
	req := constrainedRequest{
		Players:     ps,
		Constraints: scheduleConstraints{ByeCandidates: &listA},
	}
	if _, err := constrainedPair(req); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

func TestEmptyByeCandidatesEvenFieldStillPairs(t *testing.T) {
	// Even fields have no bye at all; an empty list must not block pairing.
	empty := []string{}
	req := constrainedRequest{
		Players:     players("A", "B", "C", "D"),
		Constraints: scheduleConstraints{ByeCandidates: &empty},
	}
	res, err := constrainedPair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye != "" || len(res.Games) != 2 {
		t.Fatalf("unexpected result %+v", res)
	}
}

// ---------------------------------------------------------------------------
// Structurally invalid constraints => INVALID_INPUT (never NO_PAIRING)
// ---------------------------------------------------------------------------

func TestInvalidConstraints(t *testing.T) {
	base := func() []Player { return players("A", "B", "C", "D") }
	cases := []struct {
		name string
		c    scheduleConstraints
	}{
		{"fixed unknown white", scheduleConstraints{Fixed: []Game{{White: "Z", Black: "A"}}}},
		{"fixed unknown black", scheduleConstraints{Fixed: []Game{{White: "A", Black: "Z"}}}},
		{"fixed self pair", scheduleConstraints{Fixed: []Game{{White: "A", Black: "A"}}}},
		{"fixed reuses player", scheduleConstraints{Fixed: []Game{
			{White: "A", Black: "B"}, {White: "A", Black: "C"},
		}}},
		{"fixed pair listed twice", scheduleConstraints{Fixed: []Game{
			{White: "A", Black: "B"}, {White: "B", Black: "A"},
		}}},
		{"forbidden unknown", scheduleConstraints{Forbidden: [][2]string{{"A", "Z"}}}},
		{"forbidden self", scheduleConstraints{Forbidden: [][2]string{{"A", "A"}}}},
		{"forbidden duplicate", scheduleConstraints{Forbidden: [][2]string{{"A", "B"}, {"B", "A"}}}},
		{"fixed and forbidden clash", scheduleConstraints{
			Fixed:     []Game{{White: "A", Black: "B"}},
			Forbidden: [][2]string{{"B", "A"}},
		}},
		{"unknown bye candidate", scheduleConstraints{
			ByeCandidates: &[]string{"Z"},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := constrainedPair(constrainedRequest{Players: base(), Constraints: tc.c})
			if !IsValidation(err) {
				t.Fatalf("err = %v, want validation error", err)
			}
		})
	}
}

func TestConstrainedScoreBound(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].Score = maxConstrainedScore + 1
	if _, err := constrainedPair(constrainedRequest{Players: ps}); !IsValidation(err) {
		t.Fatalf("score over bound: err = %v, want validation error", err)
	}
	ps[0].Score = maxConstrainedScore
	if _, err := constrainedPair(constrainedRequest{Players: ps}); err != nil {
		t.Fatalf("score at bound rejected: %v", err)
	}
	// The original endpoint keeps accepting scores above the new bound.
	ps[0].Score = maxConstrainedScore * 2
	if _, err := Pair(PairRequest{Players: ps}); err != nil {
		t.Fatalf("original /pair must keep accepting large scores, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Empty / null constraints are behaviorally identical to the old endpoint
// ---------------------------------------------------------------------------

func TestNoConstraintsMatchesOriginalEndpoint(t *testing.T) {
	rng := rand.New(rand.NewSource(777))
	for it := 0; it < 50; it++ {
		req := genRandomInstance(rng)
		orig, errO := Pair(req)
		for _, c := range []scheduleConstraints{{}, {Fixed: nil, Forbidden: nil, ByeCandidates: nil}} {
			cr := constrainedRequest{Players: req.Players, Constraints: c}
			got, errG := constrainedPair(cr)
			if (errO == nil) != (errG == nil) ||
				(errO != nil && errO.Error() != errG.Error()) {
				t.Fatalf("case %d: errors diverge %v vs %v", it, errO, errG)
			}
			if errO == nil && !reflect.DeepEqual(got, orig) {
				t.Fatalf("case %d: %+v != original %+v", it, got, orig)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// HTTP regression for POST /pair/constraints
// ---------------------------------------------------------------------------

func postConstraints(t *testing.T, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/pair/constraints", bytes.NewReader(buf))
	rec := httptest.NewRecorder()
	constraintsHandler(rec, req)
	return rec
}

func TestHTTPConstraintsOK(t *testing.T) {
	rec := postConstraints(t, map[string]any{
		"players": []map[string]any{
			{"id": "A"}, {"id": "B"}, {"id": "C"}, {"id": "D"},
		},
		"constraints": map[string]any{
			"fixed": []map[string]string{{"white": "B", "black": "A"}},
		},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "OK" || resp.Result == nil {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
	found := false
	for _, g := range resp.Result.Games {
		if g.White == "B" && g.Black == "A" {
			found = true
		}
	}
	if !found {
		t.Fatalf("fixed game missing: %v", resp.Result.Games)
	}
}

func TestHTTPConstraintsNoPairing(t *testing.T) {
	// Odd field (5) with an explicit empty bye list => no legal schedule,
	// but the input itself is perfectly legal => 200 NO_PAIRING.
	rec := postConstraints(t, map[string]any{
		"players": []map[string]any{
			{"id": "A"}, {"id": "B"}, {"id": "C"}, {"id": "D"}, {"id": "E"},
		},
		"constraints": map[string]any{"byeCandidates": []string{}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "NO_PAIRING" || resp.Result != nil {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestHTTPConstraintsInvalidInput(t *testing.T) {
	rec := postConstraints(t, map[string]any{
		"players": []map[string]any{
			{"id": "A"}, {"id": "B"}, {"id": "C"}, {"id": "D"},
		},
		"constraints": map[string]any{
			"fixed": []map[string]string{{"white": "A", "black": "Z"}},
		},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	var resp errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Status != "INVALID_INPUT" {
		t.Fatalf("unexpected body %s", rec.Body.String())
	}
}

func TestHTTPConstraintsNullEqualsAbsent(t *testing.T) {
	body := map[string]any{
		"players": []map[string]any{
			{"id": "A"}, {"id": "B"}, {"id": "C"}, {"id": "D"},
		},
		"constraints": nil,
	}
	rec := postConstraints(t, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	var withNull PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &withNull); err != nil {
		t.Fatal(err)
	}
	plain := postPair(t, PairRequest{Players: players("A", "B", "C", "D")})
	var orig PairResponse
	if err := json.Unmarshal(plain.Body.Bytes(), &orig); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(withNull, orig) {
		t.Fatalf("null constraints %+v != original %+v", withNull, orig)
	}
}

func TestHTTPConstraintsMethodNotAllowed(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/pair/constraints", nil)
	rec := httptest.NewRecorder()
	constraintsHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// Independent brute-force oracle for constrained requests
// ---------------------------------------------------------------------------

type constrainedSpec struct {
	fixed         []Game
	forbidden     [][2]string
	byeCandidates *[]string // nil pointer means unrestricted
}

func bruteForceConstrained(t *testing.T, req PairRequest, spec constrainedSpec) oracleResult {
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
	diff := make([]int, n)
	hadBye := make([]bool, n)
	for i := range played {
		played[i] = make([]bool, n)
	}
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

	fixedMate := make([]int, n)
	for i := range fixedMate {
		fixedMate[i] = -1
	}
	fixedOrient := map[[2]int]int{}
	for _, g := range spec.fixed {
		wi, bi := rank[g.White], rank[g.Black]
		fixedMate[wi] = bi
		fixedMate[bi] = wi
		lo, hi := wi, bi
		if lo > hi {
			lo, hi = hi, lo
		}
		o := 0
		if wi == hi {
			o = 1
		}
		fixedOrient[[2]int{lo, hi}] = o
	}
	forbid := make([][]bool, n)
	for i := range forbid {
		forbid[i] = make([]bool, n)
	}
	for _, f := range spec.forbidden {
		i, j := rank[f[0]], rank[f[1]]
		forbid[i][j] = true
		forbid[j][i] = true
	}
	var byeOK map[int]bool
	if spec.byeCandidates != nil {
		byeOK = map[int]bool{}
		for _, id := range *spec.byeCandidates {
			byeOK[rank[id]] = true
		}
	}

	best := oracleResult{ok: false}
	all := (1 << n) - 1

	considerBye := func(bye int) {
		mask := all
		if bye >= 0 {
			mask ^= 1 << bye
		}
		var pairs [][2]int
		var enumerateMatch func(int)
		enumerateMatch = func(m int) {
			if m == 0 {
				post := make([]int, n)
				if bye >= 0 {
					post[bye] = diff[bye]
				}
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
						cand := oracleResult{
							ok:    true,
							sp:    spAcc,
							cp:    cpAcc,
							key:   fullKey,
							games: append([]Game(nil), games...),
							bye: func() string {
								if bye < 0 {
									return ""
								}
								return ids[bye]
							}(),
						}
						if !best.ok || tupleLess(cand.sp, cand.cp, cand.key, best.sp, best.cp, best.key) {
							best = cand
						}
						return
					}
					lo, hi := pairs[k][0], pairs[k][1]
					spPair := absInt(score[ids[lo]] - score[ids[hi]])
					allowed := func(o int) bool {
						if want, pinned := fixedOrient[[2]int{lo, hi}]; pinned && want != o {
							return false
						}
						if o == 0 {
							return absInt(diff[lo]+1) <= 2 && absInt(diff[hi]-1) <= 2
						}
						return absInt(diff[lo]-1) <= 2 && absInt(diff[hi]+1) <= 2
					}
					if allowed(0) {
						dl, dh := diff[lo]+1, diff[hi]-1
						post[lo], post[hi] = dl, dh
						orient(k+1, spAcc+spPair, cpAcc+absInt(dl)+absInt(dh),
							append(append([]int(nil), key...), lo, hi),
							append(append([]Game(nil), games...), Game{White: ids[lo], Black: ids[hi]}))
					}
					if allowed(1) {
						dl, dh := diff[lo]-1, diff[hi]+1
						post[lo], post[hi] = dl, dh
						orient(k+1, spAcc+spPair, cpAcc+absInt(dl)+absInt(dh),
							append(append([]int(nil), key...), hi, lo),
							append(append([]Game(nil), games...), Game{White: ids[hi], Black: ids[lo]}))
					}
				}
				orient(0, 0, 0, nil, nil)
				return
			}
			i := bitIndex(m)
			rem := m ^ (1 << i)
			tryPair := func(j int) {
				jb := 1 << j
				pairs = append(pairs, [2]int{i, j})
				enumerateMatch(rem ^ jb)
				pairs = pairs[:len(pairs)-1]
			}
			if fixedMate[i] >= 0 {
				j := fixedMate[i]
				jb := 1 << j
				if rem&jb != 0 && !played[i][j] && !forbid[i][j] {
					tryPair(j)
				}
				return
			}
			bits := rem
			for bits != 0 {
				jb := bits & -bits
				j := bitIndex(jb)
				bits ^= jb
				if played[i][j] || forbid[i][j] || fixedMate[j] >= 0 {
					continue
				}
				tryPair(j)
			}
		}
		enumerateMatch(mask)
	}

	if n%2 == 0 {
		considerBye(-1)
	} else {
		for b := 0; b < n; b++ {
			if hadBye[b] || fixedMate[b] >= 0 {
				continue
			}
			if byeOK != nil && !byeOK[b] {
				continue
			}
			considerBye(b)
		}
	}
	return best
}

// genConstrainedInstance builds a structurally valid random constraint
// set on top of a random valid request.
func genConstrainedInstance(rng *rand.Rand) (PairRequest, constrainedSpec) {
	req := genRandomInstance(rng)
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
	n := len(ids)

	// Fixed matching: shuffled unused players, pair when they have not met.
	perm := rng.Perm(n)
	used := make([]bool, n)
	var fixed []Game
	fixedSet := map[[2]int]bool{}
	for k := 0; k+1 < n; k++ {
		a, b := perm[k], perm[k+1]
		if used[a] || used[b] {
			continue
		}
		ida, idb := ids[a], ids[b]
		lo, hi := ida, idb
		if lo > hi {
			lo, hi = hi, lo
		}
		ra, rb := a, b
		if ra > rb {
			ra, rb = rb, ra
		}
		if played[[2]string{lo, hi}] || fixedSet[[2]int{ra, rb}] {
			continue
		}
		if rng.Intn(2) == 0 {
			fixed = append(fixed, Game{White: ida, Black: idb})
		} else {
			fixed = append(fixed, Game{White: idb, Black: ida})
		}
		fixedSet[[2]int{ra, rb}] = true
		used[a], used[b] = true, true
		k++
	}

	// Forbidden pairs: random undirected pairs that are not fixed.
	var allPairs [][2]int
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			allPairs = append(allPairs, [2]int{i, j})
		}
	}
	rng.Shuffle(len(allPairs), func(i, j int) { allPairs[i], allPairs[j] = allPairs[j], allPairs[i] })
	var forbidden [][2]string
	for _, p := range allPairs {
		if fixedSet[p] {
			continue
		}
		if rng.Intn(3) == 0 {
			if rng.Intn(2) == 0 {
				forbidden = append(forbidden, [2]string{ids[p[0]], ids[p[1]]})
			} else {
				forbidden = append(forbidden, [2]string{ids[p[1]], ids[p[0]]})
			}
		}
	}

	spec := constrainedSpec{fixed: fixed, forbidden: forbidden}
	if n%2 == 1 {
		switch rng.Intn(3) {
		case 0:
			// unrestricted
		case 1:
			empty := []string{}
			spec.byeCandidates = &empty
		case 2:
			var list []string
			for _, id := range ids {
				if rng.Intn(2) == 0 {
					list = append(list, id)
				}
			}
			spec.byeCandidates = &list
		}
	}
	return req, spec
}

func TestFuzzConstrainedAgainstOracle(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	rng := rand.New(rand.NewSource(20261006))
	const iterations = 1500
	for it := 0; it < iterations; it++ {
		req, spec := genConstrainedInstance(rng)
		oracle := bruteForceConstrained(t, req, spec)
		res, err := constrainedPair(constrainedRequest{
			Players: req.Players,
			Constraints: scheduleConstraints{
				Fixed:         spec.fixed,
				Forbidden:     spec.forbidden,
				ByeCandidates: spec.byeCandidates,
			},
		})
		if !oracle.ok {
			if err != ErrNoPairing {
				t.Fatalf("case %d: oracle says no pairing, got %v / %v", it, res, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: unexpected error %v; oracle sp=%d cp=%d", it, err, oracle.sp, oracle.cp)
		}
		if res.ScorePenalty != oracle.sp || res.ColorPenalty != oracle.cp {
			t.Fatalf("case %d: penalties (%d,%d) want (%d,%d)",
				it, res.ScorePenalty, res.ColorPenalty, oracle.sp, oracle.cp)
		}
		if got := encodeSchedule(t, req, res); !reflect.DeepEqual(got, oracle.key) {
			t.Fatalf("case %d: key %v want %v", it, got, oracle.key)
		}
		if !reflect.DeepEqual(res.Games, oracle.games) || res.Bye != oracle.bye {
			t.Fatalf("case %d: %v/%q want %v/%q", it, res.Games, res.Bye, oracle.games, oracle.bye)
		}
		for _, g := range spec.fixed {
			if !containsGame(res.Games, g) {
				t.Fatalf("case %d: fixed game %v missing from %v", it, g, res.Games)
			}
		}
	}
}

func containsGame(games []Game, want Game) bool {
	for _, g := range games {
		if g == want {
			return true
		}
	}
	return false
}
