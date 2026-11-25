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

func tile(s string) Tile { return list(hand(s))[0] }

func TestWins(t *testing.T) {
	cases := []struct {
		h    string
		que  int
		want bool
	}{
		{"123m 456m 789m 234s 55s", 2, true},
		{"11m 22m 33m 44s 55s 66s 77s", 2, true}, // seven pairs
		{"1111m 33m 44s 55s 66s 77s", 2, true},   // four of a kind is two pairs
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
	if Wins(hand("123s 55s 789s"), []Meld{{"peng", 20, 1}}, 0) {
		t.Error("8 tiles and a pong are not a hand")
	}
	if !Wins(hand("123s 456s 789s 55s"), []Meld{{"peng", 20, 1}}, 0) {
		t.Error("pong 3p plus 123 55 789 条")
	}
}

func TestScore(t *testing.T) {
	pong := func(s string) Meld { return Meld{"peng", tile(s), 1} }
	kong := func(s string) Meld { return Meld{"gang", tile(s), 1} }
	cases := []struct {
		h     string
		melds []Meld
		x     WinCtx
		fan   int
		name  string
	}{
		{"123m 456m 789s 234s 55s", nil, WinCtx{}, 0, "平胡"},
		{"123m 456m 789s 234s 55s", nil, WinCtx{Zimo: true}, 1, "平胡 自摸"},
		{"111m 333m 555s 777s 99s", nil, WinCtx{}, 1, "对对胡"},
		{"123m 456m 789m 234m 55m", nil, WinCtx{}, 2, "清一色"},
		{"11m 22m 33m 44s 55s 66s 77s", nil, WinCtx{}, 2, "七对"},
		{"1111m 33m 44s 55s 66s 77s", nil, WinCtx{}, 3, "龙七对"},
		{"1111m 33m 44m 55m 66m 77m", nil, WinCtx{}, 5, "清龙七对"},
		{"11m 33m 44m 55m 66m 77m 99m", nil, WinCtx{}, 4, "清七对"},
		{"111m 333m 555m 777m 99m", nil, WinCtx{Zimo: true}, 4, "清对 自摸"},
		{"222m 555s 888s 222p 55m", nil, WinCtx{}, 3, "将对"},
		{"123m 456m 789s 55s", []Meld{pong("2s")}, WinCtx{}, 0, "平胡"},
		{"234m 567m 789s 55s", []Meld{pong("4m")}, WinCtx{}, 1, "平胡 1根"}, // pong 4万 + the fourth in hand
		{"55s", []Meld{pong("1m"), pong("3m"), pong("7s"), pong("9s")}, WinCtx{}, 2, "对对胡 金钩钓"},
		{"123m 456m 789s 55s", []Meld{kong("2s")}, WinCtx{Zimo: true, KongDraw: true}, 3, "平胡 1根 自摸 杠上花"},
		{"123m 456m 789s 234s 55s", nil, WinCtx{KongDiscard: true}, 1, "平胡 杠上炮"},
		{"123m 456m 789s 234s 55s", nil, WinCtx{RobKong: true}, 1, "平胡 抢杠胡"},
		{"123m 456m 789s 234s 55s", nil, WinCtx{Zimo: true, LastTile: true}, 2, "平胡 自摸 海底捞月"},
		{"123m 456m 789s 234s 55s", nil, WinCtx{Zimo: true, Heaven: true}, MaxFan, "天胡 自摸"},
		{"1111m 2222m 33m 44m 55m", nil, WinCtx{Zimo: true, KongDraw: true}, MaxFan, "清龙七对 1根 自摸 杠上花"}, // 9 capped at 8
	}
	for _, c := range cases {
		f, n := Fan(hand(c.h), c.melds, c.x)
		if f != c.fan || n != c.name {
			t.Errorf("%s %v: %d %q, want %d %q", c.h, c.melds, f, n, c.fan, c.name)
		}
	}
}

func TestShanten(t *testing.T) {
	cases := []struct {
		h     string
		melds int
		want  int
	}{
		{"123m 456m 789s 234s 55s", 0, -1},
		{"123m 456m 789s 234s 5s", 0, 0},
		{"123m 456m 789s 24s 55s", 0, 0},
		{"123m 456m 789s 2s 5s 8s 9m", 0, 2},
		{"11m 22m 33s 44s 55s 66p 7p", 0, 0}, // seven pairs, one short
		{"123m 456m 55s 789s", 1, -1},
		{"159m 159s 159p 3m 7s 7p 3p", 0, 4}, // 13 3s 57s|79s 13p 57p
	}
	for _, c := range cases {
		if got := Shanten(hand(c.h), c.melds, -1); got != c.want {
			t.Errorf("%s: shanten %d, want %d", c.h, got, c.want)
		}
	}
	if Shanten(hand("123m 456m 789s 234s 5s 9p"), 0, 2) != 0 {
		t.Error("void-suit tiles are left out")
	}
}

func TestWaits(t *testing.T) {
	w, best := Waits(hand("123m 456m 789s 234s 5s"), nil, 2)
	if len(w) != 2 || w[0] != tile("2s") || w[1] != tile("5s") || best != 0 {
		t.Fatalf("%v %d", w, best)
	}
	seen := hand("123m 456m 789s 234s 5s 555s") // three more 5条 are out
	ws := waitsSeen(hand("123m 456m 789s 234s 5s"), nil, 2, &seen)
	if ws[1].Tile != tile("5s") || ws[1].Left != 0 || ws[0].Left != 3 {
		t.Fatalf("left counts the seen tiles: %+v", ws)
	}
}

func TestBotChoices(t *testing.T) {
	p := &Player{Que: 2, hand: hand("123m 456m 789s 24s 55s 9m")}
	seen := p.hand
	safe := func(Tile) int { return 0 }
	if d := botDiscard(p, &seen, safe); d != tile("9m") {
		t.Errorf("throws the loose 9万, got %v", d)
	}
	p = &Player{Que: 2, hand: hand("123m 456m 789s 24s 55s 7p")}
	if d := botDiscard(p, &seen, safe); d != tile("7p") {
		t.Errorf("void suit first, got %v", d)
	}
	// ready either way: wait on what is left
	p = &Player{Que: 2, hand: hand("123m 456m 789s 23s 55s 9s")}
	seen = p.hand
	seen[tile("1s")] += 3 // 1条 is gone
	if d := botDiscard(p, &seen, safe); d != tile("9s") {
		t.Errorf("keeps the open 1-4条 wait over… got %v", d)
	}
	if !botPeng(&Player{Que: 2, hand: hand("123m 456m 99s 55s 7s 8m 1m")}, tile("9s")) {
		t.Error("pongs when it brings the hand closer")
	}
	if botPeng(&Player{Que: 2, hand: hand("11m 22m 44m 55s 77s 8s 9m")}, tile("1m")) {
		t.Error("keeps five pairs for seven pairs")
	}
	if got := suggestQue(&[]Counts{hand("123m 456m 789s 11s 9p")}[0]); got != 2 {
		t.Errorf("void in the suit with fewest tiles, got %d", got)
	}
	c := hand("123m 456m 789s 1p 5p 9p 3s")
	sw := suggestSwap(&c)
	if len(sw) != 3 || sw[0].Suit() != 2 || sw[1].Suit() != 2 || sw[2].Suit() != 2 {
		t.Errorf("swap the three loose 筒: %v", sw)
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

// autoplay plays the human's side: swap and void as suggested, win when
// possible, pass on claims, discard the drawn tile (or a void-suit one).
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
		case v.Phase == "swap":
			if err := g.Swap(v.SwapHint); err != nil {
				t.Fatal(err)
			}
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
	w := &wallet{chips: 100000}
	g := New(Saved{}, w)
	g.BotDelay = func() time.Duration { return 0 }
	wins, fans := 0, map[string]bool{}
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
		for i, p := range v.Players {
			sum += p.Net
			if len(p.Hand) != p.Count {
				t.Fatalf("hands are shown at the end: %+v", p)
			}
			if p.Count+3*len(p.Melds) != 13 && !p.Hu {
				t.Fatalf("round %d: %s holds %d tiles + %d melds", r, p.Name, p.Count, len(p.Melds))
			}
			if p.Hu {
				wins++
				fans[p.HuItems[0].Name] = true
			}
			lines := 0
			for _, l := range v.Sheet[i] {
				lines += l.Amount
			}
			if lines != p.Net {
				t.Fatalf("round %d: %s's sheet adds up to %d, net %d", r, p.Name, lines, p.Net)
			}
		}
		if sum != 0 {
			t.Fatalf("round %d: nets add up to %d", r, sum)
		}
		if me := v.Players[Human].Net; w.Chips() != before+me || me < -Stake {
			t.Fatalf("round %d: wallet %d → %d, net %d", r, before, w.Chips(), me)
		}
	}
	if wins < 40 {
		t.Fatalf("bots should win hands: %d wins in 40 rounds", wins)
	}
	if len(fans) < 2 {
		t.Errorf("bots only ever win one way: %v", fans)
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

func TestSwapThree(t *testing.T) {
	g := New(Saved{}, &wallet{chips: 5000})
	g.BotDelay = func() time.Duration { return time.Hour }
	g.Start()
	v := g.View()
	if v.Phase != "swap" || len(v.SwapHint) != 3 || v.SwapDir != 0 {
		t.Fatalf("swap first, direction hidden: %+v", v)
	}
	before := hand("")
	for _, t := range v.Players[Human].Hand {
		before[t]++
	}
	mixed := []Tile{v.Players[Human].Hand[0], v.Players[Human].Hand[12], v.Players[Human].Hand[6]}
	if mixed[0].Suit() != mixed[1].Suit() {
		if err := g.Swap(mixed); err == nil {
			t.Fatal("three of one suit only")
		}
	}
	g.mu.Lock()
	dir := g.swapDir
	from := g.players[(Human+4-dir)%4].swap
	g.mu.Unlock()
	hint := v.SwapHint
	if err := g.Swap(hint); err != nil {
		t.Fatal(err)
	}
	v = g.View()
	if v.Phase != "dingque" || v.SwapDir != dir || len(v.Got) != 3 {
		t.Fatalf("after the swap: %+v", v)
	}
	after := before
	for _, t := range hint {
		after[t]--
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, t := range from {
		after[t]++
	}
	if after != g.players[Human].hand {
		t.Fatal("gave three, got the three from the player before in that direction")
	}
	for _, p := range g.players {
		if p.hand.total() != 13 {
			t.Fatalf("%s holds %d", p.Name, p.hand.total())
		}
	}
}

// playing starts a round, swaps and chooses voids, with bots that never
// move by themselves; the human (dealer) has drawn.
func playing(t *testing.T) *Game {
	t.Helper()
	g := New(Saved{}, &wallet{chips: 5000})
	g.BotDelay = func() time.Duration { return time.Hour }
	g.Start()
	g.Swap(g.View().SwapHint)
	if err := g.ChooseQue(2); err != nil {
		t.Fatal(err)
	}
	return g
}

func TestVoidSuitGoesFirst(t *testing.T) {
	g := playing(t)
	g.mu.Lock()
	me := g.players[Human]
	me.hand = hand("1123m 456m 789s 23s 5p 6p")
	me.drawn = tile("1m")
	g.mu.Unlock()
	if err := g.Act("discard", tile("1m")); err == nil || !strings.Contains(err.Error(), "筒") {
		t.Fatalf("void suit first: %v", err)
	}
	if err := g.Act("discard", tile("5p")); err != nil {
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
		g.pay(me, bot, 40, "test", false)
	}
	g.mu.Unlock()
	if me.Net != -120 || bot.Net != 120 {
		t.Fatalf("paid %d", -me.Net)
	}
}

// table sets everyone's hand (bots void in 筒 unless set) and makes seat
// `turn` hold the discard `d`, then discards it.
func discardTo(g *Game, from int, d Tile) {
	p := g.players[from]
	p.hand[d]++
	g.turn = from
	g.doDiscard(p, d)
}

func TestHumanClaimsPong(t *testing.T) {
	g := playing(t)
	g.mu.Lock()
	me := g.players[Human]
	me.Que, me.hand, me.drawn = 2, hand("55m 123s 456s 789s 11s 9m"), -1
	for _, p := range g.players[1:] {
		p.Que, p.hand = 2, hand("1s")
	}
	discardTo(g, 3, tile("5m"))
	g.mu.Unlock()
	v := g.View()
	if v.Options == nil || !v.Options.Peng || v.Options.Hu || v.Last == nil || v.Last.Tile != tile("5m") {
		t.Fatalf("the human is asked: %+v", v.Options)
	}
	if err := g.Act("discard", 4); err == nil {
		t.Fatal("only a claim answer now")
	}
	if err := g.Act("peng", -1); err != nil {
		t.Fatal(err)
	}
	v = g.View()
	if m := v.Players[Human].Melds; len(m) != 1 || m[0].Tile != 4 || m[0].From != 3 || v.Turn != Human || v.Options == nil || !v.Options.Discard || v.Options.Hu {
		t.Fatalf("pong then discard: %+v", v)
	}
	if n := len(v.Players[3].Discards); n != 0 {
		t.Fatal("the claimed tile leaves the discards")
	}
}

func TestPassingAWinBlocksDiscardWinsUntilNextDraw(t *testing.T) {
	g := playing(t)
	g.mu.Lock()
	me := g.players[Human]
	me.Que, me.hand, me.drawn = 2, hand("123m 456m 789s 234s 5s"), -1
	for _, p := range g.players[1:] {
		p.Que, p.hand = 2, hand("1s")
	}
	discardTo(g, 3, tile("5s"))
	g.mu.Unlock()
	if v := g.View(); v.Options == nil || !v.Options.Hu {
		t.Fatalf("can win on 5条: %+v", v.Options)
	}
	g.Act("pass", -1) // the human draws next
	g.mu.Lock()
	me.hand[me.drawn]--
	me.drawn = -1
	me.PassHu = true // as after passing, before the next draw
	discardTo(g, 1, tile("2s"))
	g.mu.Unlock()
	if v := g.View(); v.Options != nil && v.Options.Hu {
		t.Fatal("过手胡: no discard win before drawing again")
	}
}

func TestRobbingAKong(t *testing.T) {
	g := playing(t)
	g.mu.Lock()
	me, bot := g.players[Human], g.players[1]
	me.Que, me.hand, me.drawn = 2, hand("123m 456m 789s 234s 5s"), -1
	bot.Que, bot.hand, bot.Melds = 2, hand("5s 11m 234m 567m 9m"), []Meld{{"peng", tile("5s"), 2}}
	bot.drawn = tile("9m")
	g.turn = 1
	g.selfKong(bot, tile("5s"))
	g.mu.Unlock()
	v := g.View()
	if v.Options == nil || !v.Options.Hu || !v.Options.Rob {
		t.Fatalf("the human may rob the kong: %+v", v.Options)
	}
	if err := g.Act("hu", -1); err != nil {
		t.Fatal(err)
	}
	v = g.View()
	p := v.Players[Human]
	if !p.Hu || !strings.Contains(p.HuName, "抢杠胡") || p.Net != Base<<1 || v.Players[1].Melds[0].Kind != "peng" {
		t.Fatalf("robbed: %+v / %+v", p, v.Players[1].Melds)
	}
}

func TestKongThenDiscardWinTransfersTheKong(t *testing.T) {
	g := playing(t)
	g.mu.Lock()
	me, bot := g.players[Human], g.players[3]
	me.Que, me.hand, me.drawn = 2, hand("123m 456m 789s 234s 5s"), -1
	bot.Que, bot.hand = 2, hand("777m 123m 99m 44s 678s")
	g.players[1].Que, g.players[1].hand = 2, hand("1s")
	g.players[2].Que, g.players[2].hand = 2, hand("1s")
	discardTo(g, 2, tile("7m")) // bot 3 kongs it (2 pays 20)
	g.mu.Unlock()
	if v := g.View(); len(v.Players[3].Melds) != 1 || v.Players[3].Melds[0].Kind != "gang" || v.Turn != 3 {
		t.Fatalf("bot kongs the 7万: %+v", v.Players[3])
	}
	g.mu.Lock()
	b := g.players[3]
	b.hand[b.drawn]--
	b.drawn = -1
	b.hand[tile("5s")]++
	g.doDiscard(b, tile("5s"))
	g.mu.Unlock()
	if v := g.View(); v.Options == nil || !v.Options.Hu {
		t.Fatalf("human can win: %+v", v.Options)
	}
	g.Act("hu", -1)
	v := g.View()
	if p := v.Players[Human]; !strings.Contains(p.HuName, "杠上炮") || p.Net != Base<<1+2*Base {
		t.Fatalf("杠上炮 plus the kong's 20 (呼叫转移): %+v", p)
	}
}

func TestDrawnRoundRefundsKongsOfPlayersNotReady(t *testing.T) {
	g := playing(t)
	g.mu.Lock()
	for _, p := range g.players {
		p.Que, p.Melds, p.drawn = 2, nil, -1
	}
	a, b, c, d := g.players[0], g.players[1], g.players[2], g.players[3]
	a.hand = hand("123m 456m 789s 234s 5s") // ready
	b.hand = hand("147m 258m 369s 1s 5s 8s 9s")
	c.hand = hand("147m 258m 369s 1s 5s 8s 1p") // 花猪
	d.hand = hand("123m 456m 789s 234s 9s")     // ready
	g.pay(c, b, 20, "刮风", true)                 // b's kong money
	g.wall = nil
	g.end()
	g.mu.Unlock()
	v := g.View()
	if !v.Players[0].Ready || !v.Players[2].Pig || v.Players[1].Ready {
		t.Fatalf("ready / pig: %+v", v.Players)
	}
	// b: 20 kong + 2560 from the pig, pays a and d 10 each, refunds the 20
	if v.Players[1].Net != 2540 {
		t.Fatalf("b net %d", v.Players[1].Net)
	}
	if v.Players[2].Net != -3*(Base<<MaxFan) {
		t.Fatalf("pig pays everyone else the top score: %d", v.Players[2].Net)
	}
}

func TestNextDealerIsFirstWinner(t *testing.T) {
	g := New(Saved{}, &wallet{chips: 100000})
	g.BotDelay = func() time.Duration { return 0 }
	for r := 0; r < 5; r++ {
		g.Start()
		v := autoplay(t, g)
		g.mu.Lock()
		next, dealer := g.nextDealer, g.dealer
		g.mu.Unlock()
		first := -1
		for _, p := range v.Players {
			if p.HuOrder == 1 {
				first = p.ID
			}
		}
		if first < 0 && next != -1 {
			t.Fatal("no winner, no new dealer")
		}
		g.Start()
		g.mu.Lock()
		got := g.dealer
		g.mu.Unlock()
		if (next >= 0 && got != next) || (next < 0 && got != dealer) {
			t.Fatalf("dealer %d, want %d (first winner %d)", got, next, first)
		}
		g.mu.Lock()
		g.end() // abandon this round to start the next one
		g.mu.Unlock()
	}
}
