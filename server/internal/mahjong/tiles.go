// Package mahjong is a Sichuan "bloody battle" (血战到底) table as the
// mainstream apps play it: 108 tiles in three suits, exchange three (换三张),
// a void suit (定缺), pong and kong but no chow, kongs paid on the spot
// (刮风下雨), robbing a kong, passing a win blocks discard wins until your
// next draw, and play goes on after a win until three players have won or
// the wall runs out (then 查花猪, 查大叫 and 退税).
package mahjong

import "fmt"

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
	return c.suitCount(s) > 0
}

func (c *Counts) suitCount(s int) int {
	if s < 0 || s > 2 {
		return 0
	}
	n := 0
	for i := s * 9; i < s*9+9; i++ {
		n += int(c[i])
	}
	return n
}

// Meld is an exposed pong or kong (angang: a concealed kong).
type Meld struct {
	Kind string `json:"kind"` // peng gang angang
	Tile Tile   `json:"tile"`
	From int    `json:"from"` // who gave the tile (own ID for angang and 补杠's pong giver)
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

// sevenPairs: seven pairs, where four of a kind counts as two (龙七对).
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

// list is the tiles of c in order (万, 条, 筒; low to high).
func list(c Counts) []Tile {
	out := []Tile{}
	for t := Tile(0); t < 27; t++ {
		for k := int8(0); k < c[t]; k++ {
			out = append(out, t)
		}
	}
	return out
}
