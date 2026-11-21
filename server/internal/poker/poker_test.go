package poker

import (
	"math/rand"
	"strings"
	"testing"
	"time"
)

func cards(s string) []Card {
	var out []Card
	for _, f := range strings.Fields(s) {
		out = append(out, ParseCard(f))
	}
	return out
}

func TestEvalOrdering(t *testing.T) {
	// each hand must beat the next one
	order := []string{
		"As Ks Qs Js Ts 2d 3c", // royal
		"5h 4h 3h 2h Ah Kd Kc", // steel wheel
		"9c 9d 9h 9s 2c 3d 4h", // quads
		"Kc Kd Kh 2s 2c 3d 4h", // full house
		"2h 7h 9h Jh Kh Ad Ac", // flush
		"Tc Jd Qh Ks Ac 2d 2h", // broadway straight
		"Ac 2d 3h 4s 5c Kd Kh", // wheel beats trips
		"Qc Qd Qh 2s 7c 9d Th", // trips
		"Ac Ad Kc Kd 2s 3h 9c", // two pair
		"Ac Ad Kc Qd 2s 3h 9c", // pair, K kicker
		"Ac Ad Jc Td 2s 3h 9c", // pair, J kicker
		"Ac Kd Jc 9d 7s 3h 2c", // high card
	}
	for i := 0; i+1 < len(order); i++ {
		a, b := Eval(cards(order[i])), Eval(cards(order[i+1]))
		if a <= b {
			t.Fatalf("%q (%s) should beat %q (%s)", order[i], CategoryNames[a.Category()], order[i+1], CategoryNames[b.Category()])
		}
	}
	// best five only: 6th/7th card kickers don't matter
	if Eval(cards("Ac Ad Kc Qd Js 3h 2c")) != Eval(cards("Ac Ad Kc Qd Js 4h 2c")) {
		t.Fatal("kicker beyond five cards counted")
	}
	// two trips → full house with the lower trips as the pair
	if c := Eval(cards("Kc Kd Kh 2s 2c 2d 4h")).Category(); c != FullHouse {
		t.Fatalf("got %s", CategoryNames[c])
	}
}

func TestEquitySanity(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	aa := Equity(rng, cards("As Ah"), nil, 1, 3000)
	if aa < 0.8 || aa > 0.89 {
		t.Fatalf("AA vs random %.2f", aa)
	}
	low := Equity(rng, cards("7c 2d"), nil, 1, 3000)
	if low > 0.4 {
		t.Fatalf("72o vs random %.2f", low)
	}
}

// Play many hands with a random human and check chips are conserved and the
// game never gets stuck.
func TestRandomPlayConservesChips(t *testing.T) {
	tb := NewTable(Saved{Chips: 3000})
	tb.BotDelay = func() time.Duration { return 0 }
	rng := rand.New(rand.NewSource(7))
	for hand := 0; hand < 300; hand++ {
		if tb.Chips() < BigBlind {
			tb.mu.Lock()
			tb.players[Human].Chips = 3000
			tb.mu.Unlock()
		}
		tb.mu.Lock()
		before := 0
		for _, p := range tb.players {
			before += p.Chips
		}
		// bots that will rebuy change the total; account for it
		for _, p := range tb.players {
			if p.Bot && p.Chips < BigBlind*5 {
				before += StartChips - p.Chips
			}
		}
		tb.mu.Unlock()
		if err := tb.StartHand(); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			v := tb.View()
			if v.Street == "showdown" {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("hand %d stuck: %+v", hand, v)
			}
			if v.ToAct == Human && v.Legal != nil {
				var err error
				switch r := rng.Intn(10); {
				case r < 2:
					err = tb.Act("fold", 0)
				case r < 6:
					err = tb.Act("call", 0)
				case r < 9:
					err = tb.Act("raise", v.Legal.MinRaiseTo+rng.Intn(100))
				default:
					err = tb.Act("allin", 0)
				}
				if err != nil && !strings.Contains(err.Error(), "还没轮到你") {
					t.Fatalf("act: %v", err)
				}
				continue
			}
			time.Sleep(time.Millisecond)
		}
		tb.mu.Lock()
		after := 0
		for _, p := range tb.players {
			after += p.Chips
			if p.Chips < 0 {
				t.Fatalf("negative chips %+v", p)
			}
		}
		tb.mu.Unlock()
		if after != before {
			t.Fatalf("hand %d: chips %d -> %d", hand, before, after)
		}
	}
}

func TestSidePot(t *testing.T) {
	tb := NewTable(Saved{Chips: 100})
	tb.mu.Lock()
	defer tb.mu.Unlock()
	// human all-in 100 with the best hand, two bots put 500 each
	tb.board = cards("2c 7d 9h Js 3s")
	tb.deck = Deck(tb.rng)
	tb.players[0].Hole, tb.players[0].Total, tb.players[0].Chips = cards("Ac Ad"), 100, 0
	tb.players[1].Hole, tb.players[1].Total = cards("Kc Kd"), 500
	tb.players[2].Hole, tb.players[2].Total = cards("Qc Qd"), 500
	tb.players[3].Folded, tb.players[4].Folded = true, true
	tb.players[3].Total = 20
	tb.street = "river"
	c1, c2 := tb.players[1].Chips, tb.players[2].Chips
	tb.finish(true)
	if got := tb.players[0].Chips; got != 320 { // 100×3 + 20 folded
		t.Fatalf("main pot %d", got)
	}
	if got := tb.players[1].Chips - c1; got != 800 {
		t.Fatalf("side pot %d", got)
	}
	if tb.players[2].Chips != c2 {
		t.Fatal("loser got chips")
	}
}
