package mahjong

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

const (
	Base     = 10   // chips for a 0-fan win
	Stake    = 1000 // the most the human can lose in one round
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
	ID         int       `json:"id"`
	Name       string    `json:"name"`
	Bot        bool      `json:"bot"`
	Que        int       `json:"que"` // void suit, -1 until chosen
	Hu         bool      `json:"hu"`
	HuOrder    int       `json:"huOrder,omitempty"` // 1st, 2nd, 3rd to win
	HuName     string    `json:"huName,omitempty"`
	HuItems    []FanItem `json:"huItems,omitempty"`
	HuFan      int       `json:"huFan"`
	HuTile     Tile      `json:"huTile"`
	HuZimo     bool      `json:"huZimo,omitempty"`
	Net        int       `json:"net"` // chips won (lost) this round
	Melds      []Meld    `json:"melds"`
	Discards   []Tile    `json:"discards"`
	Count      int       `json:"count"`          // concealed tiles
	Hand       []Tile    `json:"hand,omitempty"` // the human's; everyone's once the round ends
	LastAction string    `json:"lastAction,omitempty"`
	Swapped    bool      `json:"swapped,omitempty"` // has handed in three tiles
	PassHu     bool      `json:"passHu,omitempty"`  // passed a win: no discard wins until the next draw
	Ready      bool      `json:"ready,omitempty"`   // ready at the end of a drawn round
	Pig        bool      `json:"pig,omitempty"`     // 花猪 at the end of a drawn round

	hand     Counts
	drawn    Tile   // the tile just drawn, -1 if none
	swap     []Tile // the three tiles handed in
	kongGain int    // what the latest kong earned (呼叫转移 hands it on)
}

// Saved is what persists across restarts.
type Saved struct {
	Rounds   int    `json:"rounds"`
	Wins     int    `json:"wins"`
	Net      int    `json:"net"`
	BestFan  int    `json:"bestFan"`
	BestName string `json:"bestName,omitempty"`
}

// Payment is one movement of chips, for the settlement sheet.
type Payment struct {
	From   int    `json:"from"`
	To     int    `json:"to"`
	Amount int    `json:"amount"`
	Why    string `json:"why"`
	Kong   bool   `json:"kong,omitempty"` // 刮风下雨: refunded by 退税
}

// claim is a discard (or a kong to rob) the human may answer.
type claim struct {
	rob            bool // 抢杠: tile is being added to konger's pong
	huers          []int
	gangBy, pengBy int
	opts           Options
}

type Game struct {
	mu         sync.Mutex
	rng        *rand.Rand
	players    [4]*Player
	wall       []Tile
	phase      string // idle swap dingque play end
	stage      string // discard (turn player to discard) | claim (the human may claim)
	round      int
	dealer     int
	nextDealer int // first winner of this round (the discarder on a multi-win), -1 if none yet
	turn       int
	discard    Tile
	from       int
	afterKong  bool // the turn player drew after a kong
	kongShot   bool // the pending discard came right after the discarder's kong (杠上炮)
	konger     int  // who is adding to a pong while it may be robbed
	claimed    bool // a pong or kong happened this round (no more 地胡)
	swapDir    int  // 1 下家, 2 对家, 3 上家
	got        []Tile
	huCount    int
	pending    *claim
	seq        int
	stake      int
	pays       []Payment
	log        []string
	saved      Saved
	wallet     Wallet

	// OnChange receives the human's view after every state change.
	OnChange func(View)
	// Persist is called with the state to save after each round.
	Persist func(Saved)
	// BotDelay returns how long a bot "thinks".
	BotDelay func() time.Duration
}

var botNames = []string{"幺鸡", "二筒", "红中哥"}

// SwapNames names the direction three tiles go: to the next player, across,
// or to the previous one.
var SwapNames = []string{"", "下家", "对家", "上家"}

func New(saved Saved, w Wallet) *Game {
	g := &Game{rng: rand.New(rand.NewSource(time.Now().UnixNano())), phase: "idle", saved: saved, wallet: w, turn: -1, nextDealer: -1}
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

func (g *Game) inRound() bool { return g.phase == "swap" || g.phase == "dingque" || g.phase == "play" }

// Start deals a new round, holding the human's stake out of the wallet. The
// dealer is the first winner of the last round (it stays put after a round
// nobody won).
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
	if g.nextDealer >= 0 {
		g.dealer = g.nextDealer
	}
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
	for _, p := range g.players[1:] {
		p.swap, p.Swapped = suggestSwap(&p.hand), true
	}
	g.swapDir = 1 + g.rng.Intn(3)
	g.phase, g.stage, g.turn, g.pending, g.afterKong, g.kongShot = "swap", "", g.dealer, nil, false, false
	g.claimed, g.huCount, g.nextDealer, g.pays, g.got = false, 0, -1, nil, nil
	g.logf("—— 第 %d 局，庄家 %s ——", g.round, g.players[g.dealer].Name)
	g.step()
	return nil
}

func (g *Game) take() Tile {
	t := g.wall[0]
	g.wall = g.wall[1:]
	return t
}

// Swap hands in the human's three tiles (one suit); then everyone's go round.
func (g *Game) Swap(tiles []Tile) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.phase != "swap" {
		return errors.New("现在不是换三张的时候")
	}
	me := g.players[Human]
	if len(tiles) != 3 {
		return errors.New("要选三张")
	}
	h := me.hand
	for _, t := range tiles {
		if t < 0 || t > 26 || h[t] == 0 {
			return errors.New("手里没有这张牌")
		}
		if t.Suit() != tiles[0].Suit() {
			return errors.New("三张要同一门花色")
		}
		h[t]--
	}
	me.swap, me.Swapped = append([]Tile{}, tiles...), true
	for _, p := range g.players {
		for _, t := range p.swap {
			p.hand[t]--
		}
	}
	for _, p := range g.players {
		to := g.players[(p.ID+g.swapDir)%4]
		for _, t := range p.swap {
			to.hand[t]++
		}
	}
	g.got = append([]Tile{}, g.players[(Human+4-g.swapDir)%4].swap...)
	g.logf("换三张：换给%s", SwapNames[g.swapDir])
	for _, p := range g.players[1:] {
		p.Que = suggestQue(&p.hand)
	}
	g.phase = "dingque"
	g.step()
	return nil
}

// ChooseQue sets the human's void suit; the dealer then draws the first tile.
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
	g.phase = "play"
	g.draw(g.dealer, false)
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

// seenBy counts the tiles player p can see: its own hand, every discard and
// every exposed meld.
func (g *Game) seenBy(p *Player) Counts {
	s := p.hand
	for _, q := range g.players {
		for _, t := range q.Discards {
			s[t]++
		}
		for _, m := range q.Melds {
			if m.Kind == "angang" && q != p {
				continue // face down
			}
			n := int8(3)
			if m.Kind != "peng" {
				n = 4
			}
			s[m.Tile] += n
		}
	}
	return s
}

// danger guesses how likely a discard feeds a player still in the round.
func (g *Game) danger(p *Player) func(Tile) int {
	return func(t Tile) int {
		d := 0
		for _, q := range g.players {
			if q == p || q.Hu || t.Suit() == q.Que || q.hasQue() {
				continue // a player still holding the void suit isn't ready
			}
			safe := false
			for _, x := range q.Discards {
				if x == t {
					safe = true
				}
			}
			if !safe {
				d += 1 + len(q.Melds)
			}
		}
		return d
	}
}

func (g *Game) botTurn(seq int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.seq != seq || g.phase != "play" || g.stage != "discard" || !g.players[g.turn].Bot {
		return
	}
	p := g.players[g.turn]
	if p.drawn >= 0 && Wins(p.hand, p.Melds, p.Que) {
		g.zimo(p)
		return
	}
	for _, k := range g.kongs(p) {
		if botKong(p, k, false) {
			g.selfKong(p, k)
			return
		}
	}
	seen := g.seenBy(p)
	g.doDiscard(p, botDiscard(p, &seen, g.danger(p)))
}

// kongs lists the tiles p can kong on its own turn, right after drawing.
func (g *Game) kongs(p *Player) []Tile {
	if len(g.wall) == 0 || p.drawn < 0 {
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
	g.discard, g.from, g.kongShot, g.afterKong = t, p.ID, g.afterKong, false
	g.resolveDiscard()
	return nil
}

func (g *Game) takeDiscard() {
	d := g.players[g.from]
	d.Discards = d.Discards[:len(d.Discards)-1]
}

// canRon: q may win on t from someone else (not while passing a win).
func canRon(q *Player, t Tile) bool {
	if q.Hu || q.PassHu {
		return false
	}
	h := q.hand
	h[t]++
	return Wins(h, q.Melds, q.Que)
}

// resolveDiscard finds who claims the tile just discarded. Wins beat a kong
// or pong (several players may win on one tile); bots decide at once, the
// human is asked when it matters.
func (g *Game) resolveDiscard() {
	t := g.discard
	c := &claim{gangBy: -1, pengBy: -1}
	human := false
	for i := 1; i < 4; i++ {
		q := g.players[(g.from+i)%4]
		if q.Hu {
			continue
		}
		hu := canRon(q, t)
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
		case gang && botKong(q, t, true):
			c.gangBy = q.ID
		case peng && botPeng(q, t):
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

// inOrder sorts winners by seat order after the discarder.
func (g *Game) inOrder(ids []int) []int {
	var out []int
	for i := 1; i < 4; i++ {
		id := (g.from + i) % 4
		for _, h := range ids {
			if h == id {
				out = append(out, id)
				break
			}
		}
	}
	return out
}

func (g *Game) settle(huers []int, gangBy, pengBy int) {
	g.pending = nil
	t := g.discard
	switch {
	case len(huers) > 0:
		huers = g.inOrder(huers)
		g.takeDiscard()
		d := g.players[g.from]
		for _, id := range huers {
			q := g.players[id]
			q.hand[t]++
			g.win(q, t, d, WinCtx{KongDiscard: g.kongShot, LastTile: len(g.wall) == 0})
		}
		if g.kongShot && d.kongGain > 0 { // 呼叫转移: the kong's takings go to the winner
			q := g.players[huers[0]]
			paid := g.pay(d, q, d.kongGain, "呼叫转移", false)
			g.logf("呼叫转移：%s 把杠钱 %d 转给 %s", d.Name, paid, q.Name)
			d.kongGain = 0
		}
		d.LastAction = "点炮"
		if g.nextDealer < 0 {
			g.nextDealer = huers[0]
			if len(huers) > 1 {
				g.nextDealer = d.ID
			}
		}
		g.kongShot = false
		if g.done() {
			g.end()
			return
		}
		g.draw(g.next(huers[len(huers)-1]), false)
	case gangBy >= 0:
		g.takeDiscard()
		q, d := g.players[gangBy], g.players[g.from]
		q.hand[t] -= 3
		q.Melds = append(q.Melds, Meld{"gang", t, d.ID})
		q.LastAction, q.PassHu, g.claimed, g.kongShot = "杠", false, true, false
		paid := g.pay(d, q, 2*Base, "刮风 "+t.String(), true)
		q.kongGain = paid
		g.logf("%s 杠 %s，%s 付 %d", q.Name, t, d.Name, paid)
		g.draw(gangBy, true)
	case pengBy >= 0:
		g.takeDiscard()
		q := g.players[pengBy]
		q.hand[t] -= 2
		q.Melds = append(q.Melds, Meld{"peng", t, g.from})
		q.LastAction, q.PassHu, g.claimed, g.kongShot = "碰", false, true, false
		g.logf("%s 碰 %s", q.Name, t)
		g.turn, g.stage = pengBy, "discard"
		g.step()
	default:
		g.kongShot = false
		g.draw(g.next(g.from), false)
	}
}

// win marks q as having won on tile t, paid by d (nil: self-drawn, all
// players still in pay).
func (g *Game) win(q *Player, t Tile, d *Player, x WinCtx) {
	fan, items := Score(q.hand, q.Melds, x)
	name := itemsName(items)
	g.huCount++
	q.Hu, q.HuOrder, q.HuName, q.HuItems, q.HuFan, q.HuTile, q.HuZimo = true, g.huCount, name, items, fan, t, x.Zimo
	why := name
	if fan > 0 {
		why += fmt.Sprintf(" %d番", fan)
	}
	if d != nil {
		q.LastAction = "胡"
		paid := g.pay(d, q, Base<<fan, "点炮 "+why, false)
		g.logf("%s 胡 %s（%s），%s 点炮付 %d", q.Name, t, why, d.Name, paid)
		return
	}
	q.LastAction = "自摸"
	total := 0
	for _, o := range g.others(q) {
		total += g.pay(o, q, Base<<fan, why, false)
	}
	g.logf("%s 自摸 %s（%s），收 %d", q.Name, t, why, total)
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

func (g *Game) done() bool { return g.huCount >= 3 }

func (g *Game) draw(i int, afterKong bool) {
	if len(g.wall) == 0 {
		g.end()
		return
	}
	p := g.players[i]
	p.drawn = g.take()
	p.hand[p.drawn]++
	p.PassHu = false // 过手胡 lasts until your next draw
	g.turn, g.stage, g.afterKong = i, "discard", afterKong
	g.step()
}

// pay moves chips; the human never pays more than what is left of the stake.
func (g *Game) pay(from, to *Player, n int, why string, kong bool) int {
	if from.ID == Human {
		n = max(0, min(n, g.stake+from.Net))
	}
	if n == 0 {
		return 0
	}
	from.Net -= n
	to.Net += n
	g.pays = append(g.pays, Payment{from.ID, to.ID, n, why, kong})
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
	x := WinCtx{Zimo: true, KongDraw: g.afterKong, LastTile: len(g.wall) == 0}
	switch {
	case g.claimed:
	case p.ID == g.dealer && g.noDiscards():
		x.Heaven = true // the dealer's first 14 tiles
	case p.ID != g.dealer && len(p.Discards) == 0:
		x.Earth = true // first own draw, nothing claimed yet
	}
	t := p.drawn
	p.drawn = -1
	g.win(p, t, nil, x)
	if g.nextDealer < 0 {
		g.nextDealer = p.ID
	}
	g.afterKong = false
	if g.done() {
		g.end()
		return
	}
	g.draw(g.next(p.ID), false)
}

func (g *Game) noDiscards() bool {
	for _, q := range g.players {
		if len(q.Discards) > 0 {
			return false
		}
	}
	return true
}

func (g *Game) selfKong(p *Player, t Tile) {
	p.drawn = -1
	g.claimed = true
	if p.hand[t] == 4 {
		p.hand[t] = 0
		p.Melds = append(p.Melds, Meld{"angang", t, p.ID})
		total := 0
		for _, q := range g.others(p) {
			total += g.pay(q, p, 2*Base, "下雨 "+t.String(), true)
		}
		p.kongGain, p.LastAction = total, "暗杠"
		g.logf("%s 暗杠，收 %d", p.Name, total)
		g.draw(p.ID, true)
		return
	}
	// 补杠: the others may rob the fourth tile to win (抢杠胡)
	c := &claim{rob: true, gangBy: -1, pengBy: -1}
	human := false
	for i := 1; i < 4; i++ {
		q := g.players[(p.ID+i)%4]
		if !canRon(q, t) {
			continue
		}
		if q.Bot {
			c.huers = append(c.huers, q.ID)
		} else {
			human = true
			c.opts = Options{Hu: true, Pass: true, Tile: t, Rob: true}
		}
	}
	g.discard, g.from, g.konger = t, p.ID, p.ID
	if human {
		p.LastAction = "补杠"
		g.stage, g.pending = "claim", c
		g.step()
		return
	}
	g.finishAddKong(c.huers)
}

// finishAddKong completes a 补杠, or lets the robbers win on its tile.
func (g *Game) finishAddKong(robbers []int) {
	g.pending = nil
	p, t := g.players[g.konger], g.discard
	p.hand[t]--
	if len(robbers) > 0 {
		robbers = g.inOrder(robbers)
		for _, id := range robbers {
			q := g.players[id]
			q.hand[t]++
			g.win(q, t, p, WinCtx{RobKong: true, LastTile: len(g.wall) == 0})
		}
		p.LastAction = "被抢杠"
		if g.nextDealer < 0 {
			g.nextDealer = robbers[0]
			if len(robbers) > 1 {
				g.nextDealer = p.ID
			}
		}
		if g.done() {
			g.end()
			return
		}
		g.draw(g.next(robbers[len(robbers)-1]), false)
		return
	}
	for i := range p.Melds {
		if p.Melds[i].Kind == "peng" && p.Melds[i].Tile == t {
			p.Melds[i].Kind = "gang"
		}
	}
	total := 0
	for _, q := range g.others(p) {
		total += g.pay(q, p, Base, "刮风 "+t.String(), true)
	}
	p.kongGain, p.LastAction = total, "补杠"
	g.logf("%s 补杠 %s，收 %d", p.Name, t, total)
	g.draw(p.ID, true)
}

// end settles a finished round. When the wall ran out: a player still
// holding the void suit (花猪) pays every other player still in the top
// score; a player not ready (没叫) pays each ready one what their best wait is
// worth; and whoever isn't ready hands back what their kongs earned (退税).
func (g *Game) end() {
	if !g.done() {
		var left []*Player
		for _, p := range g.players {
			if !p.Hu {
				left = append(left, p)
			}
		}
		best := map[int]int{}
		for _, p := range left {
			if p.hasQue() {
				p.Pig = true
				continue
			}
			if w, f := Waits(p.hand, p.Melds, p.Que); len(w) > 0 {
				p.Ready, best[p.ID] = true, f
			}
		}
		for _, p := range left {
			if !p.Pig {
				continue
			}
			p.LastAction = "花猪"
			for _, q := range left {
				if !q.Pig {
					g.logf("%s 花猪，付 %s %d", p.Name, q.Name, g.pay(p, q, Base<<MaxFan, "查花猪", false))
				}
			}
		}
		for _, p := range left {
			if p.Ready || p.Pig {
				continue
			}
			p.LastAction = "没叫"
			for _, q := range left {
				if q.Ready {
					g.logf("%s 没叫，付 %s %d", p.Name, q.Name, g.pay(p, q, Base<<best[q.ID], "查大叫", false))
				}
			}
		}
		for _, p := range left {
			if p.Ready {
				continue
			}
			for _, k := range append([]Payment{}, g.pays...) {
				if k.Kong && k.To == p.ID {
					g.pay(p, g.players[k.From], k.Amount, "退税", false)
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
			huers := append([]int{Human}, c.huers...)
			if c.rob {
				g.finishAddKong(huers)
			} else {
				g.settle(huers, -1, -1)
			}
		case "gang", "peng":
			if (action == "gang" && len(c.opts.Gang) == 0) || (action == "peng" && !c.opts.Peng) {
				return errors.New("现在" + map[string]string{"gang": "杠", "peng": "碰"}[action] + "不了")
			}
			if c.opts.Hu {
				me.PassHu = true
			}
			switch {
			case len(c.huers) > 0:
				g.settle(c.huers, -1, -1) // a win beats the claim
			case action == "gang":
				g.settle(nil, Human, -1)
			default:
				g.settle(nil, -1, Human)
			}
		case "pass":
			if c.opts.Hu {
				me.PassHu = true // 过手胡: no discard wins until the next draw
			}
			if c.rob {
				g.finishAddKong(c.huers)
			} else {
				g.settle(c.huers, c.gangBy, c.pengBy)
			}
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
		if me.drawn < 0 || !Wins(me.hand, me.Melds, me.Que) {
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
