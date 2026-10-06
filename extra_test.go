package main

import (
	"math/rand"
	"reflect"
	"testing"
)

func TestMaxSizeTwelvePlayers(t *testing.T) {
	// 12 players, no history: exercises the largest matching space.
	ps := make([]Player, 12)
	for i := range ps {
		ps[i] = Player{ID: "P" + format2(i), Score: i % 3 * 2}
	}
	res, err := Pair(PairRequest{Players: ps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res.Games) != 6 {
		t.Fatalf("want 6 games, got %d", len(res.Games))
	}
	assertValidSchedule(t, PairRequest{Players: ps}, res)
}

func TestColorPenaltyBreaksScoreTie(t *testing.T) {
	// After round 1: A(+1)-C(-1), B(+1)-D(-1). Rematch rules forbid A-C
	// and B-D next round. Of the two remaining matchings, A-D + B-C lets
	// each +1 player take black and each -1 player take white, so every
	// post-round imbalance is 0 (cp = 0); pairing A-B + C-D costs more.
	ps := players("A", "B", "C", "D")
	ps[0].History = []Record{hist(1, "C", White)}
	ps[1].History = []Record{hist(1, "D", White)}
	ps[2].History = []Record{hist(1, "A", Black)}
	ps[3].History = []Record{hist(1, "B", Black)}
	// Rematch rules forbid A-C and B-D in the next round, so the only
	// matchings are A-B/C-D or A-D/B-C. Both have identical structure w.r.t.
	// color diffs (A:+1,B:+1,C:-1,D:-1):
	//   A-D: A black -> 0, D white -> 0 (cost 0); B-C: B black ->0, C white->0
	// gives cp 0. A-B pairing: A black->0,B white->2 (cost 2) etc -> worse.
	// Expect the cross matching (A-D, B-C) with every player ending at 0.
	res, err := Pair(PairRequest{Players: ps})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ColorPenalty != 0 {
		t.Fatalf("colorPenalty = %d, want 0; games=%v", res.ColorPenalty, res.Games)
	}
	got := map[[2]string]bool{}
	for _, g := range res.Games {
		got[[2]string{g.White, g.Black}] = true
	}
	// A and B must both take black, C and D white.
	if !got[[2]string{"D", "A"}] || !got[[2]string{"C", "B"}] {
		t.Fatalf("unexpected games %v", res.Games)
	}
}

func TestFuzzLargeScale(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	rng := rand.New(rand.NewSource(424242))
	for it := 0; it < 20000; it++ {
		req := genRandomInstance(rng)
		oracle := bruteForce(t, req)
		res, err := Pair(req)
		if !oracle.ok {
			if err != ErrNoPairing {
				t.Fatalf("case %d: expected NO_PAIRING, got %v", it, res)
			}
			continue
		}
		if err != nil {
			t.Fatalf("case %d: unexpected error %v", it, err)
		}
		if res.ScorePenalty != oracle.sp || res.ColorPenalty != oracle.cp {
			t.Fatalf("case %d: penalties (%d,%d) want (%d,%d)",
				it, res.ScorePenalty, res.ColorPenalty, oracle.sp, oracle.cp)
		}
		if got := encodeSchedule(t, req, res); !reflect.DeepEqual(got, oracle.key) {
			t.Fatalf("case %d: key mismatch %v vs %v", it, got, oracle.key)
		}
	}
}
