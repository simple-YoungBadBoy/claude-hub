package mahjong

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

const (
	Base     = 10  // chips for a 0-fan win
	Stake    = 500 // the most the human can lose in one round
	MinChips = 100
	Human    = 0
)

// Wallet is the shared chip balance (the poker stack).
type Wallet interface {
	Chips() int
	Spend(n int) error
	Earn(n int)
}

type Player struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Bot        bool   `json:"bot"`
	Que        int    `json:"que"` // void suit, -1 until chosen
	Hu         bool   `json:"hu"`
	HuName     string `json:"huName,omitempty"`
	HuFan      int    `json:"huFan"`
	HuTile     Tile   `json:"huTile"`
	Net        int    `json:"net"` // chips won (lost) this round
	Melds      []Meld `json:"melds"`
	Discards   []Tile `json:"discards"`
	Count      int    `json:"count"`          // concealed tiles
	Hand       []Tile `json:"hand,omitempty"` // the human's; everyone's once the round ends
	LastAction string `json:"lastAction,omitempty"`

	hand  Counts
	drawn Tile // the tile just drawn, -1 if none
}

// Saved is what persists across restarts.
type Saved struct {
	Rounds   int    `json:"rounds"`
	Wins     int    `json:"wins"`
	Net      int    `json:"net"`
	BestFan  int    `json:"bestFan"`
	BestName string `json:"bestName,omitempty"`
}

type claim struct {
	huers          []int
	gangBy, pengBy int
	opts           Options
}

type Game struct {
	mu        sync.Mutex
	rng       *rand.Rand
	players   [4]*Player
	wall      []Tile
	phase     string // idle dingque play end
	stage     string // discard (turn player to discard) | claim (the human may claim)
	round     int
	dealer    int
	turn      int
	discard   Tile
	from      int
	afterGang bool
	pending   *claim
	seq       int
	stake     int
	log       []string
	saved     Saved
	wallet    Wallet

	// OnChange receives the human's view after every state change.
	OnChange func(View)
	// Persist is called with the state to save after each round.
	Persist func(Saved)
	// BotDelay returns how long a bot "thinks".
	BotDelay func() time.Duration
}

var botNames = []string{"幺鸡", "二筒", "红中哥"}

func New(saved Saved, w Wallet) *Game {
	g := &Game{rng: rand.New(rand.NewSource(time.Now().UnixNano())), phase: "idle", saved: saved, wallet: w, turn: -1}
	g.players[0] = &Player{ID: 0, Name: "你", Que: -1, drawn: -1}
	for i, n := range botNames {
		g.players[i+1] = &Player{ID: i + 1, Name: n, Bot: true, Que: -1, drawn: -1}
	}
	g.BotDelay = func() time.Duration { return time.Duration(450+g.rng.Intn(600)) * time.Millisecond }
	return g
}

func (g *Game) logf(format string, a ...any) {
	g.log = append(g.log, fmt.Sprintf(format, a...))
	if len(g.log) > 80 {
		g.log = g.log[len(g.log)-80:]
	}
}

func (g *Game) inRound() bool { return g.phase == "dingque" || g.phase == "play" }

// Start deals a new round, holding the human's stake out of the wallet.
func (g *Game) Start() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inRound() {
		return errors.New("这局还没打完")
	}
	chips := g.wallet.Chips()
	if chips < MinChips {
		return fmt.Errorf("筹码不足 %d，先去德州领救济", MinChips)
	}
	stake := min(chips, Stake)
	if err := g.wallet.Spend(stake); err != nil {
		return errors.New("德州这手还没打完，打完再开麻将")
	}
	g.stake = stake
	g.round++
	g.dealer = (g.round - 1) % 4
	g.wall = g.wall[:0]
	for t := Tile(0); t < 27; t++ {
		for k := 0; k < 4; k++ {
			g.wall = append(g.wall, t)
		}
	}
	g.rng.Shuffle(len(g.wall), func(i, j int) { g.wall[i], g.wall[j] = g.wall[j], g.wall[i] })
	for _, p := range g.players {
		*p = Player{ID: p.ID, Name: p.Name, Bot: p.Bot, Que: -1, drawn: -1}
		for k := 0; k < 13; k++ {
			p.hand[g.take()]++
		}
	}
	d := g.players[g.dealer]
	d.drawn = g.take()
	d.hand[d.drawn]++
	for _, p := range g.players[1:] {
		p.Que = suggestQue(&p.hand)
	}
	g.phase, g.stage, g.turn, g.pending, g.afterGang = "dingque", "", g.dealer, nil, false
	g.logf("—— 第 %d 局，庄家 %s ——", g.round, d.Name)
	g.step()
	return nil
}

func (g *Game) take() Tile {
	t := g.wall[0]
	g.wall = g.wall[1:]
	return t
}

// suggestQue is the suit with the fewest tiles.
func suggestQue(c *Counts) int {
	best, bestN := 0, 99
	for s := 0; s < 3; s++ {
		n := 0
		for i := s * 9; i < s*9+9; i++ {
			n += int(c[i])
		}
		if n < bestN {
			best, bestN = s, n
		}
	}
	return best
}

// ChooseQue sets the human's void suit and starts play.
func (g *Game) ChooseQue(suit int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.phase != "dingque" {
		return errors.New("现在不是定缺的时候")
	}
	if suit < 0 || suit > 2 {
		return errors.New("只能缺万、条或筒")
	}
	g.players[Human].Que = suit
	parts := ""
	for _, p := range g.players {
		parts += fmt.Sprintf(" %s缺%s", p.Name, SuitNames[p.Que])
	}
	g.logf("定缺：%s", parts[1:])
	g.phase, g.stage = "play", "discard"
	g.step()
	return nil
}

// step publishes the view and, when a bot is to move, schedules it.
func (g *Game) step() {
	g.seq++ // the game advanced: any older pending bot turn is stale
	g.changed()
	if g.phase != "play" || g.stage != "discard" || !g.players[g.turn].Bot {
		return
	}
	seq, delay := g.seq, g.BotDelay()
	if g.players[Human].Hu {
		delay /= 4 // only bots left: don't make the human wait
	}
	go func() {
		time.Sleep(delay)
		g.botTurn(seq)
	}()
}

func (g *Game) changed() {
	if g.OnChange != nil {
		g.OnChange(g.viewLocked())
	}
}

func (g *Game) botTurn(seq int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seq != seq || g.phase != "play" || g.stage != "discard" || !g.players[g.turn].Bot {
		return
	}
	p := g.players[g.turn]
	if Wins(p.hand, p.Melds, p.Que) {
		g.zimo(p)
		return
	}
	if ks := g.kongs(p); len(ks) > 0 {
		g.selfKong(p, ks[0])
		return
	}
	g.doDiscard(p, botDiscard(p))
}

// kongs lists the tiles p can kong on its own turn.
func (g *Game) kongs(p *Player) []Tile {
	if len(g.wall) == 0 {
		return nil
	}
	var out []Tile
	for t := Tile(0); t < 27; t++ {
		if t.Suit() == p.Que {
			continue
		}
		if p.hand[t] == 4 {
			out = append(out, t)
		}
		for _, m := range p.Melds {
			if m.Kind == "peng" && m.Tile == t && p.hand[t] == 1 {
				out = append(out, t)
			}
		}
	}
	return out
}

func (p *Player) hasQue() bool { return p.hand.hasSuit(p.Que) }

func (g *Game) doDiscard(p *Player, t Tile) error {
	if t < 0 || t > 26 || p.hand[t] == 0 {
		return errors.New("手里没有这张牌")
	}
	if p.hasQue() && t.Suit() != p.Que {
		return fmt.Errorf("先把缺的%s打完", SuitNames[p.Que])
	}
	p.hand[t]--
	p.drawn = -1
	p.Discards = append(p.Discards, t)
	p.LastAction = "打 " + t.String()
	g.discard, g.from, g.afterGang = t, p.ID, false
	g.resolveDiscard()
	return nil
}

func (g *Game) takeDiscard() {
	d := g.players[g.from]
	d.Discards = d.Discards[:len(d.Discards)-1]
}

// resolveDiscard finds who claims the tile just discarded. A win beats a
// kong or pong; bots decide at once, the human is asked when it matters.
func (g *Game) resolveDiscard() {
	t := g.discard
	c := &claim{gangBy: -1, pengBy: -1}
	human := false
	for i := 1; i < 4; i++ {
		q := g.players[(g.from+i)%4]
		if q.Hu {
			continue
		}
		h := q.hand
		h[t]++
		hu := Wins(h, q.Melds, q.Que)
		gang := q.hand[t] == 3 && t.Suit() != q.Que && len(g.wall) > 0
		peng := q.hand[t] >= 2 && t.Suit() != q.Que
		if !q.Bot {
			c.opts = Options{Hu: hu, Peng: peng, Pass: hu || gang || peng, Tile: t}
			if gang {
				c.opts.Gang = []Tile{t}
			}
			human = c.opts.Pass
			continue
		}
		switch {
		case hu:
			c.huers = append(c.huers, q.ID)
		case gang:
			c.gangBy = q.ID
		case peng:
			c.pengBy = q.ID
		}
	}
	if human && (c.opts.Hu || len(c.huers) == 0) {
		g.stage, g.pending = "claim", c
		g.step()
		return
	}
	g.settle(c.huers, c.gangBy, c.pengBy)
}

func (g *Game) settle(huers []int, gangBy, pengBy int) {
	g.pending = nil
	t := g.discard
	switch {
	case len(huers) > 0:
		g.takeDiscard()
		d := g.players[g.from]
		for _, id := range huers {
			q := g.players[id]
			q.hand[t]++
			fan, name := Fan(q.hand, q.Melds, false, false)
			paid := g.pay(d, q, Base<<fan)
			q.Hu, q.HuName, q.HuFan, q.HuTile, q.LastAction = true, name, fan, t, "胡"
			g.logf("%s 胡 %s（%s %d番），%s 点炮付 %d", q.Name, t, name, fan, d.Name, paid)
		}
		d.LastAction = "点炮"
		if g.done() {
			g.end()
			return
		}
		g.draw(g.next(huers[len(huers)-1]), false)
	case gangBy >= 0:
		g.takeDiscard()
		q := g.players[gangBy]
		q.hand[t] -= 3
		q.Melds = append(q.Melds, Meld{"gang", t})
		q.LastAction = "杠"
		paid := g.pay(g.players[g.from], q, 2*Base)
		g.logf("%s 杠 %s，%s 付 %d", q.Name, t, g.players[g.from].Name, paid)
		g.draw(gangBy, true)
	case pengBy >= 0:
		g.takeDiscard()
		q := g.players[pengBy]
		q.hand[t] -= 2
		q.Melds = append(q.Melds, Meld{"peng", t})
		q.LastAction = "碰"
		g.logf("%s 碰 %s", q.Name, t)
		g.turn, g.stage = pengBy, "discard"
		g.step()
	default:
		g.draw(g.next(g.from), false)
	}
}

// next is the next player still in the round after i.
func (g *Game) next(i int) int {
	for k := 1; k <= 4; k++ {
		if j := (i + k) % 4; !g.players[j].Hu {
			return j
		}
	}
	return i
}

func (g *Game) done() bool {
	n := 0
	for _, p := range g.players {
		if p.Hu {
			n++
		}
	}
	return n >= 3
}

func (g *Game) draw(i int, afterGang bool) {
	if len(g.wall) == 0 {
		g.end()
		return
	}
	p := g.players[i]
	p.drawn = g.take()
	p.hand[p.drawn]++
	g.turn, g.stage, g.afterGang = i, "discard", afterGang
	g.step()
}

// pay moves chips; the human never pays more than what is left of the stake.
func (g *Game) pay(from, to *Player, n int) int {
	if from.ID == Human {
		n = max(0, min(n, g.stake+from.Net))
	}
	from.Net -= n
	to.Net += n
	return n
}

func (g *Game) others(p *Player) []*Player {
	var out []*Player
	for _, q := range g.players {
		if q != p && !q.Hu {
			out = append(out, q)
		}
	}
	return out
}

func (g *Game) zimo(p *Player) {
	fan, name := Fan(p.hand, p.Melds, true, g.afterGang)
	total := 0
	for _, q := range g.others(p) {
		total += g.pay(q, p, Base<<fan)
	}
	p.Hu, p.HuName, p.HuFan, p.HuTile, p.LastAction = true, name, fan, p.drawn, "自摸"
	p.drawn = -1
	g.logf("%s 自摸（%s %d番），收 %d", p.Name, name, fan, total)
	if g.done() {
		g.end()
		return
	}
	g.draw(g.next(p.ID), false)
}

func (g *Game) selfKong(p *Player, t Tile) {
	total := 0
	if p.hand[t] == 4 {
		p.hand[t] = 0
		p.Melds = append(p.Melds, Meld{"angang", t})
		for _, q := range g.others(p) {
			total += g.pay(q, p, 2*Base)
		}
		g.logf("%s 暗杠，收 %d", p.Name, total)
	} else {
		p.hand[t] = 0
		for i := range p.Melds {
			if p.Melds[i].Kind == "peng" && p.Melds[i].Tile == t {
				p.Melds[i].Kind = "gang"
			}
		}
		for _, q := range g.others(p) {
			total += g.pay(q, p, Base)
		}
		g.logf("%s 补杠 %s，收 %d", p.Name, t, total)
	}
	p.drawn = -1
	p.LastAction = "杠"
	g.draw(p.ID, true)
}

// end settles a finished round. When the wall ran out, a player still
// holding the void suit (花猪) pays everyone else still in; a player who is
// not ready (没叫) pays each ready one what their best wait was worth.
func (g *Game) end() {
	if !g.done() {
		var left []*Player
		for _, p := range g.players {
			if !p.Hu {
				left = append(left, p)
			}
		}
		ready := map[int]int{}
		for _, p := range left {
			if !p.hasQue() {
				if w, best := Waits(p.hand, p.Melds, p.Que); len(w) > 0 {
					ready[p.ID] = best
				}
			}
		}
		for _, p := range left {
			if !p.hasQue() {
				continue
			}
			p.LastAction = "花猪"
			for _, q := range left {
				if !q.hasQue() {
					g.logf("%s 花猪，付 %s %d", p.Name, q.Name, g.pay(p, q, Base<<MaxFan))
				}
			}
		}
		for _, p := range left {
			if _, ok := ready[p.ID]; ok || p.hasQue() {
				continue
			}
			p.LastAction = "没叫"
			for _, q := range left {
				if f, ok := ready[q.ID]; ok {
					g.logf("%s 没叫，付 %s %d", p.Name, q.Name, g.pay(p, q, Base<<f))
				}
			}
		}
		g.logf("流局")
	}
	me := g.players[Human]
	g.wallet.Earn(g.stake + me.Net)
	g.saved.Rounds++
	g.saved.Net += me.Net
	if me.Hu {
		g.saved.Wins++
		if me.HuFan > g.saved.BestFan || g.saved.BestName == "" {
			g.saved.BestFan, g.saved.BestName = me.HuFan, me.HuName
		}
	}
	if me.Net >= 0 {
		g.logf("本局你赢 %d", me.Net)
	} else {
		g.logf("本局你输 %d", -me.Net)
	}
	if g.Persist != nil {
		g.Persist(g.saved)
	}
	g.phase, g.stage, g.pending, g.stake = "end", "", nil, 0
	g.step()
}

// Act applies the human's move: discard, hu, gang (with a tile), peng or pass.
func (g *Game) Act(action string, t Tile) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	me := g.players[Human]
	if g.phase != "play" || me.Hu {
		return errors.New("还没轮到你")
	}
	if g.stage == "claim" {
		c := g.pending
		switch action {
		case "hu":
			if !c.opts.Hu {
				return errors.New("这张胡不了")
			}
			var huers []int // in turn order from the discarder
			for i := 1; i < 4; i++ {
				id := (g.from + i) % 4
				if id == Human {
					huers = append(huers, id)
				}
				for _, h := range c.huers {
					if h == id {
						huers = append(huers, id)
					}
				}
			}
			g.settle(huers, -1, -1)
		case "gang":
			if len(c.opts.Gang) == 0 {
				return errors.New("杠不了")
			}
			g.settle(nil, Human, -1)
		case "peng":
			if !c.opts.Peng {
				return errors.New("碰不了")
			}
			g.settle(nil, -1, Human)
		case "pass":
			g.settle(c.huers, c.gangBy, c.pengBy)
		default:
			return errors.New("现在只能胡、杠、碰或过")
		}
		return nil
	}
	if g.turn != Human || g.stage != "discard" {
		return errors.New("还没轮到你")
	}
	switch action {
	case "hu":
		if !Wins(me.hand, me.Melds, me.Que) {
			return errors.New("还没胡")
		}
		g.zimo(me)
	case "gang":
		for _, k := range g.kongs(me) {
			if k == t {
				g.selfKong(me, t)
				return nil
			}
		}
		return errors.New("杠不了这张")
	case "discard":
		return g.doDiscard(me, t)
	default:
		return errors.New("现在该出牌")
	}
	return nil
}

// botDiscard keeps the hand closest to ready: void-suit tiles go first, then
// a discard that leaves the most waits, then the least connected tile.
func botDiscard(p *Player) Tile {
	c := p.hand
	if p.hasQue() {
		best, bestN := Tile(-1), int8(9)
		for t := Tile(p.Que * 9); t < Tile(p.Que*9+9); t++ {
			if c[t] > 0 && c[t] < bestN {
				best, bestN = t, c[t]
			}
		}
		return best
	}
	best, bestWaits := Tile(-1), 0
	for t := Tile(0); t < 27; t++ {
		if c[t] == 0 {
			continue
		}
		c[t]--
		if w, _ := Waits(c, p.Melds, p.Que); len(w) > bestWaits {
			best, bestWaits = t, len(w)
		}
		c[t]++
	}
	if best >= 0 {
		return best
	}
	bestScore := 1 << 30
	for t := Tile(0); t < 27; t++ {
		if c[t] == 0 {
			continue
		}
		s := int(c[t]-1) * 4
		r := t.Rank()
		for _, d := range []int{-2, -1, 1, 2} {
			if r+d >= 1 && r+d <= 9 && c[int(t)+d] > 0 {
				s += 3 - abs(d)
			}
		}
		if r == 1 || r == 9 {
			s--
		}
		if s < bestScore {
			best, bestScore = t, s
		}
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ---- view ----

// Options are the human's legal moves right now.
type Options struct {
	Discard bool   `json:"discard"`
	Hu      bool   `json:"hu"`
	Gang    []Tile `json:"gang,omitempty"`
	Peng    bool   `json:"peng"`
	Pass    bool   `json:"pass"`
	Tile    Tile   `json:"tile"` // the discard that can be claimed
	MustQue bool   `json:"mustQue"`
}

type View struct {
	Phase   string    `json:"phase"`
	Round   int       `json:"round"`
	Dealer  int       `json:"dealer"`
	Turn    int       `json:"turn"`
	Wall    int       `json:"wall"`
	Players []Player  `json:"players"`
	Drawn   Tile      `json:"drawn"` // the human's new tile, -1 if none
	Last    *LastTile `json:"last,omitempty"`
	Options *Options  `json:"options,omitempty"`
	QueHint int       `json:"queHint"`
	Stake   int       `json:"stake"`
	Log     []string  `json:"log"`
	Stats   Saved     `json:"stats"`
}

type LastTile struct {
	From int  `json:"from"`
	Tile Tile `json:"tile"`
}

func list(c Counts) []Tile {
	out := []Tile{}
	for t := Tile(0); t < 27; t++ {
		for k := int8(0); k < c[t]; k++ {
			out = append(out, t)
		}
	}
	return out
}

func (g *Game) View() View {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.viewLocked()
}

func (g *Game) viewLocked() View {
	v := View{Phase: g.phase, Round: g.round, Dealer: g.dealer, Turn: g.turn, Wall: len(g.wall), Drawn: -1, Stake: g.stake,
		Log: append([]string{}, g.log...), Stats: g.saved, QueHint: suggestQue(&g.players[Human].hand)}
	for _, p := range g.players {
		c := *p
		c.Melds = append([]Meld{}, p.Melds...)
		c.Discards = append([]Tile{}, p.Discards...)
		c.Count = p.hand.total()
		if p.ID == Human || g.phase == "end" {
			h := p.hand
			if p.ID == Human && p.drawn >= 0 && g.inRound() {
				h[p.drawn]--
				v.Drawn = p.drawn
			}
			c.Hand = list(h)
		}
		if g.phase == "dingque" && p.ID != Human {
			c.Que = -1 // revealed together once the human has chosen
		}
		v.Players = append(v.Players, c)
	}
	if g.phase == "play" && g.stage == "claim" {
		v.Last = &LastTile{g.from, g.discard}
		o := g.pending.opts
		v.Options = &o
	} else if g.phase == "play" {
		if n := len(g.players[g.from].Discards); g.turn != g.from && n > 0 && g.players[g.from].Discards[n-1] == g.discard {
			v.Last = &LastTile{g.from, g.discard}
		}
		if me := g.players[Human]; g.turn == Human && !me.Hu {
			v.Options = &Options{Discard: true, Hu: Wins(me.hand, me.Melds, me.Que), Gang: g.kongs(me), MustQue: me.hasQue()}
		}
	}
	sort.SliceStable(v.Players, func(i, j int) bool { return v.Players[i].ID < v.Players[j].ID })
	return v
}
