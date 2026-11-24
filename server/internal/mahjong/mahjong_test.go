package mahjong

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// hand parses "123m 55p 789s" (m 万, s 条, p 筒).
func hand(s string) Counts {
	var c Counts
	for _, f := range strings.Fields(s) {
		suit := strings.IndexByte("msp", f[len(f)-1])
		for _, r := range f[:len(f)-1] {
			c[suit*9+int(r-'1')]++
		}
	}
	return c
}

func TestWins(t *testing.T) {
	cases := []struct {
		h    string
		que  int
		want bool
	}{
		{"123m 456m 789m 234s 55s", 2, true},
		{"11m 22m 33m 44s 55s 66s 77s", 2, true}, // seven pairs
		{"123m 456m 789m 234s 56s", 2, false},
		{"123m 456m 789m 234s 55p", 2, false}, // holds the void suit
		{"111m 123m 345s 678s 99s", 2, true},
		{"1112345678999m", 1, false}, // 13 tiles
	}
	for _, c := range cases {
		if got := Wins(hand(c.h), nil, c.que); got != c.want {
			t.Errorf("%s: %v", c.h, got)
		}
	}
	if Wins(hand("123s 55s 789s"), []Meld{{"peng", 20}}, 0) {
		t.Error("8 tiles and a pong are not a hand")
	}
	if !Wins(hand("123s 456s 789s 55s"), []Meld{{"peng", 20}}, 0) {
		t.Error("pong 3p plus 123 55 789 条")
	}
}

func TestFan(t *testing.T) {
	cases := []struct {
		h     string
		melds []Meld
		zimo  bool
		fan   int
		name  string
	}{
		{"123m 456m 789s 234s 55s", nil, false, 0, "平胡"},
		{"123m 456m 789s 234s 55s", nil, true, 1, "平胡 自摸"},
		{"111m 333m 555s 777s 99s", nil, false, 1, "对对胡"},
		{"123m 456m 789m 234m 55m", nil, false, 2, "清一色"},
		{"11m 22m 33m 44s 55s 66s 77s", nil, false, 2, "七对"},
		{"11m 11m 33m 44s 55s 66s 77s", nil, false, 3, "龙七对 1根"},
		{"111m 333m 555m 777m 99m", nil, true, 4, "清一色 对对胡 自摸"}, // 4 capped
		{"123m 456m 789s 55s", []Meld{{"peng", 1 + 9}}, false, 0, "平胡"},
		{"234m 567m 789s 55s", []Meld{{"peng", 3}}, false, 1, "1根"}, // pong 4万 + the fourth in hand
	}
	for _, c := range cases {
		f, n := Fan(hand(c.h), c.melds, c.zimo, false)
		if f != c.fan || n != c.name {
			t.Errorf("%s: %d %q, want %d %q", c.h, f, n, c.fan, c.name)
		}
	}
}

func TestWaits(t *testing.T) {
	w, best := Waits(hand("123m 456m 789s 234s 5s"), nil, 2)
	if len(w) != 2 || w[0] != 9+1 || w[1] != 9+4 || best != 0 { // 2条 5条 (单吊 5 or 2-5 not: 2345 + 5)
		t.Fatalf("%v %d", w, best)
	}
}

type wallet struct {
	mu    sync.Mutex
	chips int
	busy  bool
}

func (w *wallet) Chips() int { w.mu.Lock(); defer w.mu.Unlock(); return w.chips }
func (w *wallet) Earn(n int) { w.mu.Lock(); defer w.mu.Unlock(); w.chips += n }
func (w *wallet) Spend(n int) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.busy {
		return errors.New("busy")
	}
	w.chips -= n
	return nil
}

// autoplay plays the human's side: win when possible, pass on claims,
// discard the drawn tile (or the first void-suit one).
func autoplay(t *testing.T, g *Game) View {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		v := g.View()
		if v.Phase == "end" {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("round stalled: %+v", v)
		}
		switch {
		case v.Phase == "dingque":
			if err := g.ChooseQue(v.QueHint); err != nil {
				t.Fatal(err)
			}
		case v.Options != nil && v.Options.Hu:
			if err := g.Act("hu", -1); err != nil {
				t.Fatal(err)
			}
		case v.Options != nil && v.Options.Pass:
			if err := g.Act("pass", -1); err != nil {
				t.Fatal(err)
			}
		case v.Options != nil && v.Options.Discard:
			me := v.Players[Human]
			tile := v.Drawn
			for _, h := range append(append([]Tile{}, me.Hand...), v.Drawn) {
				if v.Options.MustQue && h >= 0 && h.Suit() == me.Que {
					tile = h
					break
				}
			}
			if tile < 0 {
				tile = me.Hand[len(me.Hand)-1]
			}
			if err := g.Act("discard", tile); err != nil {
				t.Fatalf("discard %v: %v (%+v)", tile, err, v.Options)
			}
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestRoundsFinishAndChipsBalance(t *testing.T) {
	w := &wallet{chips: 5000}
	g := New(Saved{}, w)
	g.BotDelay = func() time.Duration { return 0 }
	wins := 0
	for r := 0; r < 40; r++ {
		before := w.Chips()
		if err := g.Start(); err != nil {
			t.Fatal(err)
		}
		if w.Chips() != before-Stake {
			t.Fatal("the stake is held during the round")
		}
		v := autoplay(t, g)
		sum := 0
		for _, p := range v.Players {
			sum += p.Net
			if len(p.Hand) != p.Count {
				t.Fatalf("hands are shown at the end: %+v", p)
			}
			if p.Hu {
				wins++
			}
		}
		if sum != 0 {
			t.Fatalf("round %d: nets add up to %d", r, sum)
		}
		if me := v.Players[Human].Net; w.Chips() != before+me || me < -Stake {
			t.Fatalf("round %d: wallet %d → %d, net %d", r, before, w.Chips(), me)
		}
	}
	if wins < 20 {
		t.Fatalf("bots should win hands: %d wins in 40 rounds", wins)
	}
	if g.View().Stats.Rounds != 40 {
		t.Fatal("rounds counted")
	}
}

func TestStartNeedsChipsAndIdlePoker(t *testing.T) {
	w := &wallet{chips: 50}
	g := New(Saved{}, w)
	if v := g.View(); v.Drawn != -1 || len(v.Players[Human].Hand) != 0 {
		t.Fatalf("no tiles before the first round: %+v", v)
	}
	if err := g.Start(); err == nil {
		t.Fatal("too few chips")
	}
	w.chips, w.busy = 300, true
	if err := g.Start(); err == nil || !strings.Contains(err.Error(), "德州") {
		t.Fatalf("poker hand in progress: %v", err)
	}
	w.busy = false
	if err := g.Start(); err != nil || g.View().Stake != 300 {
		t.Fatalf("stake is what there is: %v", err)
	}
	if err := g.Start(); err == nil {
		t.Fatal("one round at a time")
	}
}

func TestVoidSuitGoesFirst(t *testing.T) {
	g := New(Saved{}, &wallet{chips: 1000})
	g.BotDelay = func() time.Duration { return time.Hour }
	g.Start()
	g.mu.Lock()
	me := g.players[Human]
	me.hand = hand("123m 456m 789s 23s 5p 6p")
	me.hand[0]++ // 14 tiles: the human deals first
	g.mu.Unlock()
	if err := g.Act("discard", 0); err == nil {
		t.Fatal("not before choosing the void suit")
	}
	g.ChooseQue(2)
	if err := g.Act("discard", 0); err == nil || !strings.Contains(err.Error(), "筒") {
		t.Fatalf("void suit first: %v", err)
	}
	if err := g.Act("discard", 18+4); err != nil {
		t.Fatal(err)
	}
}

func TestHumanLossCappedAtStake(t *testing.T) {
	w := &wallet{chips: 120}
	g := New(Saved{}, w)
	g.BotDelay = func() time.Duration { return time.Hour }
	g.Start()
	g.mu.Lock()
	me, bot := g.players[Human], g.players[1]
	for i := 0; i < 5; i++ {
		g.pay(me, bot, 40)
	}
	g.mu.Unlock()
	if me.Net != -120 || bot.Net != 120 {
		t.Fatalf("paid %d", -me.Net)
	}
}

func TestHumanClaimsPong(t *testing.T) {
	g := New(Saved{}, &wallet{chips: 1000})
	g.BotDelay = func() time.Duration { return time.Hour }
	g.Start()
	g.ChooseQue(2)
	g.mu.Lock()
	me, bot := g.players[Human], g.players[3]
	me.Que, me.hand = 2, hand("55m 123s 456s 789s 11s 9m")
	for _, p := range g.players[1:] {
		p.Que, p.hand = 2, Counts{}
	}
	bot.hand = hand("5m 1p")
	bot.Que = 1
	g.turn = 3
	g.doDiscard(bot, 4) // 5万
	g.mu.Unlock()
	v := g.View()
	if v.Options == nil || !v.Options.Peng || v.Options.Hu || v.Last == nil || v.Last.Tile != 4 {
		t.Fatalf("the human is asked: %+v", v.Options)
	}
	if err := g.Act("discard", 4); err == nil {
		t.Fatal("only a claim answer now")
	}
	if err := g.Act("peng", -1); err != nil {
		t.Fatal(err)
	}
	v = g.View()
	if m := v.Players[Human].Melds; len(m) != 1 || m[0].Tile != 4 || v.Turn != Human || v.Options == nil || !v.Options.Discard {
		t.Fatalf("pong then discard: %+v", v)
	}
	if n := len(v.Players[3].Discards); n != 0 {
		t.Fatal("the claimed tile leaves the discards")
	}
}
