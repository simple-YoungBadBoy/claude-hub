package mahjong

import "sort"

// Options are the human's legal moves right now.
type Options struct {
	Discard bool   `json:"discard"`
	Hu      bool   `json:"hu"`
	Gang    []Tile `json:"gang,omitempty"`
	Peng    bool   `json:"peng"`
	Pass    bool   `json:"pass"`
	Tile    Tile   `json:"tile"` // the discard (or kong tile) that can be claimed
	Rob     bool   `json:"rob,omitempty"`
	MustQue bool   `json:"mustQue"`
}

// Hint is what a discard leaves the human waiting on.
type Hint struct {
	Tile  Tile   `json:"tile"`
	Waits []Wait `json:"waits"`
}

// Line is one row of a player's settlement sheet.
type Line struct {
	Text   string `json:"text"`
	Amount int    `json:"amount"`
}

type View struct {
	Phase    string    `json:"phase"`
	Round    int       `json:"round"`
	Dealer   int       `json:"dealer"`
	Turn     int       `json:"turn"`
	Wall     int       `json:"wall"`
	Players  []Player  `json:"players"`
	Drawn    Tile      `json:"drawn"` // the human's new tile, -1 if none
	Last     *LastTile `json:"last,omitempty"`
	Options  *Options  `json:"options,omitempty"`
	QueHint  int       `json:"queHint"`
	SwapHint []Tile    `json:"swapHint,omitempty"`
	SwapDir  int       `json:"swapDir,omitempty"` // shown once everyone has swapped
	Got      []Tile    `json:"got,omitempty"`     // the three tiles the human received
	Hints    []Hint    `json:"hints,omitempty"`   // discards that leave the human ready
	Waits    []Wait    `json:"waits,omitempty"`   // what the human is ready for now
	Sheet    [][]Line  `json:"sheet,omitempty"`   // per player, once the round ends
	Stake    int       `json:"stake"`
	Log      []string  `json:"log"`
	Stats    Saved     `json:"stats"`
}

type LastTile struct {
	From int  `json:"from"`
	Tile Tile `json:"tile"`
}

func (g *Game) View() View {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.viewLocked()
}

func (g *Game) viewLocked() View {
	me := g.players[Human]
	v := View{Phase: g.phase, Round: g.round, Dealer: g.dealer, Turn: g.turn, Wall: len(g.wall), Drawn: -1, Stake: g.stake,
		Log: append([]string{}, g.log...), Stats: g.saved}
	if g.phase != "idle" {
		v.QueHint = suggestQue(&me.hand)
	}
	if g.phase == "swap" && !me.Swapped {
		v.SwapHint = suggestSwap(&me.hand)
	}
	if g.phase != "swap" && g.phase != "idle" {
		v.SwapDir = g.swapDir
		if g.phase == "dingque" || (g.phase == "play" && len(me.Discards) == 0) {
			v.Got = g.got
		}
	}
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
		} else {
			for i := range c.Melds {
				if c.Melds[i].Kind == "angang" && g.phase != "end" {
					c.Melds[i].Tile = -1 // face down
				}
			}
		}
		if (g.phase == "swap" || g.phase == "dingque") && p.ID != Human {
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
		if g.turn == Human && !me.Hu {
			v.Options = &Options{Discard: true, Hu: me.drawn >= 0 && Wins(me.hand, me.Melds, me.Que), Gang: g.kongs(me), MustQue: me.hasQue()}
		}
	}
	if g.phase == "play" && !me.Hu {
		seen := g.seenBy(me)
		if v.Options != nil && v.Options.Discard {
			v.Hints = g.hints(me, &seen)
		} else if me.hand.total()%3 == 1 {
			v.Waits = waitsSeen(me.hand, me.Melds, me.Que, &seen)
		}
	}
	if g.phase == "end" {
		v.Sheet = g.sheet()
	}
	sort.SliceStable(v.Players, func(i, j int) bool { return v.Players[i].ID < v.Players[j].ID })
	return v
}

// hints lists, for each tile the human could discard, the waits it leaves.
func (g *Game) hints(p *Player, seen *Counts) []Hint {
	var out []Hint
	c := p.hand
	for t := Tile(0); t < 27; t++ {
		if c[t] == 0 {
			continue
		}
		c[t]--
		if ws := waitsSeen(c, p.Melds, p.Que, seen); len(ws) > 0 {
			out = append(out, Hint{t, ws})
		}
		c[t]++
	}
	return out
}

// sheet is each player's payments this round, as lines with a sign.
func (g *Game) sheet() [][]Line {
	out := make([][]Line, 4)
	for _, p := range g.pays {
		from, to := g.players[p.From].Name, g.players[p.To].Name
		out[p.To] = append(out[p.To], Line{p.Why + " ← " + from, p.Amount})
		out[p.From] = append(out[p.From], Line{p.Why + " → " + to, -p.Amount})
	}
	return out
}
