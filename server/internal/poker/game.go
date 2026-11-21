package poker

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

const (
	SmallBlind   = 10
	BigBlind     = 20
	StartChips   = 2000
	ReliefChips  = 1000
	ReliefPeriod = time.Hour
	Human        = 0
)

type Player struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Bot        bool   `json:"bot"`
	Style      string `json:"style,omitempty"`
	Chips      int    `json:"chips"`
	Bet        int    `json:"bet"`
	Total      int    `json:"total"`
	Folded     bool   `json:"folded"`
	AllIn      bool   `json:"allIn"`
	LastAction string `json:"lastAction"`
	Hole       []Card `json:"hole"`

	acted bool
}

type Winner struct {
	ID     int    `json:"id"`
	Amount int    `json:"amount"`
	Hand   string `json:"hand"`
}

// Saved is what persists across restarts.
type Saved struct {
	Chips      int       `json:"chips"`
	LastRelief time.Time `json:"lastRelief"`
	Hands      int       `json:"hands"`
	Won        int       `json:"won"`
	BiggestPot int       `json:"biggestPot"`
}

type Table struct {
	mu       sync.Mutex
	rng      *rand.Rand
	players  []*Player
	dealer   int
	deck     []Card
	board    []Card
	street   string // idle preflop flop turn river showdown
	curBet   int
	minRaise int
	toAct    int
	seq      int
	handNo   int
	log      []string
	winners  []Winner
	shown    bool // hands were revealed at showdown
	saved    Saved

	// OnChange receives the human's view after every state change.
	OnChange func(View)
	// Persist is called with the state to save after each hand.
	Persist func(Saved)
	// BotDelay returns how long a bot "thinks".
	BotDelay func() time.Duration
}

var botSeats = []struct{ name, style string }{
	{"螃蟹哥", "loose"},
	{"石头", "rock"},
	{"教授", "tight"},
	{"疯子", "maniac"},
}

func NewTable(saved Saved) *Table {
	if saved.Chips <= 0 && saved.Hands == 0 {
		saved.Chips = StartChips
	}
	t := &Table{
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		street: "idle",
		toAct:  -1,
		saved:  saved,
		dealer: 0,
	}
	t.players = append(t.players, &Player{ID: 0, Name: "你", Chips: saved.Chips})
	for i, b := range botSeats {
		t.players = append(t.players, &Player{ID: i + 1, Name: b.name, Bot: true, Style: b.style, Chips: StartChips})
	}
	t.BotDelay = func() time.Duration { return time.Duration(700+t.rng.Intn(900)) * time.Millisecond }
	return t
}

func (t *Table) next(i int) int { return (i + 1) % len(t.players) }

func (t *Table) logf(format string, a ...any) {
	t.log = append(t.log, fmt.Sprintf(format, a...))
	if len(t.log) > 60 {
		t.log = t.log[len(t.log)-60:]
	}
}

func (t *Table) pot() int {
	n := 0
	for _, p := range t.players {
		n += p.Total
	}
	return n
}

func (t *Table) inHand() bool { return t.street != "idle" && t.street != "showdown" }

// Chips returns the human's stack.
func (t *Table) Chips() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.players[Human].Chips
}

// Spend removes chips from the human (pet shop). Only between hands.
func (t *Table) Spend(n int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inHand() {
		return errors.New("牌局进行中，打完这手再买")
	}
	if t.players[Human].Chips < n {
		return fmt.Errorf("筹码不够（需要 %d）", n)
	}
	t.players[Human].Chips -= n
	t.saveLocked()
	t.changed()
	return nil
}

// Earn adds chips to the human (pet rewards).
func (t *Table) Earn(n int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.players[Human].Chips += n
	t.saveLocked()
	t.changed()
}

// Relief tops up a busted player once per ReliefPeriod.
func (t *Table) Relief() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inHand() {
		return errors.New("牌局进行中")
	}
	if t.players[Human].Chips >= BigBlind {
		return errors.New("还有筹码，不能领救济")
	}
	if wait := ReliefPeriod - time.Since(t.saved.LastRelief); wait > 0 {
		return fmt.Errorf("%d 分钟后才能再领", int(wait.Minutes())+1)
	}
	t.players[Human].Chips += ReliefChips
	t.saved.LastRelief = time.Now()
	t.logf("你领取了 %d 救济筹码", ReliefChips)
	t.saveLocked()
	t.changed()
	return nil
}

func (t *Table) saveLocked() {
	t.saved.Chips = t.players[Human].Chips
	if t.Persist != nil {
		t.Persist(t.saved)
	}
}

func (t *Table) changed() {
	t.seq++
	if t.OnChange != nil {
		t.OnChange(t.viewLocked())
	}
}

// StartHand deals a new hand.
func (t *Table) StartHand() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.inHand() {
		return errors.New("这手还没打完")
	}
	if t.players[Human].Chips < BigBlind {
		return errors.New("筹码不足一个大盲，先领救济")
	}
	for _, p := range t.players {
		if p.Bot && p.Chips < BigBlind*5 {
			p.Chips = StartChips
			t.logf("%s 重新买入 %d", p.Name, StartChips)
		}
		p.Bet, p.Total, p.Folded, p.AllIn, p.acted, p.LastAction, p.Hole = 0, 0, false, false, false, "", nil
	}
	t.handNo++
	t.saved.Hands++
	t.dealer = t.next(t.dealer)
	t.deck = Deck(t.rng)
	t.board = nil
	t.winners = nil
	t.shown = false
	for r := 0; r < 2; r++ {
		for i := 0; i < len(t.players); i++ {
			p := t.players[(t.dealer+1+i)%len(t.players)]
			p.Hole = append(p.Hole, t.deck[0])
			t.deck = t.deck[1:]
		}
	}
	t.street = "preflop"
	t.logf("—— 第 %d 手，庄家 %s ——", t.handNo, t.players[t.dealer].Name)
	sb := t.next(t.dealer)
	bb := t.next(sb)
	t.put(t.players[sb], min(SmallBlind, t.players[sb].Chips))
	t.players[sb].LastAction = "小盲"
	t.put(t.players[bb], min(BigBlind, t.players[bb].Chips))
	t.players[bb].LastAction = "大盲"
	t.curBet = BigBlind
	t.minRaise = BigBlind
	t.toAct = t.next(bb)
	t.proceed()
	return nil
}

func (t *Table) put(p *Player, amt int) {
	if amt > p.Chips {
		amt = p.Chips
	}
	p.Chips -= amt
	p.Bet += amt
	p.Total += amt
	if p.Chips == 0 {
		p.AllIn = true
	}
}

func (t *Table) live() []*Player {
	var out []*Player
	for _, p := range t.players {
		if !p.Folded {
			out = append(out, p)
		}
	}
	return out
}

func (t *Table) canAct(p *Player) bool { return !p.Folded && !p.AllIn }

func (t *Table) bettingDone() bool {
	var actors []*Player
	for _, p := range t.players {
		if t.canAct(p) {
			actors = append(actors, p)
		}
	}
	if len(actors) == 0 {
		return true
	}
	if len(actors) == 1 && actors[0].Bet >= t.curBet {
		// everyone else is all-in or folded and this player has matched
		return true
	}
	for _, p := range actors {
		if !p.acted || p.Bet != t.curBet {
			return false
		}
	}
	return true
}

// proceed advances the hand until someone has to act; schedules bots.
func (t *Table) proceed() {
	for {
		if len(t.live()) == 1 {
			t.finish(false)
			return
		}
		if t.bettingDone() {
			if t.street == "river" {
				t.finish(true)
				return
			}
			t.nextStreet()
			continue
		}
		for i := 0; i < len(t.players) && !t.canAct(t.players[t.toAct]); i++ {
			t.toAct = t.next(t.toAct)
		}
		t.changed()
		if p := t.players[t.toAct]; p.Bot {
			seq := t.seq
			delay := t.BotDelay()
			go func() {
				time.Sleep(delay)
				t.botTurn(seq)
			}()
		}
		return
	}
}

func (t *Table) nextStreet() {
	for _, p := range t.players {
		p.Bet = 0
		p.acted = false
		if !p.Folded && !p.AllIn {
			p.LastAction = ""
		}
	}
	t.curBet = 0
	t.minRaise = BigBlind
	deal := func(n int) {
		t.board = append(t.board, t.deck[:n]...)
		t.deck = t.deck[n:]
	}
	switch t.street {
	case "preflop":
		t.street = "flop"
		deal(3)
	case "flop":
		t.street = "turn"
		deal(1)
	case "turn":
		t.street = "river"
		deal(1)
	}
	t.toAct = t.next(t.dealer)
}

// finish distributes the pot. showdown=false means everyone else folded.
func (t *Table) finish(showdown bool) {
	pot := t.pot()
	if pot > t.saved.BiggestPot {
		t.saved.BiggestPot = pot
	}
	won := map[int]int{}
	hands := map[int]string{}
	if !showdown {
		w := t.live()[0]
		won[w.ID] = pot
	} else {
		// run out the board (in case of all-ins before the river)
		for len(t.board) < 5 {
			t.board = append(t.board, t.deck[0])
			t.deck = t.deck[1:]
		}
		t.shown = true
		scores := map[int]Score{}
		for _, p := range t.live() {
			s := Eval(append(append([]Card{}, p.Hole...), t.board...))
			scores[p.ID] = s
			hands[p.ID] = CategoryNames[s.Category()]
		}
		contrib := map[int]int{}
		for _, p := range t.players {
			contrib[p.ID] = p.Total
		}
		for {
			level := 0
			for _, p := range t.live() {
				if c := contrib[p.ID]; c > 0 && (level == 0 || c < level) {
					level = c
				}
			}
			if level == 0 {
				break
			}
			slice := 0
			var eligible []*Player
			for _, p := range t.players {
				take := min(contrib[p.ID], level)
				slice += take
				if !p.Folded && contrib[p.ID] > 0 {
					eligible = append(eligible, p)
				}
				contrib[p.ID] -= take
			}
			var best Score
			var ws []*Player
			for _, p := range eligible {
				switch s := scores[p.ID]; {
				case s > best:
					best, ws = s, []*Player{p}
				case s == best:
					ws = append(ws, p)
				}
			}
			share := slice / len(ws)
			rem := slice - share*len(ws)
			// odd chips go to the first winner left of the dealer
			sort.Slice(ws, func(i, j int) bool {
				di := (ws[i].ID - t.dealer + len(t.players)) % len(t.players)
				dj := (ws[j].ID - t.dealer + len(t.players)) % len(t.players)
				return di < dj
			})
			for i, w := range ws {
				won[w.ID] += share
				if i == 0 {
					won[w.ID] += rem
				}
			}
		}
		// chips contributed beyond what any live player could match go back
		for _, p := range t.players {
			if c := contrib[p.ID]; c > 0 {
				won[p.ID] += c
			}
		}
	}
	t.winners = nil
	for _, p := range t.players {
		if a := won[p.ID]; a > 0 {
			p.Chips += a
			net := a - p.Total
			if net > 0 || showdown {
				t.winners = append(t.winners, Winner{ID: p.ID, Amount: a, Hand: hands[p.ID]})
			}
			if hands[p.ID] != "" {
				t.logf("%s 赢得 %d（%s）", p.Name, a, hands[p.ID])
			} else {
				t.logf("%s 赢得 %d", p.Name, a)
			}
			if p.ID == Human && net > 0 {
				t.saved.Won++
			}
		}
	}
	t.street = "showdown"
	t.toAct = -1
	t.saveLocked()
	t.changed()
}

// Act applies the human's action.
func (t *Table) Act(action string, amount int) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.inHand() || t.toAct != Human {
		return errors.New("还没轮到你")
	}
	return t.apply(t.players[Human], action, amount)
}

func (t *Table) botTurn(seq int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.seq != seq || !t.inHand() {
		return
	}
	p := t.players[t.toAct]
	if !p.Bot {
		return
	}
	a, amt := t.decide(p)
	if err := t.apply(p, a, amt); err != nil {
		// should not happen; fall back to the safest legal move
		if p.Bet == t.curBet {
			t.apply(p, "check", 0)
		} else {
			t.apply(p, "fold", 0)
		}
	}
}

// MinRaiseTo is the smallest legal raise target for p (or all-in).
func (t *Table) minRaiseTo(p *Player) int {
	return min(t.curBet+t.minRaise, p.Bet+p.Chips)
}

func (t *Table) apply(p *Player, action string, amount int) error {
	switch action {
	case "fold":
		p.Folded = true
		p.LastAction = "弃牌"
		t.logf("%s 弃牌", p.Name)
	case "check":
		if p.Bet != t.curBet {
			return errors.New("不能过牌")
		}
		p.LastAction = "过牌"
		t.logf("%s 过牌", p.Name)
	case "call":
		need := t.curBet - p.Bet
		if need <= 0 {
			return t.apply(p, "check", 0)
		}
		t.put(p, need)
		p.LastAction = "跟注"
		if p.AllIn {
			p.LastAction = "全下"
		}
		t.logf("%s %s %d", p.Name, p.LastAction, p.Bet)
	case "allin":
		return t.apply(p, "raise", p.Bet+p.Chips)
	case "raise":
		maxTo := p.Bet + p.Chips
		if amount > maxTo {
			amount = maxTo
		}
		if amount <= t.curBet {
			return t.apply(p, "call", 0)
		}
		if amount < t.curBet+t.minRaise && amount != maxTo {
			return fmt.Errorf("最少加到 %d", t.curBet+t.minRaise)
		}
		if size := amount - t.curBet; size > t.minRaise {
			t.minRaise = size
		}
		wasBet := t.curBet == 0
		t.put(p, amount-p.Bet)
		t.curBet = p.Bet
		for _, o := range t.players {
			if o != p {
				o.acted = false
			}
		}
		switch {
		case p.AllIn:
			p.LastAction = "全下"
		case wasBet:
			p.LastAction = "下注"
		default:
			p.LastAction = "加注"
		}
		t.logf("%s %s %d", p.Name, p.LastAction, p.Bet)
	default:
		return errors.New("未知操作")
	}
	p.acted = true
	t.toAct = t.next(p.ID)
	t.proceed()
	return nil
}

// View is the table as seen by the human player.
type View struct {
	Hand      int      `json:"hand"`
	Street    string   `json:"street"`
	Board     []Card   `json:"board"`
	Pot       int      `json:"pot"`
	CurBet    int      `json:"curBet"`
	Dealer    int      `json:"dealer"`
	ToAct     int      `json:"toAct"`
	Players   []Player `json:"players"`
	Winners   []Winner `json:"winners"`
	Log       []string `json:"log"`
	Legal     *Legal   `json:"legal,omitempty"`
	Stats     Saved    `json:"stats"`
	CanRelief bool     `json:"canRelief"`
	MyHand    string   `json:"myHand,omitempty"`
}

type Legal struct {
	CanCheck   bool `json:"canCheck"`
	CallAmount int  `json:"callAmount"`
	MinRaiseTo int  `json:"minRaiseTo"`
	MaxRaiseTo int  `json:"maxRaiseTo"`
}

func (t *Table) View() View {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.viewLocked()
}

func (t *Table) viewLocked() View {
	v := View{
		Hand: t.handNo, Street: t.street, Board: append([]Card{}, t.board...),
		Pot: t.pot(), CurBet: t.curBet, Dealer: t.dealer, ToAct: t.toAct,
		Winners: t.winners, Log: append([]string{}, t.log...), Stats: t.saved,
	}
	v.Stats.Chips = t.players[Human].Chips
	for _, p := range t.players {
		cp := *p
		if p.Bot && !(t.shown && !p.Folded) {
			if len(p.Hole) > 0 {
				cp.Hole = []Card{-1, -1}
			}
		}
		cp.Hole = append([]Card{}, cp.Hole...)
		v.Players = append(v.Players, cp)
	}
	h := t.players[Human]
	if len(h.Hole) == 2 {
		s := Eval(append(append([]Card{}, h.Hole...), t.board...))
		if len(t.board) >= 3 {
			v.MyHand = CategoryNames[s.Category()]
		} else if h.Hole[0].Rank() == h.Hole[1].Rank() {
			v.MyHand = "口袋对"
		}
	}
	if t.inHand() && t.toAct == Human {
		v.Legal = &Legal{
			CanCheck:   h.Bet == t.curBet,
			CallAmount: min(t.curBet-h.Bet, h.Chips),
			MinRaiseTo: t.minRaiseTo(h),
			MaxRaiseTo: h.Bet + h.Chips,
		}
	}
	v.CanRelief = !t.inHand() && h.Chips < BigBlind && time.Since(t.saved.LastRelief) >= ReliefPeriod
	return v
}
