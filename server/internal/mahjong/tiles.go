// Package mahjong is a Sichuan "bloody battle" (血战到底) table: 108 tiles,
// three suits, a void suit (定缺) chosen at the start, pong and kong but no
// chow, and play goes on after a win until three players have won or the
// wall runs out.
package mahjong

import (
	"fmt"
	"strings"
)

// Tile is 0..26: suit*9 + rank-1. Suits: 0 万, 1 条, 2 筒.
type Tile int8

var SuitNames = []string{"万", "条", "筒"}

func (t Tile) Suit() int { return int(t) / 9 }
func (t Tile) Rank() int { return int(t)%9 + 1 }

func (t Tile) String() string { return fmt.Sprintf("%d%s", t.Rank(), SuitNames[t.Suit()]) }

// Counts is a hand as the number of each tile.
type Counts [27]int8

func (c *Counts) total() int {
	n := 0
	for _, v := range c {
		n += int(v)
	}
	return n
}

func (c *Counts) hasSuit(s int) bool {
	for i := s * 9; i < s*9+9; i++ {
		if c[i] > 0 {
			return true
		}
	}
	return false
}

// Meld is an exposed pong or kong (angang: a concealed kong).
type Meld struct {
	Kind string `json:"kind"` // peng gang angang
	Tile Tile   `json:"tile"`
}

// sets reports whether c splits into triplets and runs.
func sets(c *Counts) bool {
	i := 0
	for i < 27 && c[i] == 0 {
		i++
	}
	if i == 27 {
		return true
	}
	if c[i] >= 3 {
		c[i] -= 3
		ok := sets(c)
		c[i] += 3
		if ok {
			return true
		}
	}
	if i%9 <= 6 && c[i+1] > 0 && c[i+2] > 0 {
		c[i]--
		c[i+1]--
		c[i+2]--
		ok := sets(c)
		c[i]++
		c[i+1]++
		c[i+2]++
		if ok {
			return true
		}
	}
	return false
}

func standard(c Counts) bool {
	for i := range c {
		if c[i] >= 2 {
			c[i] -= 2
			if sets(&c) {
				return true
			}
			c[i] += 2
		}
	}
	return false
}

func sevenPairs(c Counts, melds int) bool {
	if melds > 0 {
		return false
	}
	for _, v := range c {
		if v%2 != 0 {
			return false
		}
	}
	return c.total() == 14
}

// Wins reports whether a concealed hand plus melds is a winning hand for a
// player void in suit que.
func Wins(c Counts, melds []Meld, que int) bool {
	if c.total() != 14-3*len(melds) || (que >= 0 && c.hasSuit(que)) {
		return false
	}
	return sevenPairs(c, len(melds)) || standard(c)
}

const MaxFan = 4

// Fan scores a winning hand: its doublings (capped at MaxFan) and their names.
func Fan(c Counts, melds []Meld, zimo, kongDraw bool) (int, string) {
	fan := 0
	var names []string
	all := c
	for _, m := range melds {
		n := int8(3)
		if m.Kind != "peng" {
			n = 4
		}
		all[m.Tile] += n
	}
	gen := 0 // 根: all four of a tile
	for _, v := range all {
		if v == 4 {
			gen++
		}
	}
	seven := sevenPairs(c, len(melds))
	switch {
	case seven && gen > 0:
		fan += 2
		names = append(names, "龙七对")
	case seven:
		fan += 2
		names = append(names, "七对")
	case triplets(c):
		fan++
		names = append(names, "对对胡")
	}
	suits := 0
	for s := 0; s < 3; s++ {
		if all.hasSuit(s) {
			suits++
		}
	}
	if suits == 1 {
		fan += 2
		names = append([]string{"清一色"}, names...)
	}
	if gen > 0 {
		fan += gen
		names = append(names, fmt.Sprintf("%d根", gen))
	}
	if zimo {
		fan++
		names = append(names, "自摸")
	}
	if kongDraw {
		fan++
		names = append(names, "杠上开花")
	}
	if len(names) == 0 || (len(names) == 1 && zimo) {
		names = append([]string{"平胡"}, names...)
	}
	if fan > MaxFan {
		fan = MaxFan
	}
	return fan, strings.Join(names, " ")
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

// Waits lists the tiles that would complete a 13-tile hand, with the best fan
// reachable by a discard win.
func Waits(c Counts, melds []Meld, que int) (waits []Tile, best int) {
	best = -1
	for t := Tile(0); t < 27; t++ {
		if t.Suit() == que || c[t] >= 4 {
			continue
		}
		c[t]++
		if Wins(c, melds, que) {
			waits = append(waits, t)
			if f, _ := Fan(c, melds, false, false); f > best {
				best = f
			}
		}
		c[t]--
	}
	return waits, best
}
