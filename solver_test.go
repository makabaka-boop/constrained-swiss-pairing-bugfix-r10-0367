package main

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// Hand-built cases
// ---------------------------------------------------------------------------

func hist(round int, opp string, c Color) Record {
	return Record{Round: round, Opponent: opp, Color: c}
}

func players(ids ...string) []Player {
	ps := make([]Player, len(ids))
	for i, id := range ids {
		ps[i] = Player{ID: id}
	}
	return ps
}

func TestFourPlayersNoHistoryCanonicalTiebreak(t *testing.T) {
	// Equal scores and no history: every matching has sp=0 and every
	// orientation gives cp=4. The canonical winner is (A white B black,
	// C white D black).
	req := PairRequest{Players: players("D", "C", "B", "A")} // intentionally unsorted
	res, err := Pair(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye != "" {
		t.Fatalf("unexpected bye %q", res.Bye)
	}
	want := []Game{{White: "A", Black: "B"}, {White: "C", Black: "D"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
	if res.ScorePenalty != 0 || res.ColorPenalty != 4 {
		t.Fatalf("penalties = (%d,%d), want (0,4)", res.ScorePenalty, res.ColorPenalty)
	}
}

func TestScorePenaltyDominates(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].Score = 10
	ps[1].Score = 10
	ps[2].Score = 0
	ps[3].Score = 0
	// Best: equal-score pairs, sp=0.
	res, err := Pair(PairRequest{Players: ps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ScorePenalty != 0 {
		t.Fatalf("scorePenalty = %d, want 0", res.ScorePenalty)
	}
	got := map[[2]string]bool{}
	for _, g := range res.Games {
		got[[2]string{g.White, g.Black}] = true
	}
	if !got[[2]string{"A", "B"}] || !got[[2]string{"C", "D"}] {
		t.Fatalf("expected (A,B)+(C,D), got %v", res.Games)
	}
}

func TestRematchForbiddenLeadsToDifferentMatching(t *testing.T) {
	ps := players("A", "B", "C", "D")
	// A already played everyone except C => A must pair C.
	ps[0].History = []Record{hist(1, "B", White), hist(2, "D", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	ps[3].History = []Record{hist(2, "A", Black)}
	res, err := Pair(PairRequest{Players: ps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	found := false
	for _, g := range res.Games {
		if (g.White == "A" && g.Black == "C") || (g.White == "C" && g.Black == "A") {
			found = true
		}
	}
	if !found {
		t.Fatalf("A must pair C, got %v", res.Games)
	}
}

func TestNoUnplayedOpponentReturnsNoPairing(t *testing.T) {
	ps := players("A", "B", "C", "D")
	// A has already met B, C and D.
	ps[0].History = []Record{hist(1, "B", White), hist(2, "C", White), hist(3, "D", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	ps[2].History = []Record{hist(2, "A", Black)}
	ps[3].History = []Record{hist(3, "A", Black)}
	if _, err := Pair(PairRequest{Players: ps}); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

func TestOddNumberOfPlayersExactlyOneBye(t *testing.T) {
	res, err := Pair(PairRequest{Players: players("E", "D", "C", "B", "A")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Games) != 2 {
		t.Fatalf("want 2 games, got %d", len(res.Games))
	}
	// All sp=0/cp tie: the canonical sequence consumes the earliest IDs
	// in games, so the highest ID takes the bye.
	if res.Bye != "E" {
		t.Fatalf("bye = %q, want E", res.Bye)
	}
	want := []Game{{White: "A", Black: "B"}, {White: "C", Black: "D"}}
	if !reflect.DeepEqual(res.Games, want) {
		t.Fatalf("games = %v, want %v", res.Games, want)
	}
	seen := map[string]int{}
	for _, g := range res.Games {
		seen[g.White]++
		seen[g.Black]++
	}
	for _, id := range []string{"A", "B", "C", "D"} {
		if seen[id] != 1 {
			t.Fatalf("player %s appears %d times", id, seen[id])
		}
	}
	if seen[res.Bye] != 0 {
		t.Fatalf("bye player %s also plays", res.Bye)
	}
}

func TestPriorByePlayerCannotByeAgain(t *testing.T) {
	// 5 players; A already had the bye in round 1.
	ps := players("A", "B", "C", "D", "E")
	ps[0].Byes = []int{1}
	// Also force all of B,C,D to have played each other? Instead: make A
	// unable to play any of B,C,D without... simpler: block A vs B,C,D,E
	// is impossible with 5 (A needs to play). Instead check directly that
	// the chosen bye is never A across many random shapes — here just run
	// and assert bye != A.
	res, err := Pair(PairRequest{Players: ps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Bye == "A" {
		t.Fatalf("player with prior bye got another bye")
	}
}

func TestAllPlayersAlreadyHadBye(t *testing.T) {
	ps := players("A", "B", "C", "D", "E")
	for i := range ps {
		ps[i].Byes = []int{1}
	}
	if _, err := Pair(PairRequest{Players: ps}); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

func TestColorImbalanceForcesOrientation(t *testing.T) {
	ps := players("A", "B", "C", "D")
	// A has played black twice more than white: diff = -2, so A is forced
	// to white (black would reach -3).
	ps[0].History = []Record{
		hist(1, "C", Black), hist(2, "D", Black),
	}
	ps[2].History = []Record{hist(1, "A", White)}
	ps[3].History = []Record{hist(2, "A", White)}
	res, err := Pair(PairRequest{Players: ps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, g := range res.Games {
		if g.Black == "A" {
			t.Fatalf("A was forced white but got black: %v", res.Games)
		}
	}
}

func TestUnrecoverableColorImbalance(t *testing.T) {
	ps := players("A", "B", "C", "D")
	// diff(A) = +4: one more round yields 3 or 5 (or 4 on bye for odd n) —
	// never within 2.
	recsA := []Record{
		hist(1, "B", White), hist(2, "C", White),
		hist(3, "D", White), hist(4, "B", White),
	}
	ps[0].History = recsA
	ps[1].History = []Record{hist(1, "A", Black), hist(4, "A", Black)}
	ps[2].History = []Record{hist(2, "A", Black)}
	ps[3].History = []Record{hist(3, "A", Black)}
	if _, err := Pair(PairRequest{Players: ps}); err != ErrNoPairing {
		t.Fatalf("err = %v, want ErrNoPairing", err)
	}
}

// ---------------------------------------------------------------------------
// Validation rejection cases
// ---------------------------------------------------------------------------

func TestRejectDuplicateAppearanceInOneRound(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White), hist(1, "C", White)}
	ps[1].History = []Record{hist(1, "A", Black)}
	ps[2].History = []Record{hist(1, "A", Black)}
	if _, err := Pair(PairRequest{Players: ps}); !IsValidation(err) {
		t.Fatalf("err = %v, want validation error", err)
	}
}

func TestRejectConflictingColors(t *testing.T) {
	ps := players("A", "B", "C", "D")
	// Both claim white in round 1.
	ps[0].History = []Record{hist(1, "B", White)}
	ps[1].History = []Record{hist(1, "A", White)}
	if _, err := Pair(PairRequest{Players: ps}); !IsValidation(err) {
		t.Fatalf("err = %v, want validation error", err)
	}
}

func TestRejectConflictingOpponent(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White)}
	ps[1].History = []Record{hist(1, "C", Black)}
	ps[2].History = []Record{hist(1, "B", White)}
	if _, err := Pair(PairRequest{Players: ps}); !IsValidation(err) {
		t.Fatalf("err = %v, want validation error", err)
	}
}

func TestRejectMissingReciprocal(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White)}
	if _, err := Pair(PairRequest{Players: ps}); !IsValidation(err) {
		t.Fatalf("err = %v, want validation error", err)
	}
}

func TestRejectBadCountAndDuplicateID(t *testing.T) {
	if _, err := Pair(PairRequest{Players: players("A", "B", "C")}); !IsValidation(err) {
		t.Fatal("3 players must be rejected")
	}
	thirteen := make([]Player, 13)
	for i := range thirteen {
		thirteen[i].ID = "p" + string(rune('a'+i))
	}
	if _, err := Pair(PairRequest{Players: thirteen}); !IsValidation(err) {
		t.Fatal("13 players must be rejected")
	}
	if _, err := Pair(PairRequest{Players: players("A", "A", "B", "C")}); !IsValidation(err) {
		t.Fatal("duplicate IDs must be rejected")
	}
}

func TestRejectPlayAndByeSameRound(t *testing.T) {
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "B", White)}
	ps[0].Byes = []int{1}
	ps[1].History = []Record{hist(1, "A", Black)}
	if _, err := Pair(PairRequest{Players: ps}); !IsValidation(err) {
		t.Fatal("playing and bye in same round must be rejected")
	}
}

// ---------------------------------------------------------------------------
// Result sanity checks shared with the fuzz tests
// ---------------------------------------------------------------------------

func assertValidSchedule(t *testing.T, req PairRequest, res *PairResult) {
	t.Helper()
	ps := req.Players
	byID := map[string]Player{}
	for _, p := range ps {
		byID[p.ID] = p
	}
	n := len(ps)
	if n%2 == 0 {
		if res.Bye != "" {
			t.Fatalf("even field but bye = %q", res.Bye)
		}
	} else {
		if res.Bye == "" {
			t.Fatal("odd field but no bye")
		}
		b := byID[res.Bye]
		if len(b.Byes) > 0 {
			t.Fatalf("repeat bye for %q", res.Bye)
		}
	}
	if len(res.Games) != n/2 {
		t.Fatalf("got %d games, want %d", len(res.Games), n/2)
	}
	appears := map[string]int{}
	whites, blacks := map[string]int{}, map[string]int{}
	for _, g := range res.Games {
		w, ok1 := byID[g.White]
		b, ok2 := byID[g.Black]
		if !ok1 || !ok2 {
			t.Fatalf("unknown player in game %v", g)
		}
		if w.ID == b.ID {
			t.Fatalf("self pairing %v", g)
		}
		appears[w.ID]++
		appears[b.ID]++
		whites[w.ID]++
		blacks[b.ID]++
		for _, h := range w.History {
			if h.Opponent == b.ID {
				t.Fatalf("rematch %q vs %q", w.ID, b.ID)
			}
		}
	}
	for _, p := range ps {
		want := 1
		if p.ID == res.Bye {
			want = 0
		}
		if appears[p.ID] != want {
			t.Fatalf("player %q appears %d times, want %d", p.ID, appears[p.ID], want)
		}
		// Post-round color imbalance.
		diff := 0
		for _, h := range p.History {
			if h.Color == White {
				diff++
			} else {
				diff--
			}
		}
		diff += whites[p.ID] - blacks[p.ID]
		if absInt(diff) > 2 {
			t.Fatalf("player %q post imbalance %d exceeds 2", p.ID, diff)
		}
	}
	// Games must be ordered by the smaller player ID.
	for i := 1; i < len(res.Games); i++ {
		prev := minString(res.Games[i-1].White, res.Games[i-1].Black)
		cur := minString(res.Games[i].White, res.Games[i].Black)
		if prev >= cur {
			t.Fatalf("games not ordered by smaller ID: %q before %q", prev, cur)
		}
	}
}

func minString(a, b string) string {
	if a < b {
		return a
	}
	return b
}

// ---------------------------------------------------------------------------
// Independent brute-force oracle and differential fuzzing
// ---------------------------------------------------------------------------

// oracleResult is the independently computed optimum.
type oracleResult struct {
	ok     bool
	sp, cp int
	key    []int // rank-encoded canonical sequence, bye (or -1) last
	games  []Game
	bye    string
}

// bruteForce re-derives the optimum without using the production solver:
// it enumerates every bye, every perfect matching and every orientation.
func bruteForce(t *testing.T, req PairRequest) oracleResult {
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
				// Enumerate all orientations.
				post := make([]int, n)
				if bye >= 0 {
					post[bye] = diff[bye]
				}
				var orient func(int, int, int, []int, []Game)
				orient = func(k, spAcc, cpAcc int, key []int, games []Game) {
					if k == len(pairs) {
						// The bye player does not play; their imbalance is
						// unchanged and must also be within 2.
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
					// lo white
					if dl, dh := diff[lo]+1, diff[hi]-1; absInt(dl) <= 2 && absInt(dh) <= 2 {
						post[lo], post[hi] = dl, dh
						orient(k+1, spAcc+spPair, cpAcc+absInt(dl)+absInt(dh),
							append(append([]int(nil), key...), lo, hi),
							append(append([]Game(nil), games...), Game{White: ids[lo], Black: ids[hi]}))
					}
					// hi white
					if dl, dh := diff[lo]-1, diff[hi]+1; absInt(dl) <= 2 && absInt(dh) <= 2 {
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
			bits := rem
			for bits != 0 {
				jb := bits & -bits
				j := bitIndex(jb)
				bits ^= jb
				if played[i][j] {
					continue
				}
				pairs = append(pairs, [2]int{i, j})
				enumerateMatch(rem ^ jb)
				pairs = pairs[:len(pairs)-1]
			}
		}
		enumerateMatch(mask)
	}

	if n%2 == 0 {
		considerBye(-1)
	} else {
		for b := 0; b < n; b++ {
			if !hadBye[b] {
				considerBye(b)
			}
		}
	}
	return best
}

func bitIndex(m int) int {
	for i := 0; m != 0; i++ {
		if m&1 != 0 {
			return i
		}
		m >>= 1
	}
	return -1
}

func tupleLess(sp1, cp1 int, k1 []int, sp2, cp2 int, k2 []int) bool {
	if sp1 != sp2 {
		return sp1 < sp2
	}
	if cp1 != cp2 {
		return cp1 < cp2
	}
	return lexLess(k1, k2)
}

// encodeSchedule converts a production result into the oracle key form.
func encodeSchedule(t *testing.T, req PairRequest, res *PairResult) []int {
	t.Helper()
	ids := make([]string, len(req.Players))
	for i, p := range req.Players {
		ids[i] = p.ID
	}
	sort.Strings(ids)
	rank := map[string]int{}
	for i, id := range ids {
		rank[id] = i
	}
	key := make([]int, 0, len(res.Games)*2+1)
	for _, g := range res.Games {
		key = append(key, rank[g.White], rank[g.Black])
	}
	if res.Bye == "" {
		key = append(key, -1)
	} else {
		key = append(key, rank[res.Bye])
	}
	return key
}

// genRandomInstance builds a mutually-consistent random tournament history.
func genRandomInstance(rng *rand.Rand) PairRequest {
	n := 4 + rng.Intn(5) // 4..8
	ids := make([]string, n)
	for i := range ids {
		ids[i] = "P" + format2(i)
	}
	ps := make([]Player, n)
	for i := range ps {
		ps[i] = Player{ID: ids[i], Score: rng.Intn(12)}
	}
	rounds := 1 + rng.Intn(3)
	for r := 1; r <= rounds; r++ {
		// Random partial matching this round.
		perm := rng.Perm(n)
		paired := make([]bool, n)
		for k := 0; k+1 < n; k++ {
			a, b := perm[k], perm[k+1]
			if rng.Intn(2) == 0 || paired[a] || paired[b] {
				continue
			}
			// Avoid duplicate rematches across rounds.
			already := false
			for _, h := range ps[a].History {
				if h.Opponent == ids[b] {
					already = true
					break
				}
			}
			if already {
				continue
			}
			ca, cb := White, Black
			if rng.Intn(2) == 0 {
				ca, cb = Black, White
			}
			ps[a].History = append(ps[a].History, hist(r, ids[b], ca))
			ps[b].History = append(ps[b].History, hist(r, ids[a], cb))
			paired[a], paired[b] = true, true
			k++ // skip b's slot
		}
		// Random byes among players idle this round.
		for i := 0; i < n; i++ {
			if !paired[i] && rng.Intn(3) == 0 && len(ps[i].Byes) == 0 {
				ps[i].Byes = append(ps[i].Byes, r)
			}
		}
	}
	// Shuffle input order so ID-based tie-breaking is exercised.
	rng.Shuffle(n, func(i, j int) { ps[i], ps[j] = ps[j], ps[i] })
	return PairRequest{Players: ps}
}

func format2(i int) string {
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

func TestFuzzAgainstBruteForceOracle(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	rng := rand.New(rand.NewSource(20261001))
	const iterations = 3000
	for it := 0; it < iterations; it++ {
		req := genRandomInstance(rng)
		oracle := bruteForce(t, req)
		res, err := Pair(req)
		if !oracle.ok {
			if err != ErrNoPairing {
				t.Fatalf("case %d: oracle says no pairing, service gave %v / %v", it, res, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: unexpected error %v; oracle ok sp=%d cp=%d key=%v", it, err, oracle.sp, oracle.cp, oracle.key)
		}
		assertValidSchedule(t, req, res)
		if res.ScorePenalty != oracle.sp {
			t.Fatalf("case %d: sp=%d want %d (%v)", it, res.ScorePenalty, oracle.sp, describe(req))
		}
		if res.ColorPenalty != oracle.cp {
			t.Fatalf("case %d: cp=%d want %d (%v)", it, res.ColorPenalty, oracle.cp, describe(req))
		}
		gotKey := encodeSchedule(t, req, res)
		if !reflect.DeepEqual(gotKey, oracle.key) {
			t.Fatalf("case %d: key=%v want %v (%v)", it, gotKey, oracle.key, describe(req))
		}
		if !reflect.DeepEqual(res.Games, oracle.games) || res.Bye != oracle.bye {
			t.Fatalf("case %d: schedule %v/%q want %v/%q", it, res.Games, res.Bye, oracle.games, oracle.bye)
		}
	}
}

func describe(req PairRequest) string {
	out := ""
	for _, p := range req.Players {
		out += p.ID
	}
	return out
}
