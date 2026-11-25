package mahjong

import (
	"fmt"
	"strings"
)

// MaxFan caps the doublings of one win (and is what a 花猪 pays).
const MaxFan = 8

// WinCtx is how a hand was won, for the bonus doublings.
type WinCtx struct {
	Zimo        bool // self-drawn
	KongDraw    bool // 杠上花: self-drawn on the tile after a kong
	KongDiscard bool // 杠上炮: won on the discard right after the discarder's kong
	RobKong     bool // 抢杠胡
	LastTile    bool // 海底: on the last tile of the wall
	Heaven      bool // 天胡: the dealer's first 14 tiles
	Earth       bool // 地胡: another player's first draw, nothing claimed yet
}

// FanItem is one named part of a score.
type FanItem struct {
	Name string `json:"name"`
	Fan  int    `json:"fan"`
}

// Score values a winning hand: its doublings, capped at MaxFan, and what
// they are made of. The main pattern is the best one that fits; 根, 自摸 and
// the situational bonuses add to it.
func Score(c Counts, melds []Meld, x WinCtx) (int, []FanItem) {
	all := c
	for _, m := range melds {
		n := int8(3)
		if m.Kind != "peng" {
			n = 4
		}
		all[m.Tile] += n
	}
	suits := 0
	for s := 0; s < 3; s++ {
		if all.hasSuit(s) {
			suits++
		}
	}
	qing := suits == 1
	gen := 0 // 根: all four of a tile in the hand
	for _, v := range all {
		if v == 4 {
			gen++
		}
	}
	var items []FanItem
	add := func(name string, fan int) { items = append(items, FanItem{name, fan}) }
	seven := sevenPairs(c, len(melds))
	switch {
	case x.Heaven:
		add("天胡", MaxFan)
	case x.Earth:
		add("地胡", MaxFan)
	case seven && qing && gen > 0:
		add("清龙七对", 5)
		gen--
	case seven && qing:
		add("清七对", 4)
	case seven && gen > 0:
		add("龙七对", 3)
		gen--
	case seven:
		add("七对", 2)
	case triplets(c) && qing:
		add("清对", 3)
	case triplets(c) && only258(all):
		add("将对", 3)
	case qing:
		add("清一色", 2)
	case triplets(c):
		add("对对胡", 1)
	default:
		add("平胡", 0)
	}
	if gen > 0 {
		add(fmt.Sprintf("%d根", gen), gen)
	}
	if !seven && c.total() == 2 {
		add("金钩钓", 1)
	}
	if x.Zimo {
		add("自摸", 1)
	}
	if x.KongDraw {
		add("杠上花", 1)
	}
	if x.KongDiscard {
		add("杠上炮", 1)
	}
	if x.RobKong {
		add("抢杠胡", 1)
	}
	if x.LastTile {
		if x.Zimo {
			add("海底捞月", 1)
		} else {
			add("海底炮", 1)
		}
	}
	fan := 0
	for _, it := range items {
		fan += it.Fan
	}
	return min(fan, MaxFan), items
}

// Fan is Score as a number and a name, e.g. "清一色 1根 自摸".
func Fan(c Counts, melds []Meld, x WinCtx) (int, string) {
	f, items := Score(c, melds, x)
	return f, itemsName(items)
}

func itemsName(items []FanItem) string {
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.Name
	}
	return strings.Join(names, " ")
}

// triplets: the concealed part is pongs and one pair (melds are pongs already).
func triplets(c Counts) bool {
	pairs := 0
	for _, v := range c {
		switch v {
		case 0, 3:
		case 2:
			pairs++
		default:
			return false
		}
	}
	return pairs == 1
}

func only258(c Counts) bool {
	for t, v := range c {
		if r := Tile(t).Rank(); v > 0 && r != 2 && r != 5 && r != 8 {
			return false
		}
	}
	return true
}

// Wait is a tile that completes a ready hand.
type Wait struct {
	Tile Tile `json:"tile"`
	Left int  `json:"left"` // copies not yet seen
	Fan  int  `json:"fan"`  // as a discard win, before situational bonuses
}

// Waits lists the tiles that would complete a 13-tile hand, with the best fan
// reachable by a discard win.
func Waits(c Counts, melds []Meld, que int) (waits []Tile, best int) {
	best = -1
	for _, w := range waitsSeen(c, melds, que, nil) {
		waits = append(waits, w.Tile)
		best = max(best, w.Fan)
	}
	return waits, best
}

// waitsSeen is Waits with how many of each wait are still unseen, given the
// tiles visible to the player (nil: only the hand itself).
func waitsSeen(c Counts, melds []Meld, que int, seen *Counts) []Wait {
	var out []Wait
	for t := Tile(0); t < 27; t++ {
		if t.Suit() == que || c[t] >= 4 {
			continue
		}
		c[t]++
		if Wins(c, melds, que) {
			f, _ := Score(c, melds, WinCtx{})
			left := 4 - int(c[t]) + 1
			if seen != nil {
				left = 4 - int(seen[t])
			}
			out = append(out, Wait{t, max(0, left), f})
		}
		c[t]--
	}
	return out
}
